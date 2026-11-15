import { OrderVerification } from './verification-client';

export default async function VerifyOrderPage({ params }: { params: Promise<{ locale: string; orderId: string }> }) {
  const { orderId } = await params;
  return <main className="mx-auto max-w-3xl px-5 py-14 md:px-8"><OrderVerification orderId={orderId} /></main>;
}
