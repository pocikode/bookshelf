import { useEffect } from 'react';

export const useLibraryPageHistoryReset = (onRestore: () => void) => {
  useEffect(() => {
    const handlePageShow = (event: PageTransitionEvent) => {
      if (event.persisted) onRestore();
    };
    window.addEventListener('pageshow', handlePageShow);
    return () => window.removeEventListener('pageshow', handlePageShow);
  }, [onRestore]);
};
