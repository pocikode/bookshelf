import { useEffect, useRef, useState } from 'react';
import { useAuth } from '@/context/AuthContext';
import { useEnv } from '@/context/EnvContext';
import { personalBooks, personalBookToLibraryBook } from '@/services/personal/booksApi';
import { useLibraryStore } from '@/store/libraryStore';
import { useSettingsStore } from '@/store/settingsStore';

const isPersonal = process.env['NEXT_PUBLIC_PERSONAL_APP'] === 'true';

export const useLibrary = () => {
  const { envConfig } = useEnv();
  const { user, isAuthLoading } = useAuth();
  const { setLibrary, libraryLoaded: storeLibraryLoaded } = useLibraryStore();
  const { setSettings } = useSettingsStore();
  const [libraryLoaded, setLibraryLoaded] = useState(storeLibraryLoaded);
  const isInitiating = useRef(false);

  useEffect(() => {
    if (isInitiating.current || storeLibraryLoaded || (isPersonal && isAuthLoading)) {
      if (storeLibraryLoaded && !libraryLoaded) setLibraryLoaded(true);
      return;
    }
    if (isPersonal && !user) return;

    isInitiating.current = true;
    const initLibrary = async () => {
      const appService = await envConfig.getAppService();
      const settings = await appService.loadSettings();
      setSettings(settings);
      setLibrary(
        isPersonal
          ? (await personalBooks()).map(personalBookToLibraryBook)
          : await appService.loadLibraryBooks(),
      );
      setLibraryLoaded(true);
    };

    initLibrary().catch(() => {
      isInitiating.current = false;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storeLibraryLoaded, isAuthLoading, user]);

  return { libraryLoaded };
};
