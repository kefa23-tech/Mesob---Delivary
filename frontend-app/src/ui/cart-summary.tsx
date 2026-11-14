'use client';

import Link from 'next/link';
import { useCartStore } from '@/infrastructure/cart/store';
import { formatETB } from '@/domain/money';

export function CartSummary({ locale }: { locale: string }) {
  const lines = useCartStore((state) => state.lines); const subtotal = useCartStore((state) => state.subtotal());
  if (!lines.length) return null;
  return <div className="sticky bottom-5 z-20 mx-auto mt-10 flex max-w-xl items-center justify-between rounded-2xl bg-slate-950 px-5 py-4 text-white shadow-2xl"><div><p className="text-xs text-slate-400">{lines.length} item{lines.length === 1 ? '' : 's'}</p><p className="font-bold">{formatETB(subtotal)}</p></div><Link href={`/${locale}/group-cart`} className="rounded-xl bg-emerald-500 px-4 py-2 text-sm font-bold hover:bg-emerald-400">View cart →</Link></div>;
}
