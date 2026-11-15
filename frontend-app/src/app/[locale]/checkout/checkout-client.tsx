'use client';

import { useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useLocale, useTranslations } from 'next-intl';
import { ArrowLeft, Check, MapPin, ShoppingBag, WalletCards } from 'lucide-react';
import Link from 'next/link';
import { formatETB } from '@/domain/money';
import { checkoutTotal, getCODEligibility, isAddisAbabaCoordinate, type CODProfile } from '@/domain/checkout';
import { apiClient } from '@/infrastructure/api/client';
import { createCheckoutOrder, createDeliveryAddress, getCustomerCODProfile, initializeCheckoutPayment, type DeliveryAddressInput } from '@/infrastructure/api/checkout';
import { useCartStore } from '@/infrastructure/cart/store';

type SavedAddress = DeliveryAddressInput & { id: string };
type PaymentMethod = 'TELEBIRR' | 'CHAPA' | 'COD';
const DEFAULT_FEE_MINOR = 15_000;

export function CheckoutClient({ groupToken }: { groupToken?: string }) {
  const t = useTranslations('checkout');
  const locale = useLocale();
  const router = useRouter();
  const lines = useCartStore((state) => state.lines);
  const clearCart = useCartStore((state) => state.clear);
  const localSubtotal = useCartStore((state) => state.subtotal());
  const [savedAddresses, setSavedAddresses] = useState<SavedAddress[]>([]);
  const [selectedAddressId, setSelectedAddressId] = useState('');
  const [address, setAddress] = useState<DeliveryAddressInput>({ label: '', line1: '', landmark: '', lat: 9.0192, lng: 38.7525 });
  const [addingAddress, setAddingAddress] = useState(false);
  const [paymentMethod, setPaymentMethod] = useState<PaymentMethod>('CHAPA');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    try {
      const saved = JSON.parse(localStorage.getItem('mesob_delivery_addresses') ?? '[]') as SavedAddress[];
      setSavedAddresses(saved);
      if (saved[0]) setSelectedAddressId(saved[0].id);
    } catch {
      setSavedAddresses([]);
    }
  }, []);

  const groupQuery = useQuery({
    queryKey: ['checkout-group-cart', groupToken],
    enabled: Boolean(groupToken),
    queryFn: async () => {
      const { data, error: requestError } = await apiClient.GET('/group-carts/{token}', { params: { path: { token: groupToken! } } });
      if (requestError || !data) throw new Error('group-cart');
      return data.cart;
    },
  });
  const profileQuery = useQuery({ queryKey: ['customer-cod-profile'], queryFn: getCustomerCODProfile });

  const cart = groupQuery.data;
  const subtotal = groupToken ? (cart?.subtotal_minor ?? 0) : localSubtotal;
  const deliveryFee = groupToken ? (cart?.delivery_fee_minor ?? DEFAULT_FEE_MINOR) : DEFAULT_FEE_MINOR;
  const deliveryDiscount = groupToken ? (cart?.delivery_discount_minor ?? 0) : 0;
  const total = checkoutTotal(subtotal, deliveryFee, deliveryDiscount);
  const codEligibility = getCODEligibility(total, profileQuery.data ?? null);
  const addressId = selectedAddressId || '';
  const pinInAddis = isAddisAbabaCoordinate(address.lat, address.lng);
  const isCartReady = groupToken ? cart?.status === 'locked' && cart.items.length > 0 : lines.length > 0;

  const formattedSubtotal = useMemo(() => formatETB(subtotal, locale === 'am' ? 'am-ET' : 'en-ET'), [subtotal, locale]);

  function setAddressField<K extends keyof DeliveryAddressInput>(key: K, value: DeliveryAddressInput[K]) {
    setAddress((current) => ({ ...current, [key]: value }));
  }

  async function saveAddress() {
    if (!pinInAddis) { setError(t('addisOnly')); return; }
    if (!address.label.trim() || !address.line1.trim() || !Number.isFinite(address.lat) || !Number.isFinite(address.lng)) return;
    setBusy(true); setError('');
    try {
      const id = await createDeliveryAddress(address);
      const next = [...savedAddresses, { ...address, id }];
      localStorage.setItem('mesob_delivery_addresses', JSON.stringify(next));
      setSavedAddresses(next); setSelectedAddressId(id); setAddingAddress(false);
    } catch {
      setError(t('addressError'));
    } finally { setBusy(false); }
  }

  async function submitCheckout() {
    if (!addressId || !isCartReady || busy || (paymentMethod === 'COD' && !codEligibility.eligible)) return;
    setBusy(true); setError('');
    try {
      const signature = JSON.stringify({ addressId, paymentMethod, groupToken, items: groupToken ? undefined : lines.map(({ menuItemId, quantity, options }) => ({ menuItemId, quantity, options })) });
      const priorAttempt = JSON.parse(sessionStorage.getItem('mesob_checkout_attempt') ?? 'null') as { signature: string; key: string } | null;
      const attemptKey = priorAttempt?.signature === signature ? priorAttempt.key : crypto.randomUUID();
      sessionStorage.setItem('mesob_checkout_attempt', JSON.stringify({ signature, key: attemptKey }));
      const { data: order, error: orderError } = await createCheckoutOrder({
        addressId, paymentMethod, groupToken,
        items: groupToken ? undefined : lines.map((line) => ({ menu_item_id: line.menuItemId, quantity: line.quantity, options: line.options.map((option) => ({ group_id: option.group_id, group_name: option.group_name, option_id: option.option_id, option_name: option.option_name, price_delta_minor: option.price_delta_minor })) })),
        idempotencyKey: attemptKey,
      });
      if (orderError || !order) throw new Error('order');
      if (order.delivery_pin) sessionStorage.setItem(`mesob_delivery_pin:${order.id}`, order.delivery_pin);
      if (paymentMethod === 'COD') {
        router.push(`/${locale}/checkout/verify/${order.id}`);
        return;
      }
      const returnURL = `${window.location.origin}/${locale}/checkout/verify/${order.id}`;
      const { data: payment, error: paymentError } = await initializeCheckoutPayment(order.id, returnURL);
      if (paymentError || !payment?.checkout_url) throw new Error('payment');
      window.location.assign(payment.checkout_url);
    } catch {
      setError(t('checkoutError'));
    } finally { setBusy(false); }
  }

  const codReason = codEligibility.eligible ? '' : profileQuery.isLoading ? t('cod.checking') : t(`cod.${codEligibility.reason}`);

  return <div className="grid items-start gap-8 lg:grid-cols-[1.25fr_.75fr]">
    <section>
      <Link href={groupToken ? `/${locale}/group-cart/${groupToken}` : `/${locale}/group-cart`} className="inline-flex items-center gap-2 text-sm font-bold text-slate-500 hover:text-emerald-700"><ArrowLeft size={16} />{t('backToCart')}</Link>
      <div className="mt-6 flex items-end justify-between"><div><p className="text-sm font-bold uppercase tracking-[.18em] text-emerald-700">{groupToken ? t('sofur') : t('secureCheckout')}</p><h1 className="mt-2 text-4xl font-black tracking-tight text-slate-950">{t('title')}</h1></div><div className="hidden rounded-2xl bg-emerald-50 p-3 text-emerald-700 sm:block"><ShoppingBag size={24} /></div></div>

      <section className="mt-8 rounded-3xl border border-slate-200 bg-white p-6 shadow-sm md:p-8"><div className="flex items-center gap-3"><span className="grid h-9 w-9 place-items-center rounded-full bg-emerald-100 font-bold text-emerald-800">1</span><div><h2 className="text-xl font-black">{t('deliveryAddress')}</h2><p className="text-sm text-slate-500">{t('addisOnly')}</p></div></div>
        {savedAddresses.length > 0 && <div className="mt-6 grid gap-3 sm:grid-cols-2">{savedAddresses.map((saved) => <button key={saved.id} type="button" onClick={() => setSelectedAddressId(saved.id)} className={`rounded-2xl border p-4 text-left transition ${selectedAddressId === saved.id ? 'border-emerald-500 bg-emerald-50 ring-2 ring-emerald-100' : 'border-slate-200 hover:border-slate-300'}`}><span className="flex items-center justify-between font-bold">{saved.label}{selectedAddressId === saved.id && <Check size={17} className="text-emerald-700" />}</span><span className="mt-1 block text-sm text-slate-500">{saved.line1}</span><span className="mt-2 block text-xs text-slate-400">{saved.lat.toFixed(5)}, {saved.lng.toFixed(5)}</span></button>)}</div>}
        <button type="button" onClick={() => setAddingAddress((value) => !value)} className="mt-4 inline-flex items-center gap-2 text-sm font-bold text-emerald-700"><MapPin size={17} />{addingAddress ? t('cancelAddress') : t('newAddress')}</button>
        {addingAddress && <div className="mt-5 grid gap-4 rounded-2xl bg-slate-50 p-5 sm:grid-cols-2"><label className="text-sm font-semibold">{t('addressLabel')}<input value={address.label} onChange={(event) => setAddressField('label', event.target.value)} className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5" placeholder={t('home')} /></label><label className="text-sm font-semibold">{t('streetAddress')}<input value={address.line1} onChange={(event) => setAddressField('line1', event.target.value)} className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5" placeholder={t('streetPlaceholder')} /></label><label className="text-sm font-semibold sm:col-span-2">{t('landmark')}<input value={address.landmark} onChange={(event) => setAddressField('landmark', event.target.value)} className="mt-2 w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5" placeholder={t('landmarkPlaceholder')} /></label><div className="sm:col-span-2"><p className="mb-2 text-sm font-semibold">{t('mapPin')}</p><a target="_blank" rel="noreferrer" href={`https://maps.google.com/?q=${address.lat},${address.lng}`} className="mb-3 inline-flex items-center gap-2 text-xs font-semibold text-emerald-700"><MapPin size={14} />{t('previewPin')}</a><div className="grid grid-cols-2 gap-3"><label className="text-xs font-semibold text-slate-500">{t('latitude')}<input type="number" step="any" value={address.lat} onChange={(event) => setAddressField('lat', Number(event.target.value))} className="mt-1 w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900" /></label><label className="text-xs font-semibold text-slate-500">{t('longitude')}<input type="number" step="any" value={address.lng} onChange={(event) => setAddressField('lng', Number(event.target.value))} className="mt-1 w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900" /></label></div><p className="mt-2 text-xs text-slate-500">{t('mapHint')}</p></div><button type="button" onClick={saveAddress} disabled={busy || !address.label.trim() || !address.line1.trim()} className="rounded-xl bg-slate-950 px-4 py-3 text-sm font-bold text-white disabled:cursor-not-allowed disabled:opacity-50 sm:col-span-2">{busy ? t('saving') : t('saveAddress')}</button></div>}
      </section>

      <section className="mt-5 rounded-3xl border border-slate-200 bg-white p-6 shadow-sm md:p-8"><div className="flex items-center gap-3"><span className="grid h-9 w-9 place-items-center rounded-full bg-emerald-100 font-bold text-emerald-800">2</span><div><h2 className="text-xl font-black">{t('paymentMethod')}</h2><p className="text-sm text-slate-500">{t('choosePayment')}</p></div></div><div className="mt-5 space-y-3">{(['TELEBIRR', 'CHAPA'] as const).map((method) => <button key={method} type="button" onClick={() => setPaymentMethod(method)} className={`flex w-full items-center justify-between rounded-2xl border p-4 text-left transition ${paymentMethod === method ? 'border-emerald-500 bg-emerald-50 ring-2 ring-emerald-100' : 'border-slate-200 hover:border-slate-300'}`}><span className="flex items-center gap-3"><span className="grid h-10 w-10 place-items-center rounded-xl bg-white text-emerald-700"><WalletCards size={20} /></span><span><b className="block">{method === 'TELEBIRR' ? t('telebirr') : t('chapa')}</b><small className="text-slate-500">{t('onlinePayment')}</small></span></span><span className={`h-4 w-4 rounded-full border ${paymentMethod === method ? 'border-[5px] border-emerald-600' : 'border-slate-300'}`} /></button>)}<button type="button" onClick={() => codEligibility.eligible && setPaymentMethod('COD')} aria-disabled={!codEligibility.eligible} className={`w-full rounded-2xl border p-4 text-left transition ${paymentMethod === 'COD' ? 'border-emerald-500 bg-emerald-50 ring-2 ring-emerald-100' : 'border-slate-200'} ${!codEligibility.eligible ? 'cursor-not-allowed bg-slate-50 opacity-75' : 'hover:border-slate-300'}`}><span className="flex items-center justify-between"><span><b className="block">{t('codTitle')}</b><small className="text-slate-500">{t('payAtDoor')}</small></span><span className={`h-4 w-4 rounded-full border ${paymentMethod === 'COD' ? 'border-[5px] border-emerald-600' : 'border-slate-300'}`} /></span>{codReason && <span className="mt-3 block rounded-xl bg-amber-50 px-3 py-2 text-xs font-semibold text-amber-900">{t('unavailable')}: {codReason}</span>}</button></div></section>
    </section>

    <aside className="sticky top-6 rounded-3xl bg-slate-950 p-6 text-white shadow-xl md:p-7"><h2 className="text-xl font-black">{t('summary')}</h2>{groupToken ? <div className="mt-5 space-y-3 border-b border-white/10 pb-5">{cart?.items.map((item) => <div key={item.id} className="flex justify-between gap-3 text-sm"><span className="text-slate-300">{item.name} × {item.quantity}</span><b>{formatETB(item.line_total_minor, locale === 'am' ? 'am-ET' : 'en-ET')}</b></div>)}{groupQuery.isLoading && <p className="text-sm text-slate-400">{t('loadingGroup')}</p>}{groupToken && cart?.status === 'open' && <p className="rounded-xl bg-amber-500/15 p-3 text-xs text-amber-200">{t('groupMustBeLocked')}</p>}</div> : <div className="mt-5 space-y-3 border-b border-white/10 pb-5">{lines.map((line) => <div key={line.id} className="flex justify-between gap-3 text-sm"><span className="text-slate-300">{line.name} × {line.quantity}</span><b>{formatETB(line.totalMinor * line.quantity, locale === 'am' ? 'am-ET' : 'en-ET')}</b></div>)}</div>}<div className="mt-5 space-y-3 text-sm"><p className="flex justify-between"><span className="text-slate-400">{t('subtotal')}</span><b>{formattedSubtotal}</b></p><p className="flex justify-between"><span className="text-slate-400">{t('deliveryFee')}</span><b>{formatETB(deliveryFee, locale === 'am' ? 'am-ET' : 'en-ET')}</b></p>{deliveryDiscount > 0 && <p className="flex justify-between text-emerald-300"><span>{t('sofurDiscount')}</span><b>−{formatETB(deliveryDiscount, locale === 'am' ? 'am-ET' : 'en-ET')}</b></p>}<p className="flex justify-between border-t border-white/10 pt-4 text-lg"><b>{t('total')}</b><b>{formatETB(total, locale === 'am' ? 'am-ET' : 'en-ET')}</b></p></div>{error && <p role="alert" className="mt-4 rounded-xl bg-red-500/15 p-3 text-sm text-red-200">{error}</p>}<button type="button" onClick={submitCheckout} disabled={busy || !addressId || !isCartReady || (paymentMethod === 'COD' && !codEligibility.eligible)} className="mt-6 w-full rounded-xl bg-emerald-400 px-5 py-4 font-black text-slate-950 transition hover:bg-emerald-300 disabled:cursor-not-allowed disabled:opacity-40">{busy ? t('processing') : t('placeOrder')}</button><p className="mt-3 text-center text-xs text-slate-400">{t('secureNote')}</p></aside>
  </div>;
}
