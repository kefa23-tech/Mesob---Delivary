import { requireRole } from '@/application/auth/require-role';
import { QueryProvider } from '@/ui/query-provider';

export default async function AdminLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  await requireRole(['admin']);
  return <QueryProvider><section className="min-h-screen bg-slate-950 text-slate-100">{children}</section></QueryProvider>;
}
