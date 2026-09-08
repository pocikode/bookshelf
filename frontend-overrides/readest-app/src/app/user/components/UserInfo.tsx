import { PiUserCircle } from 'react-icons/pi';
import UserAvatar from '@/components/UserAvatar';

interface UserInfoProps {
  avatarUrl?: string;
  userFullName: string;
  userName: string;
}

const UserInfo: React.FC<UserInfoProps> = ({ avatarUrl, userFullName, userName }) => (
  <div className='flex flex-col items-center gap-x-6 gap-y-2 md:flex-row md:items-center'>
    <div className='aspect-square h-16 w-16 flex-shrink-0 md:h-24 md:w-24'>
      {avatarUrl ? (
        <UserAvatar
          url={avatarUrl}
          size={128}
          DefaultIcon={PiUserCircle}
          className='h-full w-full'
          borderClassName='border-base-100 border-4'
          fillContainer
        />
      ) : (
        <PiUserCircle className='h-full w-full' />
      )}
    </div>

    <div className='flex-grow text-center md:text-left'>
      <h2 className='text-base-content text-xl font-bold md:text-2xl'>{userFullName}</h2>
      <p className='text-base-content/60'>Username: {userName}</p>
    </div>
  </div>
);

export default UserInfo;
