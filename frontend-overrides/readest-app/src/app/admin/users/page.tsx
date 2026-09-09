'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/context/AuthContext';
import { useTranslation } from '@/hooks/useTranslation';
import {
  personalCreateUser,
  personalDeleteUser,
  personalListUsers,
  type PersonalManagedUser,
  type PersonalUser,
} from '@/services/personal/authApi';

export default function UserManagementPage() {
  const _ = useTranslation();
  const router = useRouter();
  const { user, isAuthLoading } = useAuth();
  const isAdmin = user?.user_metadata?.['role'] === 'admin';
  const [users, setUsers] = useState<PersonalManagedUser[]>([]);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<PersonalUser['role']>('user');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!isAdmin) return;
    personalListUsers().then(setUsers).catch((err) => setError(err instanceof Error ? err.message : _('Could not load users')));
  }, [isAdmin, _]);

  useEffect(() => {
    if (!isAuthLoading && user && !isAdmin) router.replace('/library');
  }, [isAdmin, isAuthLoading, router, user]);

  if (isAuthLoading) return null;
  if (!user || !isAdmin) return null;

  const create = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError('');
    setLoading(true);
    try {
      const result = await personalCreateUser(username, password, role);
      setUsers((current) => [...current, { ...result.user, createdAt: Date.now() }].sort((a, b) => a.username.localeCompare(b.username)));
      setUsername('');
      setPassword('');
      setRole('user');
    } catch (err) {
      setError(err instanceof Error ? err.message : _('Could not create user'));
    } finally {
      setLoading(false);
    }
  };

  const remove = async (managedUser: PersonalManagedUser) => {
    if (!window.confirm(_('Delete user {{username}}?', { username: managedUser.username }))) return;
    setError('');
    try {
      await personalDeleteUser(managedUser.id);
      setUsers((current) => current.filter((item) => item.id !== managedUser.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : _('Could not delete user'));
    }
  };

  return (
    <main className='bg-base-100 min-h-screen px-6 py-12'>
      <div className='mx-auto flex w-full max-w-3xl flex-col gap-8'>
        <div className='flex items-center justify-between gap-4'>
          <div>
            <h1 className='text-2xl font-semibold'>{_('User management')}</h1>
            <p className='text-base-content/70 mt-1'>{_('Only administrators can manage accounts.')}</p>
          </div>
          <button className='btn btn-ghost eink-bordered' onClick={() => router.push('/user')}>
            {_('Back')}
          </button>
        </div>

        <form onSubmit={create} className='eink-bordered flex flex-col gap-4 rounded-xl border p-5'>
          <h2 className='text-lg font-medium'>{_('Add user')}</h2>
          <div className='grid gap-4 sm:grid-cols-3'>
            <input className='input input-bordered eink-bordered' value={username} onChange={(event) => setUsername(event.target.value)} placeholder={_('Username')} required />
            <input className='input input-bordered eink-bordered' value={password} onChange={(event) => setPassword(event.target.value)} placeholder={_('Password')} type='password' autoComplete='new-password' required />
            <select className='select select-bordered eink-bordered' value={role} onChange={(event) => setRole(event.target.value as PersonalUser['role'])}>
              <option value='user'>{_('User')}</option>
              <option value='admin'>{_('Admin')}</option>
            </select>
          </div>
          <button className='btn btn-primary self-start' type='submit' disabled={loading}>
            {loading ? _('Adding...') : _('Add user')}
          </button>
        </form>

        {error && <p className='text-error'>{error}</p>}

        <section className='eink-bordered overflow-hidden rounded-xl border'>
          <div className='border-base-300 grid grid-cols-[1fr_auto_auto] gap-4 border-b px-5 py-3 text-sm font-medium'>
            <span>{_('Username')}</span>
            <span>{_('Role')}</span>
            <span />
          </div>
          {users.map((managedUser) => (
            <div key={managedUser.id} className='border-base-300 grid grid-cols-[1fr_auto_auto] items-center gap-4 border-b px-5 py-4 last:border-b-0'>
              <span>{managedUser.username}</span>
              <span className='text-base-content/70'>{managedUser.role}</span>
              <button className='btn btn-ghost btn-sm text-error' onClick={() => remove(managedUser)} disabled={managedUser.id === user.id}>
                {_('Delete')}
              </button>
            </div>
          ))}
        </section>
      </div>
    </main>
  );
}
