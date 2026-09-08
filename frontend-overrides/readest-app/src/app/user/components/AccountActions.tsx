import { useState } from 'react';
import { useTranslation } from '@/hooks/useTranslation';

interface DeleteConfirmationModalProps {
  show: boolean;
  title: string;
  message: string;
  onCancel: () => void;
  onConfirm: () => void;
}

const DeleteConfirmationModal: React.FC<DeleteConfirmationModalProps> = ({
  show,
  title,
  message,
  onCancel,
  onConfirm,
}) => {
  const _ = useTranslation();
  if (!show) return null;

  return (
    <div className='fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-50 p-4'>
      <div className='w-full max-w-md rounded-2xl bg-white p-6'>
        <h3 className='mb-4 text-xl font-bold text-gray-800'>{title}</h3>
        <p className='mb-6 text-gray-600'>{message}</p>
        <div className='flex flex-col gap-3 sm:flex-row'>
          <button
            onClick={onCancel}
            className='flex-1 rounded-lg bg-gray-300 px-4 py-2 font-medium text-gray-800 hover:bg-gray-400'
          >
            {_('Cancel')}
          </button>
          <button
            onClick={onConfirm}
            className='flex-1 rounded-lg bg-red-500 px-4 py-2 font-medium text-white hover:bg-red-600'
          >
            {_('Delete Permanently')}
          </button>
        </div>
      </div>
    </div>
  );
};

interface AccountActionsProps {
  onLogout: () => void;
  onResetPassword: () => void;
  onConfirmDelete: () => void;
  onConfirmDeleteAllBooks: () => void;
}

const AccountActions: React.FC<AccountActionsProps> = ({
  onLogout,
  onResetPassword,
  onConfirmDelete,
  onConfirmDeleteAllBooks,
}) => {
  const _ = useTranslation();
  const [pendingAction, setPendingAction] = useState<'account' | 'books' | null>(null);

  const confirmations = {
    account: {
      title: _('Delete Your Account?'),
      message: _('This action cannot be undone. All your account data will be permanently deleted.'),
      onConfirm: onConfirmDelete,
    },
    books: {
      title: _('Delete All Books?'),
      message: _('This action cannot be undone. Every book will be removed from this device.'),
      onConfirm: onConfirmDeleteAllBooks,
    },
  };
  const confirmation = pendingAction ? confirmations[pendingAction] : null;

  const actionClass =
    'w-full rounded-lg bg-gray-200 px-6 py-3 font-medium text-gray-800 transition-colors hover:bg-gray-300 md:w-auto';
  return (
    <>
      <DeleteConfirmationModal
        show={!!confirmation}
        title={confirmation?.title ?? ''}
        message={confirmation?.message ?? ''}
        onCancel={() => setPendingAction(null)}
        onConfirm={async () => {
          await confirmation?.onConfirm();
          setPendingAction(null);
        }}
      />
      <div className='flex flex-col gap-4 md:grid md:grid-cols-2 lg:grid-cols-3'>
        <button onClick={onResetPassword} className={actionClass}>
          {_('Reset Password')}
        </button>
        <button onClick={onLogout} className={actionClass}>
          {_('Sign Out')}
        </button>
      </div>
      <div className='mt-8 flex flex-col gap-3 rounded-lg border border-red-200 p-4'>
        <h3 className='text-sm font-semibold text-red-600'>{_('Danger Zone')}</h3>
        <div className='flex flex-col gap-4 md:grid md:grid-cols-2 lg:grid-cols-3'>
          <button
            onClick={() => setPendingAction('books')}
            className='w-full rounded-lg bg-red-100 px-6 py-3 font-medium text-red-600 transition-colors hover:bg-red-200 md:w-auto'
          >
            {_('Delete All Books')}
          </button>
          <button
            onClick={() => setPendingAction('account')}
            className='w-full rounded-lg bg-red-100 px-6 py-3 font-medium text-red-600 transition-colors hover:bg-red-200 md:w-auto'
          >
            {_('Delete Account')}
          </button>
        </div>
      </div>
    </>
  );
};

export default AccountActions;
