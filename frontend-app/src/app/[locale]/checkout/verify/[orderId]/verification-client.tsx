'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import { useEffect, useState } from 'react';
import { CheckCircle2, CircleAlert, LoaderCircle, RefreshCw } from 'lucide-react';
import { formatETB } from '@/domain/money';
import { getCheckoutOrder } from '@/infrastructure/api/checkout';
import { useCartStore } from '@/infrastructure/cart/store';

const VERIFY_TIMEOUT_MS = 5 * 60_000;

export function OrderVerification({ orderId }: { orderId: string }) {
  const t = useTranslations('verification');
  const locale = useLocale();
  const [startedAt, setStartedAt] = useState(() => Date.now());
  const [timedOut, setTimedOut] = useState(false);
  const clearCart = useCartStore((state) => state.clear);
  useEffect(() => {
    const timeout = window.setTimeout(() => setTimedOut(true), VERIFY_TIMEOUT_MS);
    return () => window.clearTimeout(timeout);
  }, [startedAt]);
  const orderQuery = useQuery({
    queryKey: ['order-verification', orderId],
    queryFn: async () => {
      return getCheckoutOrder(orderId);
    },
    retry: (failureCount, error) => error instanceof Error && error.message !== 'timeout' && failureCount < 3,
    retryDelay: (attempt) => Math.min(3_000 * (2 ** attempt), 30_000),
    refetchInterval: (query) => {
      if (Date.now() - startedAt > VERIFY_TIMEOUT_MS) return false;
      if (query.state.data && query.state.data.payment_status !== 'pending_payment') return false;
      const failures = query.state.fetchFailureCount;
      return Math.min(3_000 * (2 ** failures), 30_000);
    },
    refetchIntervalInBackground: true,
  });
  const order = orderQuery.data;
  const isFailed = order?.payment_status === 'payment_failed' || order?.status === 'cancelled';
  const isCOD = order?.payment_method === 'COD';
  const isConfirmed = Boolean(order && (order.payment_status === 'paid' || isCOD));
  const localeTag = locale === 'am' ? 'am-ET' : 'en-ET';
  useEffect(() => { if (isConfirmed) { clearCart(); sessionStorage.removeItem('mesob_checkout_attempt'); } }, [clearCart, isConfirmed]);

  function retryVerification() {
    setStartedAt(Date.now());
    setTimedOut(false);
    void orderQuery.refetch();
  }

  if (isConfirmed && order) return <section className="rounded-[2rem] border border-emerald-100 bg-white p-8 text-center shadow-xl shadow-emerald-950/5 md:p-12"><span className="mx-auto grid h-16 w-16 place-items-center rounded-full bg-emerald-100 text-emerald-700"><CheckCircle2 size={34} /></span><p className="mt-6 text-sm font-bold uppercase tracking-[.18em] text-emerald-700">{isCOD ? t('codConfirmed') : t('paid')}</p><h1 className="mt-2 text-3xl font-black text-slate-950">{t('thankYou')}</h1><p className="mt-3 text-slate-500">{t('orderNumber', { id: order.id.slice(0, 8).toUpperCase() })}</p><div className="mx-auto mt-8 max-w-sm rounded-2xl bg-slate-50 p-5 text-left"><p className="flex justify-between text-sm"><span className="text-slate-500">{t('paymentMethod')}</span><b>{order.payment_method}</b></p><p className="mt-3 flex justify-between border-t border-slate-200 pt-3"><span className="font-bold">{t('total')}</span><b>{formatETB(order.total_minor, localeTag)}</b></p></div><Link href={`/${locale}/orders/${order.id}`} className="mt-7 inline-flex rounded-xl bg-slate-950 px-5 py-3 font-bold text-white">Track your order</Link></section>;

  if (isFailed || timedOut || orderQuery.isError) return <section className="rounded-[2rem] border border-amber-100 bg-white p-8 text-center shadow-xl shadow-slate-950/5 md:p-12"><span className="mx-auto grid h-16 w-16 place-items-center rounded-full bg-amber-100 text-amber-800"><CircleAlert size={32} /></span><h1 className="mt-6 text-3xl font-black text-slate-950">{isFailed ? t('paymentFailed') : timedOut ? t('timedOut') : t('couldNotVerify')}</h1><p className="mx-auto mt-3 max-w-md text-slate-500">{isFailed ? t('failedDescription') : t('retryDescription')}</p><div className="mt-7 flex flex-wrap justify-center gap-3"><button onClick={retryVerification} className="inline-flex items-center gap-2 rounded-xl bg-emerald-600 px-5 py-3 font-bold text-white"><RefreshCw size={17} />{t('retryVerification')}</button><Link href={`/${locale}/checkout`} className="rounded-xl border border-slate-200 px-5 py-3 font-bold text-slate-700">{t('changePayment')}</Link></div></section>;

  return <section className="rounded-[2rem] border border-slate-200 bg-white p-8 text-center shadow-xl shadow-slate-950/5 md:p-12"><span className="mx-auto grid h-16 w-16 place-items-center rounded-full bg-emerald-50 text-emerald-700"><LoaderCircle size={32} className="animate-spin" /></span><p className="mt-6 text-sm font-bold uppercase tracking-[.18em] text-emerald-700">{t('verifying')}</p><h1 className="mt-2 text-3xl font-black text-slate-950">{t('confirming')}</h1><p className="mx-auto mt-3 max-w-md text-slate-500">{t('pendingDescription')}</p><p className="mt-6 text-xs text-slate-400">{t('reference', { id: orderId.slice(0, 8).toUpperCase() })}</p></section>;
}
