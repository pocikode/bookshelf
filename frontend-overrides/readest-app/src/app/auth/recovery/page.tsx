'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/context/AuthContext';
import { useThemeStore } from '@/store/themeStore';
import { useTranslation } from '@/hooks/useTranslation';
import { ThemeSupa } from '@supabase/auth-ui-shared';
import { Auth } from '@supabase/auth-ui-react';
import { supabase } from '@/utils/supabase';
import { personalChangePassword } from '@/services/personal/authApi';

const isPersonal = process.env['NEXT_PUBLIC_PERSONAL_APP'] === 'true';

export default function ResetPasswordPage() {
  const _ = useTranslation();
  const router = useRouter();
  const { login } = useAuth();
  const { isDarkMode } = useThemeStore();
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');

  useEffect(() => {
    if (isPersonal) return;

    const { data: subscription } = supabase.auth.onAuthStateChange((event, session) => {
      if (session?.access_token && session.user && event === 'USER_UPDATED') {
        login(session.access_token, session.user);
        const redirectTo = new URLSearchParams(window.location.search).get('redirect');
        router.push(redirectTo ?? '/library');
      }
    });

    return () => {
      subscription?.subscription.unsubscribe();
    };
  }, [login, router]);

  const handlePersonalSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError('');
    setMessage('');
    if (password !== confirmation) {
      setError(_('Passwords do not match'));
      return;
    }

    setLoading(true);
    try {
      await personalChangePassword(password);
      setMessage(_('Password updated successfully'));
      setPassword('');
      setConfirmation('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Auth session missing!');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className='flex min-h-screen items-center justify-center'>
      <div className='w-full max-w-md p-8'>
        {isPersonal ? (
          <form onSubmit={handlePersonalSubmit} className='flex flex-col gap-4'>
            <h1 className='text-center text-xl font-semibold'>{_('Reset Password')}</h1>
            <input
              type='password'
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              placeholder={_('New Password')}
              autoComplete='new-password'
              required
              disabled={loading}
              className='input input-bordered eink-bordered w-full rounded-lg'
            />
            <input
              type='password'
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              placeholder={_('Confirm Password')}
              autoComplete='new-password'
              required
              disabled={loading}
              className='input input-bordered eink-bordered w-full rounded-lg'
            />
            {error && <p className='text-error text-center text-sm'>{error}</p>}
            {message && <p className='text-success text-center text-sm'>{message}</p>}
            <button type='submit' className='btn btn-primary w-full rounded-lg' disabled={loading}>
              {loading ? _('Updating password...') : _('Update password')}
            </button>
          </form>
        ) : (
          <Auth
            supabaseClient={supabase}
            view='update_password'
            appearance={{ theme: ThemeSupa }}
            theme={isDarkMode ? 'dark' : 'light'}
            magicLink={false}
            providers={[]}
            localization={{
              variables: {
                update_password: {
                  password_label: _('New Password'),
                  password_input_placeholder: _('Your new password'),
                  button_label: _('Update password'),
                  loading_button_label: _('Updating password ...'),
                  confirmation_text: _('Your password has been updated'),
                },
              },
            }}
          />
        )}

        <button
          onClick={() => router.back()}
          className={`mt-6 flex w-full items-center justify-center gap-2 rounded-md border px-4 py-2.5 text-sm transition-colors ${
            isDarkMode
              ? 'border-gray-600 text-gray-300 hover:bg-gray-800'
              : 'border-gray-300 text-gray-700 hover:bg-gray-100'
          }`}
        >
          {_('Back')}
        </button>
      </div>
    </div>
  );
}
