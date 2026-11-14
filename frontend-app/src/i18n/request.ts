import { getRequestConfig } from 'next-intl/server';

export default getRequestConfig(async ({ requestLocale }) => {
  const locale = (await requestLocale) || 'en';
  const supported = locale === 'am' ? 'am' : 'en';
  return { locale: supported, messages: (await import(`../../messages/${supported}.json`)).default };
});
