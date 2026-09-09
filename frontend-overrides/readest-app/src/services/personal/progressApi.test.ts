import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

import { personalProgress } from './progressApi';

describe('personal progress API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('null')));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  test('always reads the latest progress from the server', async () => {
    await personalProgress('book/id');

    expect(fetch).toHaveBeenCalledWith(
      '/api/books/book%2Fid/progress',
      expect.objectContaining({ cache: 'no-store', credentials: 'include' }),
    );
  });
});
