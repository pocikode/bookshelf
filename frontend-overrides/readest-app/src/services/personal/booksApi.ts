import { personalRequest } from './apiClient';

export interface PersonalBook {
  id: string;
  title: string;
  author: string;
  filename: string;
  mimeType: string;
  size: number;
  hash: string;
  createdAt: number;
  updatedAt: number;
}

export const personalBooks = (query = '') =>
  personalRequest<PersonalBook[]>(`/books${query ? `?q=${encodeURIComponent(query)}` : ''}`);

export const personalBookFile = (id: string) => `/api/books/${encodeURIComponent(id)}/file`;

export const personalUploadBook = (file: File) => {
  const form = new FormData();
  form.set('file', file);
  return personalRequest<PersonalBook>('/books', { method: 'POST', body: form });
};
