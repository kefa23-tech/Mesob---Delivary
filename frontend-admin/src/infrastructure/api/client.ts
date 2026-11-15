import createClient from 'openapi-fetch';
import type { paths } from './generated';
export const apiClient = createClient<paths>({
  baseUrl: process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1',
  fetch: async (request) => {
    const token = typeof window === 'undefined' ? undefined : window.localStorage.getItem('mesob_access_token') ?? undefined;
    const headers = new Headers(request.headers);
    if (token) headers.set('Authorization', `Bearer ${decodeURIComponent(token)}`);
    return fetch(new Request(request, { headers, credentials: 'include' }));
  },
});
