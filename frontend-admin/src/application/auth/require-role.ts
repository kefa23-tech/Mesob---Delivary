import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';

export async function requireRole(allowed: string[]) {
  const role = (await cookies()).get('mesob_role')?.value;
  if (!role || !allowed.includes(role)) redirect('/en/login');
  return role;
}
