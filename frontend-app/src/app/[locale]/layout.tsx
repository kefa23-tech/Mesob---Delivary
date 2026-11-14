import { NextIntlClientProvider } from 'next-intl';
import { getLocale, getMessages } from 'next-intl/server';
import '../globals.css';

export default async function LocaleLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const locale = await getLocale();
  const messages = await getMessages();
  return <html lang={locale} dir={locale === 'am' ? 'ltr' : 'ltr'}><body><NextIntlClientProvider messages={messages}>{children}</NextIntlClientProvider></body></html>;
}
