'use client';

import Link from 'next/link';
import { useCartStore } from '@/infrastructure/cart/store';
import { formatETB } from '@/domain/money';
import { useTranslations } from 'next-intl';

export function CartSummary({ locale }: { locale: string }) {
  const t = useTranslations('checkout');
  const lines = useCartStore((state) => state.lines); const subtotal = useCartStore((state) => state.subtotal());
  if (!lines.length) return null;
  return <div className="sticky bottom-5 z-20 mx-auto mt-10 flex max-w-xl items-center justify-between rounded-2xl bg-slate-950 px-5 py-4 text-white shadow-2xl"><div><p className="text-xs text-slate-400">{lines.length} item{lines.length === 1 ? '' : 's'}</p><p className="font-bold">{formatETB(subtotal, locale === 'am' ? 'am-ET' : 'en-ET')}</p></div><Link href={`/${locale}/checkout`} className="rounded-xl bg-emerald-500 px-4 py-2 text-sm font-bold hover:bg-emerald-400">{t('placeOrder')} →</Link></div>;
}
