import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

const h = vi.hoisted(() => {
  const makeStore = <T,>(state: T) => {
    const store = <R,>(selector?: (value: T) => R) => (selector ? selector(state) : state) as R | T;
    (store as unknown as { getState: () => T }).getState = () => state;
    return store as {
      (): T;
      <R>(selector: (value: T) => R): R;
      getState: () => T;
    };
  };
  const view = { goTo: vi.fn(), renderer: { getContents: () => [], primaryIndex: 0 } };
  const state = {
    syncedConfigs: null as unknown[] | null,
    progress: { location: 'page-one' },
    viewState: { previewMode: false },
  };
  return {
    makeStore,
    state,
    view,
    config: { location: 'page-one', progress: [1, 100] as [number, number] },
    book: { url: '/api/books/server-book/file', format: 'EPUB', metaHash: 'meta' },
    personalProgress: vi.fn(),
    saveConfig: vi.fn(async () => undefined),
    syncConfigs: vi.fn(async () => undefined),
    setHoveredBookKey: vi.fn(),
  };
});

vi.mock('@/context/AuthContext', () => ({ useAuth: () => ({ user: { id: 'user-1' } }) }));
vi.mock('@/context/EnvContext', () => ({ useEnv: () => ({ envConfig: {} }) }));
vi.mock('@/hooks/useSync', () => ({
  useSync: () => ({ syncedConfigs: h.state.syncedConfigs, syncConfigs: h.syncConfigs }),
}));
vi.mock('@/store/bookDataStore', () => ({
  useBookDataStore: h.makeStore({
    getConfig: () => h.config,
    saveConfig: h.saveConfig,
    getBookData: () => ({ book: h.book }),
  }),
}));
vi.mock('@/store/readerStore', () => ({
  useReaderStore: h.makeStore({
    getView: () => h.view,
    getViewSettings: () => null,
    setViewSettings: vi.fn(),
    recreateViewer: vi.fn(),
    setHoveredBookKey: h.setHoveredBookKey,
    getViewState: () => h.state.viewState,
  }),
}));
vi.mock('@/store/readerProgressStore', () => ({ useBookProgress: () => h.state.progress }));
vi.mock('@/store/settingsStore', () => ({
  useSettingsStore: h.makeStore({ settings: { globalViewSettings: {} } }),
}));
vi.mock('@/hooks/useTranslation', () => ({ useTranslation: () => (value: string) => value }));
vi.mock('@/utils/proofread', () => ({ mergeProofreadRules: vi.fn() }));
vi.mock('@/utils/serializer', () => ({ serializeConfig: vi.fn() }));
vi.mock('@/libs/document', () => ({ CFI: { compare: () => 0 } }));
vi.mock('@/utils/debounce', () => ({
  debounce: (fn: () => void) => {
    const debounced = () => undefined;
    debounced.flush = fn;
    return debounced;
  },
}));
vi.mock('@/utils/event', () => ({
  eventDispatcher: { on: vi.fn(), off: vi.fn(), dispatch: vi.fn() },
}));
vi.mock('@/services/constants', () => ({
  DEFAULT_BOOK_SEARCH_CONFIG: {},
  SYNC_PROGRESS_INTERVAL_SEC: 3,
}));
vi.mock('@/utils/xcfi', () => ({ getCFIFromXPointer: vi.fn(), getXPointerFromCFI: vi.fn() }));
vi.mock('@/utils/cfi', () => ({ isMalformedLocationCfi: () => false }));
vi.mock('@/services/personal/progressApi', () => ({
  personalProgress: h.personalProgress,
  savePersonalProgress: vi.fn(),
}));
vi.mock('@/services/personal/booksApi', () => ({ personalBookId: () => 'server-book' }));
vi.mock('@/types/book', () => ({ FIXED_LAYOUT_FORMATS: new Set() }));

import { useProgressSync } from '@/app/reader/hooks/useProgressSync';

describe('personal reading progress restore', () => {
  beforeEach(() => {
    vi.stubEnv('NEXT_PUBLIC_PERSONAL_APP', 'true');
    h.personalProgress.mockResolvedValue({
      locator: { cfi: 'remote-page' },
      progress: 0.42,
      bookId: 'server-book',
      deviceId: 'firefox-device',
    });
    h.saveConfig.mockClear();
    h.view.goTo.mockClear();
    h.setHoveredBookKey.mockClear();
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllEnvs();
  });

  test('moves a newly opened browser to the server-saved location', async () => {
    renderHook(() => useProgressSync('book-view'));

    await waitFor(() => expect(h.saveConfig).toHaveBeenCalledOnce());

    expect(h.saveConfig).toHaveBeenCalledWith(
      {},
      'book-view',
      expect.objectContaining({
        location: 'remote-page',
        progress: [42, 100],
      }),
      { globalViewSettings: {} },
    );
    expect(h.view.goTo).toHaveBeenCalledWith('remote-page');
  });
});
