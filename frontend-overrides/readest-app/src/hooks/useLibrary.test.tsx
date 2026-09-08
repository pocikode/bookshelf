import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  loadLibraryBooks: vi.fn(),
  loadSettings: vi.fn(),
  personalBooks: vi.fn(),
  setLibrary: vi.fn(),
  setSettings: vi.fn(),
}));

vi.mock('@/context/AuthContext', () => ({
  useAuth: () => ({ user: { id: 'user-1' }, isAuthLoading: false }),
}));

vi.mock('@/context/EnvContext', () => ({
  useEnv: () => ({
    envConfig: {
      getAppService: async () => ({
        loadLibraryBooks: mocks.loadLibraryBooks,
        loadSettings: mocks.loadSettings,
      }),
    },
  }),
}));

vi.mock('@/services/personal/booksApi', () => ({
  personalBooks: mocks.personalBooks,
  personalBookToLibraryBook: (book: object) => book,
}));

vi.mock('@/store/libraryStore', () => ({
  useLibraryStore: () => ({
    setLibrary: mocks.setLibrary,
    libraryLoaded: true,
  }),
}));

vi.mock('@/store/settingsStore', () => ({
  useSettingsStore: () => ({ setSettings: mocks.setSettings }),
}));

describe('useLibrary in personal mode', () => {
  beforeEach(() => {
    vi.stubEnv('NEXT_PUBLIC_PERSONAL_APP', 'true');
    mocks.loadSettings.mockResolvedValue({});
    mocks.personalBooks.mockResolvedValue([]);
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.resetModules();
    vi.unstubAllEnvs();
  });

  test('ignores a persisted Readest library and replaces it with the API response', async () => {
    const { useLibrary } = await import('./useLibrary');
    const { result } = renderHook(() => useLibrary());

    expect(result.current.libraryLoaded).toBe(false);

    await waitFor(() => expect(result.current.libraryLoaded).toBe(true));
    expect(mocks.personalBooks).toHaveBeenCalledOnce();
    expect(mocks.loadLibraryBooks).not.toHaveBeenCalled();
    expect(mocks.setLibrary).toHaveBeenCalledWith([]);
  });
});
