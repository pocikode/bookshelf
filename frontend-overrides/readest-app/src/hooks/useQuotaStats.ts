import { useEffect, useState } from 'react';
import { useAuth } from '@/context/AuthContext';
import { UserPlan } from '@/types/quota';
import { getUserProfilePlan } from '@/utils/access';
import { setCachedUserPlan } from '@/services/sync/cloudSyncProvider';

// Keep the hook name for existing plan-gated features, but do not expose the
// unimplemented storage or translation quota data to the UI.
export const useQuotaStats = () => {
  const { token, user } = useAuth();
  const [userProfilePlan, setUserProfilePlan] = useState<UserPlan | undefined>(undefined);

  useEffect(() => {
    if (!user || !token) return;
    const profilePlan = getUserProfilePlan(token);
    setUserProfilePlan(profilePlan);
    setCachedUserPlan(profilePlan);
  }, [token, user]);

  return { userProfilePlan };
};
