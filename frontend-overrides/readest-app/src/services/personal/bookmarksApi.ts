import { personalRequest } from './apiClient';

export const personalBookmarks = (bookId: string) =>
  personalRequest<PersonalBookmark[]>(`/books/${encodeURIComponent(bookId)}/bookmarks`, {
    cache: 'no-store',
  }).then((bookmarks) => bookmarks ?? []);

export interface PersonalBookmark {
  id: string;
  bookId: string;
  locator: { cfi?: string };
  title: string;
  createdAt: number;
  updatedAt: number;
  deletedAt?: number;
}

export const savePersonalBookmark = (
  bookId: string,
  id: string,
  locator: unknown,
  title = '',
) =>
  personalRequest<PersonalBookmark>(`/books/${encodeURIComponent(bookId)}/bookmarks`, {
    method: 'PUT',
    body: JSON.stringify({ id, locator, title }),
  });

export const deletePersonalBookmark = (bookId: string, id: string) =>
  personalRequest<void>(`/books/${encodeURIComponent(bookId)}/bookmarks/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
