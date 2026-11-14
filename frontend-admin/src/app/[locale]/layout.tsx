import { NextIntlClientProvider } from 'next-intl';
import { getLocale, getMessages } from 'next-intl/server';
import '../globals.css';

export default async function LocaleLayout({ children }: Readonly<{ children: React.ReactNode }>) { const locale = await getLocale(); return <html lang={locale}><body><NextIntlClientProvider messages={await getMessages()}>{children}</NextIntlClientProvider></body></html>; }
