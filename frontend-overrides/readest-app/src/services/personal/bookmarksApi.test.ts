import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

import { deletePersonalBookmark, personalBookmarks, savePersonalBookmark } from './bookmarksApi';

describe('personal bookmarks API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}')));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  test('reads bookmarks without browser caching', async () => {
    await personalBookmarks('book/id');

    expect(fetch).toHaveBeenCalledWith(
      '/api/books/book%2Fid/bookmarks',
      expect.objectContaining({ cache: 'no-store', credentials: 'include' }),
    );
  });

  test('upserts and deletes a bookmark on the server', async () => {
    await savePersonalBookmark('book-id', 'bookmark-id', { cfi: 'epubcfi(/6/2)' }, 'Page 2');
    expect(fetch).toHaveBeenLastCalledWith(
      '/api/books/book-id/bookmarks',
      expect.objectContaining({ method: 'PUT', credentials: 'include' }),
    );

    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }));
    await deletePersonalBookmark('book-id', 'bookmark-id');
    expect(fetch).toHaveBeenLastCalledWith(
      '/api/books/book-id/bookmarks/bookmark-id',
      expect.objectContaining({ method: 'DELETE', credentials: 'include' }),
    );
  });
});
