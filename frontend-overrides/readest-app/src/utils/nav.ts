import { redirect, useRouter } from 'next/navigation';
import { getCurrentWindow, ScrollBarStyle } from '@tauri-apps/api/window';
import { WebviewWindow } from '@tauri-apps/api/webviewWindow';
import { isPWA, isTauriAppPlatform, isWebAppPlatform } from '@/services/environment';
import { BOOK_IDS_SEPARATOR } from '@/services/constants';
import { AppService } from '@/types/system';

let readerWindowsCount = 0;
const createReaderWindow = (appService: AppService, url: string) => {
  const currentWindow = getCurrentWindow();
  const label = currentWindow.label;
  const newLabelPrefix = label === 'main' ? 'reader' : label;
  const win = new WebviewWindow(`${newLabelPrefix}-${readerWindowsCount}`, {
    url,
    width: 800,
    height: 600,
    center: true,
    resizable: true,
    title: 'Readest',
    decorations: !!appService.isMacOSApp,
    // Linux stays opaque: a transparent WebKitGTK window turns invisible when
    // its web process is busy (#3682). macOS uses native decorations instead.
    transparent: !appService.isMacOSApp && !appService.isLinuxApp,
    shadow: appService.isMacOSApp ? undefined : true,
    titleBarStyle: appService.isMacOSApp ? 'overlay' : undefined,
    // Enum ScrollBarStyle is exported as type by tauri, so it cannot be used directly.
    scrollBarStyle: (appService.osPlatform === 'windows'
      ? 'fluentOverlay'
      : 'default') as unknown as ScrollBarStyle,
  });
  win.once('tauri://created', () => {
    console.log('new window created');
    readerWindowsCount += 1;
  });
  win.once('tauri://error', (e) => {
    console.error('error creating window', e);
  });
  win.once('tauri://destroyed', () => {
    readerWindowsCount -= 1;
  });
};

export const showReaderWindow = (
  appService: AppService,
  bookIds: string[],
  queryParams?: string,
) => {
  const ids = bookIds.join(BOOK_IDS_SEPARATOR);
  const params = new URLSearchParams(queryParams || '');
  params.set('ids', ids);
  const url = `/reader?${params.toString()}`;
  createReaderWindow(appService, url);
};

export const showLibraryWindow = (appService: AppService, filenames: string[]) => {
  const params = new URLSearchParams();
  filenames.forEach((filename) => params.append('file', filename));
  const url = `/library?${params.toString()}`;
  createReaderWindow(appService, url);
};

// Bring the main library window back when a reader window asks to "go to library".
// If main was hidden (macOS close-to-hide) we re-show it. If it was destroyed
// (Windows/Linux default close), we recreate a window with the same 'main'
// label so the existing emitTo('main', 'close-reader-window', ...) wiring
// continues to work.
export const ensureMainLibraryWindow = async (appService: AppService) => {
  const existing = await WebviewWindow.getByLabel('main');
  if (existing) {
    await existing.show();
    await existing.unminimize();
    await existing.setFocus();
    return;
  }
  const win = new WebviewWindow('main', {
    url: '/library',
    width: 800,
    height: 600,
    center: true,
    resizable: true,
    title: 'Readest',
    decorations: !!appService.isMacOSApp,
    // Linux stays opaque: a transparent WebKitGTK window turns invisible when
    // its web process is busy (#3682). macOS uses native decorations instead.
    transparent: !appService.isMacOSApp && !appService.isLinuxApp,
    shadow: appService.isMacOSApp ? undefined : true,
    titleBarStyle: appService.isMacOSApp ? 'overlay' : undefined,
    scrollBarStyle: (appService.osPlatform === 'windows'
      ? 'fluentOverlay'
      : 'default') as unknown as ScrollBarStyle,
  });
  win.once('tauri://error', (e) => {
    console.error('error recreating main window', e);
  });
};

