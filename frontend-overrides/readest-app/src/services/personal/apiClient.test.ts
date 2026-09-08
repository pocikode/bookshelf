import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

import { personalRequest } from './apiClient';

describe('personalRequest', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ ok: true }), {
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  test('lets the browser set the multipart boundary for FormData', async () => {
    const body = new FormData();
    body.set('file', new File(['book'], 'book.epub'));

    await personalRequest('/books', { method: 'POST', body });

    const [, init] = vi.mocked(fetch).mock.calls[0]!;
    expect(new Headers(init?.headers).has('Content-Type')).toBe(false);
  });

  test('sets the JSON content type for serialized request bodies', async () => {
    await personalRequest('/auth/login', { method: 'POST', body: JSON.stringify({ username: 'reader' }) });

    const [, init] = vi.mocked(fetch).mock.calls[0]!;
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json');
  });
});
