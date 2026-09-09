import { personalRequest } from './apiClient';

export interface PersonalUser {
  id: string;
  username: string;
  role: 'admin' | 'user';
}

export interface PersonalManagedUser extends PersonalUser {
  createdAt: number;
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

export const personalListUsers = () => personalRequest<PersonalManagedUser[]>('/users');
export const personalCreateUser = (username: string, password: string, role: PersonalUser['role']) =>
  personalRequest<{ user: PersonalUser }>('/users', {
    method: 'POST',
    body: JSON.stringify({ username, password, role }),
  });
export const personalDeleteUser = (id: string) =>
  personalRequest<void>(`/users/${encodeURIComponent(id)}`, { method: 'DELETE' });
