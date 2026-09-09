import { useEffect, useRef } from 'react';
import { useAuth } from '@/context/AuthContext';
import { useBookDataStore } from '@/store/bookDataStore';
import { useReaderStore } from '@/store/readerStore';
import { BookNote } from '@/types/book';
import { personalBookId } from '@/services/personal/booksApi';
import {
  deletePersonalAnnotation,
  personalAnnotations,
  savePersonalAnnotation,
} from '@/services/personal/annotationsApi';
import {
  deletePersonalBookmark,
  personalBookmarks,
  savePersonalBookmark,
} from '@/services/personal/bookmarksApi';

const isPersonal = process.env['NEXT_PUBLIC_PERSONAL_APP'] === 'true';

const remoteBookmarkToNote = (bookmark: {
  id: string;
  locator: { cfi?: string };
  title: string;
  createdAt: number;
  updatedAt: number;
  deletedAt?: number;
}): BookNote | null => {
  if (!bookmark.locator.cfi) return null;
  return {
    id: bookmark.id,
    type: 'bookmark',
    cfi: bookmark.locator.cfi,
    text: bookmark.title,
    note: '',
    createdAt: bookmark.createdAt,
    updatedAt: bookmark.updatedAt,
    deletedAt: bookmark.deletedAt,
  };
};

const remoteAnnotationToNote = (annotation: {
  id: string;
  type: 'highlight' | 'note';
  locator: { cfi?: string; xpointer0?: string; xpointer1?: string };
  selectedText: string;
  note: string;
  metadata: Record<string, unknown>;
  createdAt: number;
  updatedAt: number;
  deletedAt?: number;
}): BookNote | null => {
  if (!annotation.locator.cfi) return null;
  const metadata = annotation.metadata;
  return {
    id: annotation.id,
    type: 'annotation',
    cfi: annotation.locator.cfi,
    xpointer0: annotation.locator.xpointer0,
    xpointer1: annotation.locator.xpointer1,
    text: annotation.selectedText,
    note: annotation.note,
    style: metadata['style'] as BookNote['style'],
    color: metadata['color'] as BookNote['color'],
    global: metadata['global'] as boolean | undefined,
    page: metadata['page'] as number | undefined,
    createdAt: annotation.createdAt,
    updatedAt: annotation.updatedAt,
    deletedAt: annotation.deletedAt,
  };
};

const noteToRemote = (note: BookNote) => {
  if (note.type === 'bookmark') {
    return {
      kind: 'bookmark' as const,
      id: note.id,
      locator: { cfi: note.cfi },
      title: note.text ?? '',
    };
  }
  return {
    kind: 'annotation' as const,
    id: note.id,
    type: note.note ? 'note' : 'highlight',
    locator: { cfi: note.cfi, xpointer0: note.xpointer0, xpointer1: note.xpointer1 },
    selectedText: note.text ?? '',
    note: note.note,
    metadata: {
      style: note.style,
      color: note.color,
      global: note.global,
      page: note.page,
    },
  };
};

export const usePersonalNotesSync = (bookKey: string) => {
  const { user } = useAuth();
  const getView = useReaderStore((state) => state.getView);
  const syncing = useRef(false);
  const pendingNotes = useRef<BookNote[] | null>(null);
  const ready = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!isPersonal || !user) return;
    const store = useBookDataStore.getState();
    const book = store.getBookData(bookKey)?.book;
    const bookId = book && personalBookId(book);
    if (!bookId) return;

    const push = async (notes: BookNote[]) => {
      if (syncing.current) {
        pendingNotes.current = notes;
        return;
      }
      syncing.current = true;
      try {
        await Promise.all(
          notes.map(async (note) => {
            if (note.type === 'excerpt') return;
            if (note.deletedAt) {
              if (note.type === 'bookmark') await deletePersonalBookmark(bookId, note.id);
              else await deletePersonalAnnotation(bookId, note.id);
              return;
            }
            const remote = noteToRemote(note);
            if (remote.kind === 'bookmark') {
              await savePersonalBookmark(bookId, remote.id, remote.locator, remote.title);
            } else {
              await savePersonalAnnotation(bookId, remote);
            }
          }),
        );
      } finally {
        syncing.current = false;
        const next = pendingNotes.current;
        pendingNotes.current = null;
        if (next) await push(next);
      }
    };

    const pull = async () => {
      const [remoteBookmarks, remoteAnnotations] = await Promise.all([
        personalBookmarks(bookId),
        personalAnnotations(bookId),
      ]);
      const remoteNotes = [
        ...remoteBookmarks.map(remoteBookmarkToNote),
        ...remoteAnnotations.map(remoteAnnotationToNote),
      ].filter((note): note is BookNote => note !== null);
      const current = useBookDataStore.getState().getConfig(bookKey);
      if (!current) return;
      const localById = new Map((current.booknotes ?? []).map((note) => [note.id, note]));
      for (const remote of remoteNotes) {
        const local = localById.get(remote.id);
        if (!local || remote.updatedAt >= local.updatedAt) localById.set(remote.id, remote);
      }
      const merged = [...localById.values()];
      useBookDataStore.getState().setConfig(bookKey, { booknotes: merged });
      const view = getView(bookKey);
      if (view) {
        for (const note of remoteNotes) {
          if (note.deletedAt) view.addAnnotation(note, true);
          else if (note.type === 'annotation') view.addAnnotation(note);
        }
      }
      ready.current = true;
      await push(merged);
    };

    pull().catch((error) => console.warn('Could not sync personal notes:', error));

    const unsubscribe = useBookDataStore.subscribe((state, previous) => {
      const currentNotes = state.booksData[bookKey.split('-')[0]!]?.config?.booknotes;
      const previousNotes = previous.booksData[bookKey.split('-')[0]!]?.config?.booknotes;
      if (!ready.current || currentNotes === previousNotes) return;
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => {
        push(currentNotes ?? []).catch((error) => console.warn('Could not save personal notes:', error));
      }, 250);
    });
    return () => {
      unsubscribe();
      if (timer.current) clearTimeout(timer.current);
      ready.current = false;
    };
  }, [bookKey, user, getView]);
};
