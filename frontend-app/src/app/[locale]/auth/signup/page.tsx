import { AuthForm } from '@/ui/auth-form';

export default function SignupPage() {
  return <main className="flex min-h-[calc(100vh-5rem)] items-center justify-center px-6 py-12"><AuthForm initialMode="signup" /></main>;
}
