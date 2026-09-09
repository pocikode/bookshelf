import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

import {
  deletePersonalAnnotation,
  personalAnnotations,
  savePersonalAnnotation,
} from './annotationsApi';

describe('personal annotations API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}')));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  test('reads annotations without browser caching', async () => {
    await personalAnnotations('book/id');

    expect(fetch).toHaveBeenCalledWith(
      '/api/books/book%2Fid/annotations',
      expect.objectContaining({ cache: 'no-store', credentials: 'include' }),
    );
  });

  test('upserts and deletes an annotation on the server', async () => {
    await savePersonalAnnotation('book-id', {
      id: 'note-id',
      type: 'highlight',
      locator: { cfi: 'epubcfi(/6/2)' },
      selectedText: 'text',
      note: '',
      metadata: {},
    });
    expect(fetch).toHaveBeenLastCalledWith(
      '/api/books/book-id/annotations',
      expect.objectContaining({ method: 'PUT', credentials: 'include' }),
    );

    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }));
    await deletePersonalAnnotation('book-id', 'note-id');
    expect(fetch).toHaveBeenLastCalledWith(
      '/api/books/book-id/annotations/note-id',
      expect.objectContaining({ method: 'DELETE', credentials: 'include' }),
    );
  });
});
