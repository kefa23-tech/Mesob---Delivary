import createClient from 'openapi-fetch';
import type { paths } from './generated';
import { authenticatedFetch } from '@/infrastructure/auth/session';

export const apiClient = createClient<paths>({ baseUrl: process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1', fetch: authenticatedFetch });
