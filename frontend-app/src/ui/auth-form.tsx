'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { apiClient } from '@/infrastructure/api/client';
import { saveSession } from '@/infrastructure/auth/session';
import { Button } from './button';

type Mode = 'login' | 'signup';
type Step = 'credentials' | 'otp';

export function AuthForm({ initialMode = 'login' }: { initialMode?: Mode }) {
  const t = useTranslations('auth');
  const [mode, setMode] = useState(initialMode);
  const [step, setStep] = useState<Step>('credentials');
  const [phone, setPhone] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [whatsapp, setWhatsapp] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function requestOtp() {
    setBusy(true); setError('');
    const { error: requestError } = await apiClient.POST('/auth/otp/request', { body: { phone, channel: whatsapp ? 'whatsapp' : 'sms' } });
    setBusy(false);
    if (requestError) { setError(t('otpError')); return; }
    setStep('otp');
  }

  async function verifyOtp() {
    setBusy(true); setError('');
    const { data, error: verifyError } = await apiClient.POST('/auth/otp/verify', { body: { phone, code } });
    setBusy(false);
    if (verifyError || !data) { setError(t('otpInvalid')); return; }
    saveSession(data); window.location.assign('/restaurants');
  }

  async function login() {
    setBusy(true); setError('');
    const { data, error: loginError } = await apiClient.POST('/auth/login', { body: { email, password } });
    setBusy(false);
    if (loginError || !data) { setError(t('loginError')); return; }
    saveSession(data); window.location.assign('/restaurants');
  }

  function startGoogle() {
    const oauthURL = process.env.NEXT_PUBLIC_GOOGLE_OAUTH_URL;
    if (oauthURL) window.location.assign(oauthURL);
    else setError('Google sign-in is not configured yet.');
  }

  return <div className="w-full max-w-md rounded-3xl border border-slate-200 bg-white p-8 shadow-xl shadow-slate-200/50">
    <div className="mb-8"><p className="text-sm font-semibold uppercase tracking-[0.2em] text-emerald-600">Mesob</p><h1 className="mt-3 text-3xl font-bold tracking-tight text-slate-950">{mode === 'login' ? t('welcomeBack') : t('joinMesob')}</h1><p className="mt-2 text-sm text-slate-500">{t('description')}</p></div>
    {step === 'credentials' ? <div className="space-y-4">
      <div className="grid grid-cols-2 rounded-xl bg-slate-100 p-1 text-sm font-medium"><button onClick={() => setMode('login')} className={`rounded-lg px-3 py-2 ${mode === 'login' ? 'bg-white text-slate-950 shadow-sm' : 'text-slate-500'}`}>{t('login')}</button><button onClick={() => setMode('signup')} className={`rounded-lg px-3 py-2 ${mode === 'signup' ? 'bg-white text-slate-950 shadow-sm' : 'text-slate-500'}`}>{t('signup')}</button></div>
      {mode === 'login' ? <><label className="block text-sm font-medium">{t('email')}<input value={email} onChange={(event) => setEmail(event.target.value)} type="email" autoComplete="email" className="mt-2 w-full rounded-xl border border-slate-200 px-4 py-3 outline-none ring-emerald-500 focus:ring-2" placeholder="you@example.com" /></label><label className="block text-sm font-medium">{t('password')}<input value={password} onChange={(event) => setPassword(event.target.value)} type="password" autoComplete="current-password" className="mt-2 w-full rounded-xl border border-slate-200 px-4 py-3 outline-none ring-emerald-500 focus:ring-2" /></label><Button onClick={login} disabled={busy || !email || !password} className="w-full">{busy ? t('working') : t('continue')}</Button></> : <><label className="block text-sm font-medium">{t('phone')}<input value={phone} onChange={(event) => setPhone(event.target.value)} type="tel" autoComplete="tel" className="mt-2 w-full rounded-xl border border-slate-200 px-4 py-3 outline-none ring-emerald-500 focus:ring-2" placeholder="+251 91 100 0000" /></label><label className="flex items-center gap-3 rounded-xl bg-slate-50 p-3 text-sm text-slate-600"><input checked={whatsapp} onChange={(event) => setWhatsapp(event.target.checked)} type="checkbox" className="h-4 w-4 accent-emerald-600" />{t('whatsapp')}</label><Button onClick={requestOtp} disabled={busy || !phone} className="w-full">{busy ? t('working') : t('sendCode')}</Button></>}
      <button onClick={startGoogle} className="flex w-full items-center justify-center gap-2 rounded-xl border border-slate-200 px-4 py-3 text-sm font-semibold text-slate-700 hover:bg-slate-50"><span className="text-lg">G</span>{t('google')}</button>
      {error && <p role="alert" className="rounded-xl bg-red-50 p-3 text-sm text-red-700">{error}</p>}
    </div> : <div className="space-y-5"><button onClick={() => setStep('credentials')} className="text-sm font-medium text-emerald-700">← {t('back')}</button><div><h2 className="text-xl font-bold">{t('checkPhone')}</h2><p className="mt-1 text-sm text-slate-500">{t('codeSent', { phone })}</p></div><input value={code} onChange={(event) => setCode(event.target.value)} inputMode="numeric" maxLength={6} className="w-full rounded-xl border border-slate-200 px-4 py-4 text-center text-2xl tracking-[0.5em] outline-none ring-emerald-500 focus:ring-2" placeholder="000000" /><Button onClick={verifyOtp} disabled={busy || code.length < 4} className="w-full">{busy ? t('working') : t('verify')}</Button>{error && <p role="alert" className="rounded-xl bg-red-50 p-3 text-sm text-red-700">{error}</p>}</div>}
  </div>;
}
