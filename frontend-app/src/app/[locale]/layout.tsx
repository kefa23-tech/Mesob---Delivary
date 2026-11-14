import { NextIntlClientProvider } from 'next-intl';
import { getLocale, getMessages } from 'next-intl/server';
import Link from 'next/link';
import { QueryProvider } from '@/ui/query-provider';
import '../globals.css';

export default async function LocaleLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const locale = await getLocale();
  const messages = await getMessages();
  return <html lang={locale} dir="ltr"><body><NextIntlClientProvider messages={messages}><QueryProvider><header className="border-b border-slate-200/80 bg-white/90 backdrop-blur"><div className="mx-auto flex h-20 max-w-7xl items-center justify-between px-6"><Link href={`/${locale}`} className="text-xl font-black tracking-tight text-slate-950"><span className="text-emerald-600">●</span> Mesob</Link><nav className="flex items-center gap-5 text-sm font-semibold text-slate-600"><Link href={`/${locale}/restaurants`} className="hover:text-emerald-700">Discover</Link><Link href={`/${locale}/auth/login`} className="rounded-full bg-slate-950 px-4 py-2 text-white hover:bg-slate-800">Sign in</Link></nav></div></header>{children}</QueryProvider></NextIntlClientProvider></body></html>;
}
