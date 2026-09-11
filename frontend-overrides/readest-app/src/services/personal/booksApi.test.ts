import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

const destroy = vi.fn();
const getCover = vi.fn();

vi.mock('@/libs/document', () => ({
  DocumentLoader: class {
    async open() {
      return {
        book: {
          metadata: {
            title: 'Extracted title',
            author: 'Extracted author',
            language: 'en',
          },
          getCover,
          destroy,
        },
      };
    }
  },
}));

vi.mock('@/utils/svg', () => ({ svg2png: vi.fn() }));

import {
  personalBooks,
  personalBookId,
  personalBookToLibraryBook,
  personalDeleteBook,
  personalUpdateBook,
  personalUploadBook,
} from './booksApi';

describe('personal books API', () => {
  beforeEach(() => {
    getCover.mockResolvedValue(new Blob(['cover'], { type: 'image/png' }));
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            id: 'server-id',
            ownerId: 'owner-id',
            hash: 'content-hash',
            title: 'Extracted title',
            author: 'Extracted author',
            filename: 'book.epub',
            mimeType: 'application/epub+zip',
            size: 4,
            metadata: {},
            visibility: 'public',
            createdAt: 1,
            updatedAt: 1,
          }),
          { status: 201, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  test('uploads extracted metadata and cover with the ebook', async () => {
    await personalUploadBook(new File(['book'], 'book.epub', { type: 'application/epub+zip' }));

    const [, init] = vi.mocked(fetch).mock.calls[0]!;
    const body = init?.body as FormData;
    expect(body.get('title')).toBe('Extracted title');
    expect(body.get('author')).toBe('Extracted author');
    expect(JSON.parse(body.get('metadata') as string)).toMatchObject({
      title: 'Extracted title',
    });
    expect(body.get('cover')).toBeInstanceOf(File);
    expect(destroy).toHaveBeenCalledOnce();
  });

  test('uses the content hash for reading and the server id for API calls', () => {
    const book = personalBookToLibraryBook({
      id: 'server-id',
      ownerId: 'owner-id',
      hash: 'content-hash',
      title: 'Book',
      author: 'Author',
      filename: 'book.epub',
      mimeType: 'application/epub+zip',
      size: 4,
      metadata: {} as never,
      visibility: 'public',
      createdAt: 1,
      updatedAt: 2,
    });

    expect(book.hash).toBe('content-hash');
    expect(book.url).toBe('/api/books/server-id/file');
    expect(book.coverImageUrl).toBe('/api/books/server-id/cover');
    expect(personalBookId(book)).toBe('server-id');
  });

  test('deletes a book by its server id', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }));

    await personalDeleteBook('server-id');

    expect(fetch).toHaveBeenCalledWith(
      '/api/books/server-id',
      expect.objectContaining({ method: 'DELETE', credentials: 'include' }),
    );
  });

  test('updates metadata and visibility by server id', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ id: 'server-id' }), {
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await personalUpdateBook('server-id', {
      title: 'Updated title',
      author: 'Updated author',
      metadata: {} as never,
      visibility: 'private',
    });

    expect(fetch).toHaveBeenCalledWith(
      '/api/books/server-id',
      expect.objectContaining({
        method: 'PUT',
        body: JSON.stringify({
          title: 'Updated title',
          author: 'Updated author',
          metadata: {},
          visibility: 'private',
        }),
        credentials: 'include',
      }),
    );
  });

  test('always loads the book list from the network', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify([]), {
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await personalBooks();

    expect(fetch).toHaveBeenCalledWith(
      '/api/books',
      expect.objectContaining({ cache: 'no-store', credentials: 'include' }),
    );
  });

  test('normalizes an empty server response to an empty array', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('null'));

    await expect(personalBooks()).resolves.toEqual([]);
  });
});
