import createMiddleware from 'next-intl/middleware';
import { NextResponse, type NextRequest } from 'next/server';

const intl = createMiddleware({ locales: ['en', 'am'], defaultLocale: 'en', localePrefix: 'always' });

export default function middleware(request: NextRequest) {
  const locale = request.nextUrl.pathname.split('/')[1];
  const protectedRoute = /^\/(en|am)\/(account|orders|checkout|group-cart)(\/|$)/.test(request.nextUrl.pathname);
  if (protectedRoute && request.cookies.get('mesob_authenticated')?.value !== '1') {
    const login = new URL(`/${locale === 'am' ? 'am' : 'en'}/auth/login`, request.url);
    login.searchParams.set('next', request.nextUrl.pathname);
    return NextResponse.redirect(login);
  }
  return intl(request);
}
export const config = { matcher: ['/((?!api|_next|.*\\..*).*)'] };
