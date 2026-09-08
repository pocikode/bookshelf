import { personalRequest } from './apiClient';
import { DocumentLoader, type BookMetadata } from '@/libs/document';
import type { Book } from '@/types/book';
import { formatAuthors, formatTitle } from '@/utils/book';
import { svg2png } from '@/utils/svg';

export interface PersonalBook {
  id: string;
  title: string;
  author: string;
  filename: string;
  mimeType: string;
  size: number;
  hash: string;
  metadata: BookMetadata;
  createdAt: number;
  updatedAt: number;
}

export const personalBooks = (query = '') =>
  personalRequest<PersonalBook[]>(`/books${query ? `?q=${encodeURIComponent(query)}` : ''}`, {
    cache: 'no-store',
  }).then((books) => books ?? []);

export const personalBookFile = (id: string) => `/api/books/${encodeURIComponent(id)}/file`;
export const personalBookCover = (id: string) => `/api/books/${encodeURIComponent(id)}/cover`;

export const personalBookToLibraryBook = (book: PersonalBook): Book => ({
  hash: book.hash,
  format: book.mimeType === 'application/pdf' ? 'PDF' : 'EPUB',
  title: book.title,
  author: book.author,
  metadata: book.metadata,
  url: personalBookFile(book.id),
  coverImageUrl: personalBookCover(book.id),
  createdAt: book.createdAt,
  updatedAt: book.updatedAt,
});

export const personalBookId = (book: Book) => {
  const match = book.url?.match(/^\/api\/books\/([^/]+)\/file$/);
  return match?.[1] ? decodeURIComponent(match[1]) : null;
};

export const personalDeleteBook = (id: string) =>
  personalRequest<void>(`/books/${encodeURIComponent(id)}`, { method: 'DELETE' });

export const personalUploadBook = async (file: File) => {
  const form = new FormData();
  form.set('file', file);

  let bookDoc: Awaited<ReturnType<DocumentLoader['open']>>['book'] | undefined;
  try {
    bookDoc = (await new DocumentLoader(file).open()).book;
    const metadata = bookDoc.metadata;
    form.set('metadata', JSON.stringify(metadata));
    form.set('title', formatTitle(metadata.title));
    form.set('author', formatAuthors(metadata.author, metadata.language));

    let cover = await bookDoc.getCover();
    if (cover?.type === 'image/svg+xml') cover = await svg2png(cover);
    if (cover) form.set('cover', cover, 'cover');
  } catch (error) {
    console.warn('Could not extract uploaded book metadata or cover:', error);
  } finally {
    await bookDoc?.destroy?.();
  }

  return personalRequest<PersonalBook>('/books', { method: 'POST', body: form });
};
