import { requireRole } from '@/application/auth/require-role';

export default async function AdminLayout({ children }: Readonly<{ children: React.ReactNode }>) { await requireRole(['admin', 'operations']); return <section className="min-h-screen bg-slate-950 text-slate-100"><header className="border-b border-slate-800 px-8 py-5"><span className="font-semibold">Mesob Platform Operations</span></header><main className="mx-auto max-w-7xl px-8 py-8">{children}</main></section>; }
