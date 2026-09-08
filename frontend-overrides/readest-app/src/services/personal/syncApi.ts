import { personalRequest } from './apiClient';

export const pullPersonalSync = (cursor = 0) =>
  personalRequest<{ cursor: number; changes: unknown[] }>(`/sync?cursor=${cursor}`);

export const pushPersonalSync = (deviceId: string, cursor: number, changes: unknown[]) =>
  personalRequest<{ cursor: number; changes: unknown[] }>('/sync', {
    method: 'POST',
    body: JSON.stringify({ deviceId, cursor, changes }),
  });
