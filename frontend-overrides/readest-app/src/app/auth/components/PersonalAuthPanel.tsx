import { useState } from 'react';
import { useTranslation } from '@/hooks/useTranslation';
import { useAuth } from '@/context/AuthContext';
import { personalLogin } from '@/services/personal/authApi';

export default function PersonalAuthPanel() {
  const _ = useTranslation();
  const { personalLogin: setPersonalUser } = useAuth();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setError('');
    setLoading(true);
    try {
      const result = await personalLogin(String(data.get('username') ?? ''), String(data.get('password') ?? ''));
      setPersonalUser(result.user);
    } catch (err) {
      setError(err instanceof Error ? err.message : _('Sign in failed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={submit} className='flex w-full max-w-sm flex-col gap-4'>
      <div className='text-center'><h1 className='text-xl font-semibold'>{_('Sign in to Readest')}</h1><p className='text-base-content/70 mt-1 text-sm'>{_('Personal library')}</p></div>
      <input name='username' required autoComplete='username' placeholder={_('Username')} className='input input-bordered eink-bordered w-full rounded-lg' />
      <input name='password' required type='password' autoComplete='current-password' placeholder={_('Password')} className='input input-bordered eink-bordered w-full rounded-lg' />
      <button type='submit' className='btn btn-primary w-full rounded-lg' disabled={loading}>{loading ? _('Signing in...') : _('Sign in')}</button>
      {error && <p className='text-error text-center text-sm'>{error}</p>}
    </form>
  );
}
