import { OrderTrackingClient } from './tracking-client';

export default async function OrderTrackingPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <OrderTrackingClient orderId={id} />;
}
