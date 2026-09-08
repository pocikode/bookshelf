import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { useTranslation } from '@/hooks/useTranslation';
import { useAuth } from '@/context/AuthContext';
import { personalLogin } from '@/services/personal/authApi';

export default function PersonalAuthPanel() {
  const _ = useTranslation();
  const router = useRouter();
  const { personalLogin: setPersonalUser } = useAuth();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setError('');
    setLoading(true);
    try {
      const result = await personalLogin(
        String(data.get('username') ?? ''),
        String(data.get('password') ?? ''),
      );
      setPersonalUser(result.user);
      // Nothing else navigates on success. The non-personal flows redirect from
      // the `supabase.auth.onAuthStateChange` listener in the parent page, which
      // never fires for a cookie session, so a successful login used to just sit
      // on the form. Honour `?redirect=` the way the library route sets it.
      const redirectTo = new URLSearchParams(window.location.search).get('redirect');
      router.replace(redirectTo ?? '/library');
      return;
    } catch (err) {
      setError(err instanceof Error ? err.message : _('Sign in failed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={submit} className='flex w-full max-w-sm flex-col gap-4'>
      <div className='text-center'>
        <h1 className='text-xl font-semibold'>{_('Sign in to Bookshelf')}</h1>
        <p className='text-base-content/70 mt-1 text-sm'>{_('Shared library')}</p>
      </div>
      <input
        name='username'
        required
        autoComplete='username'
        placeholder={_('Username')}
        className='input input-bordered eink-bordered w-full rounded-lg'
      />
      <input
        name='password'
        required
        type='password'
        autoComplete='current-password'
        placeholder={_('Password')}
        className='input input-bordered eink-bordered w-full rounded-lg'
      />
      <button type='submit' className='btn btn-primary w-full rounded-lg' disabled={loading}>
        {loading ? _('Signing in...') : _('Sign in')}
      </button>
      {error && <p className='text-error text-center text-sm'>{error}</p>}
    </form>
  );
}
