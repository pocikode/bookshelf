import { createClient } from '@supabase/supabase-js';
import { getRuntimeConfig } from '@/services/runtimeConfig';

// The personal deployment has no Supabase project — the Go server owns auth and
// storage. Upstream still imports the eager `supabase` binding from modules that
// load on every route (`context/AuthContext`, `utils/access`, `helpers/auth`),
// so the client used to be constructed on page load even though personal mode
// never calls it.
//
// That construction was not harmless. `createClient` starts the auth client,
// which immediately contends for the `lock:sb-<ref>-auth-token` Navigator
// LockManager lock and rejects with "Acquiring an exclusive Navigator
// LockManager lock ... immediately failed" — the unhandled rejection seen on the
// login page. It also pointed at the upstream demo project baked into
// `NEXT_PUBLIC_DEFAULT_SUPABASE_*_BASE64`, so a personal instance was opening a
// session against a third-party backend.
//
// Rather than fix every import site, keep the module shape identical and return
// an inert client in personal mode. Auth is disabled so no lock is taken and no
// session is persisted, and the URL is a same-origin dead end so a stray call
// fails locally instead of reaching someone else's project.
const isPersonal = process.env['NEXT_PUBLIC_PERSONAL_APP'] === 'true';

const supabaseUrl = isPersonal
  ? 'http://personal.invalid'
  : getRuntimeConfig()?.supabaseUrl ||
    process.env['SUPABASE_URL'] ||
    process.env['NEXT_PUBLIC_SUPABASE_URL'] ||
    atob(process.env['NEXT_PUBLIC_DEFAULT_SUPABASE_URL_BASE64']!);
const supabaseAnonKey = isPersonal
  ? 'personal-disabled'
  : getRuntimeConfig()?.supabaseAnonKey ||
    process.env['SUPABASE_ANON_KEY'] ||
    process.env['NEXT_PUBLIC_SUPABASE_ANON_KEY'] ||
    atob(process.env['NEXT_PUBLIC_DEFAULT_SUPABASE_KEY_BASE64']!);

const personalAuthOptions = {
  auth: {
    persistSession: false,
    autoRefreshToken: false,
    detectSessionInUrl: false,
  },
};

export const supabase = createClient(
  supabaseUrl,
  supabaseAnonKey,
  isPersonal ? personalAuthOptions : undefined,
);

export const createSupabaseClient = (accessToken?: string) => {
  return createClient(supabaseUrl, supabaseAnonKey, {
    global: {
      headers: accessToken
        ? {
            Authorization: `Bearer ${accessToken}`,
          }
        : {},
    },
  });
};

export const createSupabaseAdminClient = () => {
  const supabaseAdminKey = process.env['SUPABASE_ADMIN_KEY'] || '';
  return createClient(supabaseUrl, supabaseAdminKey, {
    auth: {
      persistSession: false,
      autoRefreshToken: false,
      detectSessionInUrl: false,
    },
  });
};
