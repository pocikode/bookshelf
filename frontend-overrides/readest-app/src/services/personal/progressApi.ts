import { personalRequest } from './apiClient';

export interface PersonalProgress {
  bookId: string;
  locator: { cfi?: string };
  progress: number;
  deviceId: string;
}

const deviceId = () => {
  const key = 'readest-personal-device-id';
  const existing = localStorage.getItem(key);
  if (existing) return existing;
  const value = crypto.randomUUID();
  localStorage.setItem(key, value);
  return value;
};

export const personalProgress = (bookId: string) =>
  personalRequest<PersonalProgress | null>(`/books/${encodeURIComponent(bookId)}/progress`);

export const savePersonalProgress = (bookId: string, locator: string, progress: number) =>
  personalRequest<PersonalProgress>(`/books/${encodeURIComponent(bookId)}/progress`, {
    method: 'PUT',
    body: JSON.stringify({ bookId, locator: { cfi: locator }, progress, deviceId: deviceId() }),
  });
