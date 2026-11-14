'use client';

import { useRouter, useSearchParams } from 'next/navigation';
import { useState } from 'react';

export function DiscoveryFilters() {
  const router = useRouter(); const search = useSearchParams();
  const [q, setQ] = useState(search.get('q') ?? '');
  const [cuisine, setCuisine] = useState(search.get('cuisine') ?? '');
  const [eco, setEco] = useState(search.get('eco_packaging') === 'true');
  function apply() { const params = new URLSearchParams(search.toString()); q ? params.set('q', q) : params.delete('q'); cuisine ? params.set('cuisine', cuisine) : params.delete('cuisine'); eco ? params.set('eco_packaging', 'true') : params.delete('eco_packaging'); router.push(`?${params.toString()}`); }
  return <div className="grid gap-3 rounded-3xl border border-slate-200 bg-white p-4 shadow-sm md:grid-cols-[1.5fr_1fr_auto_auto]"><input value={q} onChange={(event) => setQ(event.target.value)} onKeyDown={(event) => event.key === 'Enter' && apply()} placeholder="Search restaurants or dishes" className="rounded-2xl bg-slate-100 px-4 py-3 text-sm outline-none ring-emerald-500 focus:ring-2" /><select value={cuisine} onChange={(event) => setCuisine(event.target.value)} className="rounded-2xl bg-slate-100 px-4 py-3 text-sm outline-none"><option value="">All cuisines</option><option value="ethiopian">Ethiopian</option><option value="italian">Italian</option><option value="vegan">Vegan</option></select><label className="flex items-center gap-2 rounded-2xl bg-slate-100 px-4 py-3 text-sm"><input checked={eco} onChange={(event) => setEco(event.target.checked)} type="checkbox" className="accent-emerald-600" /> Eco only</label><button onClick={apply} className="rounded-2xl bg-emerald-600 px-5 py-3 text-sm font-bold text-white hover:bg-emerald-700">Search</button></div>;
}
