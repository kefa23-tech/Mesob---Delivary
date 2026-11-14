import { getTranslations } from 'next-intl/server';

export default async function Home() {
  const t = await getTranslations('home');
  return <main className="mx-auto max-w-6xl px-6 py-16"><p className="text-sm text-emerald-700">Mesob</p><h1 className="mt-3 text-4xl font-bold">{t('title')}</h1><p className="mt-4 text-slate-600">{t('subtitle')}</p></main>;
}
