import { personalRequest } from './apiClient';

export interface PersonalUser {
  id: string;
  username: string;
}

export const personalLogin = (username: string, password: string) =>
  personalRequest<{ user: PersonalUser }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });

export const personalMe = () => personalRequest<{ user: PersonalUser }>('/auth/me');
export const personalLogout = () => personalRequest<void>('/auth/logout', { method: 'POST' });
export const personalChangePassword = (password: string) =>
  personalRequest<void>('/auth/password', {
    method: 'POST',
    body: JSON.stringify({ password }),
  });
