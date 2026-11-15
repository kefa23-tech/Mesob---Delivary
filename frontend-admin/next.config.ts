import createNextIntlPlugin from 'next-intl/plugin';
export default createNextIntlPlugin('./src/i18n/request.ts')({ reactStrictMode: true, output: 'standalone' });
