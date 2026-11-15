import { getRequestConfig } from 'next-intl/server';

export default getRequestConfig(async ({ requestLocale }) => {
  const locale = (await requestLocale) || 'en';
  const supported = locale === 'am' ? 'am' : 'en';
  const base = (await import(`../../messages/${supported}.json`)).default;
  const operations = (await import(`../../messages/operations.${supported}.ts`)).default;
  return { locale: supported, messages: { ...base, ...operations } };
});
