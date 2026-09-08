import { personalRequest } from './apiClient';

export const personalAnnotations = (bookId: string) =>
  personalRequest<unknown[]>(`/books/${encodeURIComponent(bookId)}/annotations`);

export const createPersonalAnnotation = (bookId: string, data: unknown) =>
  personalRequest(`/books/${encodeURIComponent(bookId)}/annotations`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
