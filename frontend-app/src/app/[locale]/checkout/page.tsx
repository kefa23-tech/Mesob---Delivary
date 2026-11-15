import { CheckoutClient } from './checkout-client';

export default async function CheckoutPage({ searchParams }: { searchParams: Promise<{ group_token?: string }> }) {
  const { group_token: groupToken } = await searchParams;
  return <main className="mx-auto max-w-6xl px-5 py-10 md:px-8 md:py-14"><CheckoutClient groupToken={groupToken} /></main>;
}
