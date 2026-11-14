import Link from 'next/link';
import { notFound } from 'next/navigation';
import { getMenu } from '@/infrastructure/api/server';
import { MenuBrowser } from '@/ui/menu-browser';

export const revalidate = 60;

export default async function RestaurantMenuPage({ params }: { params: Promise<{ locale: string; slug: string }> }) {
  const { locale, slug } = await params; const menu = await getMenu(slug);
  if (!menu) notFound();
  return <main className="mx-auto max-w-6xl px-6 py-12"><Link href={`/${locale}/restaurants`} className="text-sm font-bold text-emerald-700">← Back to discovery</Link><div className="mt-8 rounded-[2.5rem] bg-gradient-to-br from-emerald-600 via-teal-500 to-amber-300 p-8 text-white md:p-12"><p className="text-sm font-bold uppercase tracking-[0.2em] text-white/70">Restaurant menu</p><h1 className="mt-3 text-4xl font-black md:text-6xl">{menu.restaurant_name}</h1><p className="mt-4 max-w-xl text-white/80">Choose a favorite, make it yours, and we’ll bring it to your table.</p></div><MenuBrowser menu={menu} /></main>;
}
