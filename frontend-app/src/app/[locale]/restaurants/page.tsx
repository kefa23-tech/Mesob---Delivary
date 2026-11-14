import { getRestaurants } from '@/infrastructure/api/server';
import { RestaurantCard } from '@/ui/restaurant-card';
import { DiscoveryFilters } from '@/ui/discovery-filters';

export const revalidate = 30;

export default async function RestaurantsPage({ params, searchParams }: { params: Promise<{ locale: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { locale } = await params; const query = await searchParams;
  const paramsForApi = new URLSearchParams({ lat: String(query.lat ?? '9.0192'), lng: String(query.lng ?? '38.7525'), limit: '24' });
  for (const key of ['q', 'cuisine', 'eco_packaging', 'cursor']) if (typeof query[key] === 'string') paramsForApi.set(key, query[key] as string);
  const result = await getRestaurants(paramsForApi);
  return <main className="mx-auto max-w-7xl px-6 py-12"><div className="max-w-2xl"><p className="text-sm font-bold uppercase tracking-[0.2em] text-emerald-600">Addis Ababa · Near you</p><h1 className="mt-3 text-4xl font-black tracking-tight text-slate-950 md:text-6xl">What are you craving?</h1><p className="mt-4 text-lg text-slate-500">Fresh food from open kitchens, delivered to your door.</p></div><div className="mt-10"><DiscoveryFilters /></div><div className="mt-10 grid gap-6 sm:grid-cols-2 lg:grid-cols-3">{result.data.length ? result.data.map((restaurant) => <RestaurantCard key={restaurant.id} restaurant={restaurant} locale={locale} />) : <div className="col-span-full rounded-3xl border border-dashed border-slate-300 p-12 text-center text-slate-500">No restaurants matched your filters.</div>}</div></main>;
}
