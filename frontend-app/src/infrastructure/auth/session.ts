import type { components } from '@/infrastructure/api/generated';

type Session = components['schemas']['AuthResponse'];
const ACCESS_KEY = 'mesob_access_token';
const USER_KEY = 'mesob_user';

export function getAccessToken() {
  return typeof window === 'undefined' ? null : window.localStorage.getItem(ACCESS_KEY);
}

export function saveSession(session: Session) {
  window.localStorage.setItem(ACCESS_KEY, session.access_token);
  window.localStorage.setItem(USER_KEY, JSON.stringify(session.user));
  document.cookie = 'mesob_authenticated=1; path=/; max-age=2592000; SameSite=Lax';
}

export function clearSession() {
  window.localStorage.removeItem(ACCESS_KEY);
  window.localStorage.removeItem(USER_KEY);
  document.cookie = 'mesob_authenticated=; path=/; max-age=0; SameSite=Lax';
}

export async function refreshAccessToken() {
  const response = await fetch(`${process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1'}/auth/refresh`, {
    method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: '{}',
  });
  if (!response.ok) { clearSession(); return null; }
  const payload = (await response.json()) as Session | { session: Session };
  const session = 'session' in payload ? payload.session : payload;
  saveSession(session);
  return session;
}

export async function authenticatedFetch(input: RequestInfo | URL, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  const token = getAccessToken();
  if (token) headers.set('Authorization', `Bearer ${token}`);
  let response = await fetch(input, { ...init, headers, credentials: 'include' });
  if (response.status === 401 && typeof window !== 'undefined') {
    const refreshed = await refreshAccessToken();
    if (refreshed) {
      headers.set('Authorization', `Bearer ${refreshed.access_token}`);
      response = await fetch(input, { ...init, headers, credentials: 'include' });
    }
  }
  return response;
}
