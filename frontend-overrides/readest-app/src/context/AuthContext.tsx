'use client';

import {
  createContext,
  useState,
  useContext,
  useCallback,
  useMemo,
  ReactNode,
  useEffect,
} from 'react';
import { User } from '@supabase/supabase-js';
import { supabase } from '@/utils/supabase';
import posthog from 'posthog-js';
import { personalMe, personalLogout, type PersonalUser } from '@/services/personal/authApi';
import { PERSONAL_SESSION_TOKEN } from '@/services/personal/session';

const isPersonal = process.env['NEXT_PUBLIC_PERSONAL_APP'] === 'true';

interface AuthContextType {
  token: string | null;
  user: User | null;
  /**
   * True until the personal session has been resolved. The cookie is HttpOnly,
   * so the client cannot know whether it is signed in without asking the server
   * — consumers that redirect on "logged out" must wait for this to clear or
   * they will bounce an authenticated user to /auth on every page load.
   * Always false outside personal mode, where state is seeded synchronously.
   */
  isAuthLoading: boolean;
  login: (token: string, user: User) => void;
  logout: () => void;
  personalLogin: (user: PersonalUser) => void;
  refresh: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider = ({ children }: { children: ReactNode }) => {
  // Personal mode deliberately starts logged out and waits for `personalMe()`
  // below: the session lives in an HttpOnly cookie the client cannot read, so
  // localStorage is not a source of truth. Seeding from it would also restore a
  // stale Supabase JWT left over from a previous non-personal session, which
  // upstream would then try to decode.
  const [token, setToken] = useState<string | null>(() => {
    if (!isPersonal && typeof window !== 'undefined') {
      return localStorage.getItem('token');
    }
    return null;
  });
  const [user, setUser] = useState<User | null>(() => {
    if (!isPersonal && typeof window !== 'undefined') {
      const userJson = localStorage.getItem('user');
      return userJson ? JSON.parse(userJson) : null;
    }
    return null;
  });

  const [isAuthLoading, setIsAuthLoading] = useState(isPersonal);

  useEffect(() => {
    if (isPersonal) {
      personalMe()
        .then(({ user }) => {
          setToken(PERSONAL_SESSION_TOKEN);
          setUser({ id: user.id, email: user.username } as User);
        })
        .catch(() => {
          setToken(null);
          setUser(null);
        })
        .finally(() => setIsAuthLoading(false));
      return;
    }
    const syncSession = (
      session: { access_token: string; refresh_token: string; user: User } | null,
    ) => {
      if (session) {
        console.log('Syncing session');
        const { access_token, refresh_token, user } = session;
        localStorage.setItem('token', access_token);
        localStorage.setItem('refresh_token', refresh_token);
        localStorage.setItem('user', JSON.stringify(user));
        posthog.identify(user.id);
        setToken(access_token);
        setUser(user);
      } else {
        localStorage.removeItem('token');
        localStorage.removeItem('refresh_token');
        localStorage.removeItem('user');
        setToken(null);
        setUser(null);
      }
    };
    const refreshSession = async () => {
      try {
        await supabase.auth.refreshSession();
      } catch {
        syncSession(null);
      }
    };

    const { data: subscription } = supabase.auth.onAuthStateChange((_, session) => {
      syncSession(session);
    });

    refreshSession();
    return () => {
      subscription?.subscription.unsubscribe();
    };
  }, []);

  // setToken / setUser from useState are stable across renders, so the empty
  // deps array is correct. Wrapping in useCallback (and only including stable
  // refs in the deps) is what makes the useMemo below actually memoize the
  // context value — without this, login/logout/refresh would be recreated on
  // every render and the memo would always invalidate.
  const login = useCallback((newToken: string, newUser: User) => {
    console.log('Logging in');
    setToken(newToken);
    setUser(newUser);
    localStorage.setItem('token', newToken);
    localStorage.setItem('user', JSON.stringify(newUser));
  }, []);

  const logout = useCallback(async () => {
    if (isPersonal) {
      await personalLogout().catch(() => undefined);
      setToken(null);
      setUser(null);
      return;
    }
    console.log('Logging out');
    try {
      await supabase.auth.refreshSession();
    } catch {
    } finally {
      await supabase.auth.signOut();
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      setToken(null);
      setUser(null);
    }
  }, []);

  const personalLogin = useCallback((newUser: PersonalUser) => {
    setToken(PERSONAL_SESSION_TOKEN);
    setUser({ id: newUser.id, email: newUser.username } as User);
  }, []);

  const refresh = useCallback(async () => {
    // Personal sessions are refreshed server-side by cookie expiry; there is no
    // Supabase session to refresh and calling it would hit the inert client.
    if (isPersonal) return;
    try {
      await supabase.auth.refreshSession();
    } catch {}
  }, []);

  const value = useMemo(
    () => ({ token, user, isAuthLoading, login, logout, personalLogin, refresh }),
    [token, user, isAuthLoading, login, logout, personalLogin, refresh],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};

export const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth must be used within AuthProvider');
  return context;
};
