import Link from 'next/link';
import type { Restaurant } from '@/infrastructure/api/server';
import { formatETB } from '@/domain/money';

export function RestaurantCard({ restaurant, locale }: { restaurant: Restaurant; locale: string }) {
  return <Link href={`/${locale}/restaurants/${restaurant.id}`} className="group overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm transition hover:-translate-y-1 hover:shadow-xl"><div className="flex h-40 items-end bg-gradient-to-br from-emerald-600 via-teal-500 to-amber-300 p-5"><div className="rounded-2xl bg-white/20 px-3 py-2 text-3xl backdrop-blur">🍲</div></div><div className="p-5"><div className="flex items-start justify-between gap-3"><h3 className="text-lg font-bold text-slate-950 group-hover:text-emerald-700">{restaurant.name}</h3><span className={`mt-1 h-2.5 w-2.5 rounded-full ${restaurant.is_open ? 'bg-emerald-500' : 'bg-slate-300'}`} /></div><p className="mt-2 text-sm text-slate-500">{restaurant.eco_packaging_certified ? 'Eco packaging · ' : ''}Open near you</p><div className="mt-4 flex items-center justify-between text-xs font-semibold text-slate-400"><span>Delivery within {restaurant.delivery_radius_km ?? 5} km</span><span>{formatETB(0)} delivery</span></div></div></Link>;
}
