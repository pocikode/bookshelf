import { personalRequest } from './apiClient';

export const personalBookmarks = (bookId: string) =>
  personalRequest<unknown[]>(`/books/${encodeURIComponent(bookId)}/bookmarks`);

export const createPersonalBookmark = (bookId: string, locator: unknown, title = '') =>
  personalRequest(`/books/${encodeURIComponent(bookId)}/bookmarks`, {
    method: 'POST',
    body: JSON.stringify({ locator, title }),
  });
