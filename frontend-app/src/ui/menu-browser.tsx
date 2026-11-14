'use client';

import { useState } from 'react';
import type { Menu } from '@/infrastructure/api/server';
import { formatETB } from '@/domain/money';
import { CustomizationDrawer } from './customization-drawer';
import { useCartStore } from '@/infrastructure/cart/store';

export function MenuBrowser({ menu }: { menu: Menu }) {
  const [selected, setSelected] = useState<Menu['categories'][number]['items'][number] | null>(null);
  const add = useCartStore((state) => state.add);
  return <>{menu.categories.map((category) => <section key={category.id} className="mt-10"><h2 className="text-2xl font-black">{category.name}</h2><div className="mt-4 grid gap-4 md:grid-cols-2">{category.items.map((item) => <button key={item.id} disabled={!item.is_available} onClick={() => setSelected(item)} className="group flex gap-4 rounded-3xl border border-slate-200 bg-white p-4 text-left shadow-sm transition hover:-translate-y-0.5 hover:shadow-lg disabled:cursor-not-allowed disabled:opacity-50"><div className="flex h-24 w-24 shrink-0 items-center justify-center rounded-2xl bg-gradient-to-br from-amber-200 to-emerald-300 text-4xl">🍽️</div><div className="min-w-0 flex-1"><div className="flex items-start justify-between gap-3"><h3 className="font-bold group-hover:text-emerald-700">{item.name}</h3><span className="whitespace-nowrap text-sm font-bold">{formatETB(item.price_minor)}</span></div><p className="mt-2 line-clamp-2 text-sm leading-6 text-slate-500">{item.description ?? 'Made fresh to order.'}</p><p className="mt-3 text-xs font-bold text-emerald-700">{(item.option_groups ?? []).length ? 'Customize your plate →' : item.is_available ? 'Add to order →' : 'Currently unavailable'}</p></div></button>)}</div></section>)}{selected && <CustomizationDrawer item={selected} onClose={() => setSelected(null)} onAdd={(selections) => add(menu.restaurant_id, selected, selections)} />}</>;
}
