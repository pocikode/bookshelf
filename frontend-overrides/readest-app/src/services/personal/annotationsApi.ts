import { personalRequest } from './apiClient';

export const personalAnnotations = (bookId: string) =>
  personalRequest<PersonalAnnotation[]>(`/books/${encodeURIComponent(bookId)}/annotations`, {
    cache: 'no-store',
  }).then((annotations) => annotations ?? []);

export interface PersonalAnnotation {
  id: string;
  bookId: string;
  type: 'highlight' | 'note';
  locator: { cfi?: string; xpointer0?: string; xpointer1?: string };
  selectedText: string;
  note: string;
  metadata: Record<string, unknown>;
  createdAt: number;
  updatedAt: number;
  deletedAt?: number;
}

export const savePersonalAnnotation = (bookId: string, data: unknown) =>
  personalRequest<PersonalAnnotation>(`/books/${encodeURIComponent(bookId)}/annotations`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });

export const deletePersonalAnnotation = (bookId: string, id: string) =>
  personalRequest<void>(
    `/books/${encodeURIComponent(bookId)}/annotations/${encodeURIComponent(id)}`,
    { method: 'DELETE' },
  );
