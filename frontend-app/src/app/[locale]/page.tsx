import { getTranslations } from 'next-intl/server';
import Link from 'next/link';
import { getRestaurants } from '@/infrastructure/api/server';
import { RestaurantCard } from '@/ui/restaurant-card';

export default async function Home({ params }: { params: Promise<{ locale: string }> }) {
  const t = await getTranslations('home');
  const { locale } = await params;
  const restaurants = await getRestaurants(new URLSearchParams({ lat: '9.0192', lng: '38.7525', limit: '3' }));
  return <main><section className="mx-auto grid max-w-7xl gap-12 px-6 py-20 lg:grid-cols-[1.05fr_.95fr] lg:items-center"><div><p className="text-sm font-bold uppercase tracking-[0.2em] text-emerald-600">Mesob · food for every table</p><h1 className="mt-6 text-5xl font-black leading-[.95] tracking-tight text-slate-950 md:text-7xl">{t('title')}</h1><p className="mt-6 max-w-xl text-lg leading-8 text-slate-500">{t('subtitle')}</p><Link href={`/${locale}/restaurants`} className="mt-8 inline-flex rounded-full bg-slate-950 px-6 py-3 font-bold text-white hover:bg-emerald-700">Explore restaurants →</Link></div><div className="relative rounded-[3rem] bg-gradient-to-br from-emerald-600 via-teal-500 to-amber-300 p-8 shadow-2xl shadow-emerald-900/20"><div className="rounded-[2.5rem] bg-white/15 p-10 text-center text-8xl backdrop-blur">🍛</div><div className="mt-5 rounded-2xl bg-white p-4 shadow-xl"><p className="text-sm font-bold">Made for sharing</p><p className="mt-1 text-xs text-slate-500">Group carts, local favorites, one happy table.</p></div></div></section><section className="mx-auto max-w-7xl px-6 pb-20"><div className="mb-6 flex items-end justify-between"><div><p className="text-sm font-bold uppercase tracking-[0.2em] text-emerald-600">{t('nearby')}</p><h2 className="mt-2 text-3xl font-black">Open around you</h2></div><Link href={`/${locale}/restaurants`} className="text-sm font-bold text-emerald-700">View all</Link></div><div className="grid gap-6 md:grid-cols-3">{restaurants.data.map((restaurant) => <RestaurantCard key={restaurant.id} restaurant={restaurant} locale={locale} />)}</div></section></main>;
}
