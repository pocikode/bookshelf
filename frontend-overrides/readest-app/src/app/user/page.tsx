'use client';

import clsx from 'clsx';
import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useEnv } from '@/context/EnvContext';
import { useAuth } from '@/context/AuthContext';
import { useTheme } from '@/hooks/useTheme';
import { useThemeStore } from '@/store/themeStore';
import { useTranslation } from '@/hooks/useTranslation';
import { useUserActions } from '@/hooks/useUserActions';
import { navigateToLibrary } from '@/utils/nav';
import { Toast } from '@/components/Toast';
import LegalLinks from '@/components/LegalLinks';
import ProfileHeader from './components/Header';
import UserInfo from './components/UserInfo';
import AccountActions from './components/AccountActions';

const ProfilePage = () => {
  const _ = useTranslation();
  const router = useRouter();
  const { appService } = useEnv();
  const { token, user } = useAuth();
  const { safeAreaInsets, isRoundedWindow } = useThemeStore();
  const [mounted, setMounted] = useState(false);

  useEffect(() => setMounted(true), []);

  useEffect(() => {
    if (!mounted) return;
    if (user && token && appService) return;

    const timer = setTimeout(() => {
      router.push('/auth?redirect=/library');
    }, 1000);
    return () => clearTimeout(timer);
  }, [mounted, user, token, appService, router]);

  useTheme({ systemUIVisible: false });

  const {
    handleLogout,
    handleResetPassword,
    handleConfirmDelete,
    handleDeleteAllBooks,
  } = useUserActions();

  const handleGoBack = () => navigateToLibrary(router);

  const handleDeleteWithMessage = () => {
    handleConfirmDelete(_('Failed to delete user. Please try again later.'));
  };

  const handleDeleteAllBooksWithMessage = () => {
    handleDeleteAllBooks(
      _('All books deleted.'),
      _('Failed to delete books. Please try again later.'),
    );
  };

  if (!mounted) return null;

  if (!user || !token || !appService) {
    return (
      <div className='mx-auto max-w-4xl px-4 py-8'>
        <div className='overflow-hidden rounded-lg shadow-md'>
          <div className='flex min-h-[300px] items-center justify-center p-6'>
            <div className='text-base-content animate-pulse'>{_('Loading profile...')}</div>
          </div>
        </div>
      </div>
    );
  }

  const avatarUrl = user.user_metadata?.['picture'] || user.user_metadata?.['avatar_url'];
  const userFullName = user.user_metadata?.['full_name'] || '-';
  const userName =
    user.user_metadata?.['username'] || user.user_metadata?.['user_name'] || user.email || '-';

  return (
    <div
      className={clsx(
        'bg-base-100 full-height inset-0 select-none overflow-hidden',
        appService.hasRoundedWindow && isRoundedWindow && 'window-border rounded-window',
      )}
    >
      <div
        className='flex h-full w-full flex-col items-center overflow-y-auto'
        style={{ paddingTop: `${safeAreaInsets?.top || 0}px` }}
      >
        <ProfileHeader onGoBack={handleGoBack} />
        <div className='w-full min-w-60 max-w-4xl py-10'>
          <div className='sm:bg-base-200 overflow-hidden rounded-lg sm:p-6 sm:shadow-md'>
            <div className='flex flex-col gap-y-8'>
              <div className='flex flex-col gap-y-8 px-6'>
                <UserInfo
                  avatarUrl={avatarUrl}
                  userFullName={userFullName}
                  userName={userName}
                />
              </div>

              <div className='flex flex-col gap-y-8 px-6'>
                <AccountActions
                  onLogout={handleLogout}
                  onResetPassword={handleResetPassword}
                  onConfirmDelete={handleDeleteWithMessage}
                  onConfirmDeleteAllBooks={handleDeleteAllBooksWithMessage}
                />
                {user.user_metadata?.['role'] === 'admin' && (
                  <button className='btn btn-contrast self-start rounded-lg' onClick={() => router.push('/admin/users')}>
                    {_('Manage users')}
                  </button>
                )}
              </div>

              <LegalLinks />
            </div>
          </div>
        </div>
        <Toast />
      </div>
    </div>
  );
};

export default ProfilePage;