export const navigateToReader = (
  router: ReturnType<typeof useRouter>,
  bookIds: string[],
  queryParams?: string,
  navOptions?: { scroll?: boolean },
) => {
  const ids = bookIds.join(BOOK_IDS_SEPARATOR);
  if (isWebAppPlatform() && !isPWA()) {
    const href = `/reader/${ids}${queryParams ? `?${queryParams}` : ''}`;
    // `/reader/[ids]` is a Pages Router route and the library is an App Router
    // one, so leaving the library for a book is always a full document
    // navigation: `router.push` only makes the App Router discover that for
    // itself and call `location.assign`. Going through the router is what
    // breaks the shelf on Back. Firefox keeps the library document in the
    // back-forward cache, so it comes back with the App Router still holding
    // the state of that in-flight MPA navigation - it re-fetches the next route
    // on the following click but never navigates again, and the suspended
    // router tree takes the rest of the page's interactivity down with it.
    // Navigating the document directly means the router never enters that
    // state. Chromium hides the bug by not restoring this page from its
    // back-forward cache.
    //
    // Only for the hop *into* the reader. `useBooksManager` calls this again
    // from inside the reader to switch books, and that one stays on
    // `/reader/[ids]`, where `router.push` is a real client-side Pages Router
    // transition worth keeping.
    if (!window.location.pathname.startsWith('/reader/')) {
      window.location.assign(href);
      return;
    }
    router.push(href, navOptions);
  } else {
    const params = new URLSearchParams(queryParams || '');
    params.set('ids', ids);
    router.push(`/reader?${params.toString()}`, navOptions);
  }
};

export const navigateToLogin = (router: ReturnType<typeof useRouter>) => {
  const pathname = window.location.pathname;
  const search = window.location.search;
  const currentPath = pathname !== '/auth' ? pathname + search : '/';
  router.push(`/auth?redirect=${encodeURIComponent(currentPath)}`);
};

export const navigateToProfile = (router: ReturnType<typeof useRouter>) => {
  router.push('/user');
};

export const navigateToLibrary = (
  router: ReturnType<typeof useRouter>,
  queryParams?: string,
  navOptions?: { scroll?: boolean },
  navBack?: boolean,
) => {
  const lastLibraryParams =
    typeof window !== 'undefined' ? sessionStorage.getItem('lastLibraryParams') : null;
  if (navBack && lastLibraryParams) {
    queryParams = lastLibraryParams;
  }

  router.replace(`/library${queryParams ? `?${queryParams}` : ''}`, navOptions);
};

// Recovery action when a reader has nothing to display — e.g. all books were
// closed, or a book failed to load in a freshly-opened reader window.
// In a dedicated reader window we close the window itself, ensuring the main
// library window is visible first; routing the reader window to /library
// instead would leave a leftover window the user has to close manually.
// In the main window or on web, fall back to /library navigation.
export const closeReaderWindowOrGoToLibrary = async (
  appService: AppService | null,
  router: ReturnType<typeof useRouter>,
) => {
  if (isTauriAppPlatform() && appService?.hasWindow) {
    const currentWindow = getCurrentWindow();
    if (currentWindow.label !== 'main') {
      await ensureMainLibraryWindow(appService);
      await currentWindow.close();
      return;
    }
  }
  navigateToLibrary(router, '', undefined, true);
};

export const redirectToLibrary = () => {
  redirect('/library');
};

export const navigateToResetPassword = (router: ReturnType<typeof useRouter>) => {
  const pathname = window.location.pathname;
  const search = window.location.search;
  const currentPath = pathname !== '/auth' ? pathname + search : '/';
  router.push(`/auth/recovery?redirect=${encodeURIComponent(currentPath)}`);
};

export const navigateToUpdatePassword = (router: ReturnType<typeof useRouter>) => {
  const pathname = window.location.pathname;
  const search = window.location.search;
  const currentPath = pathname !== '/auth' ? pathname + search : '/';
  router.push(`/auth/update?redirect=${encodeURIComponent(currentPath)}`);
};
