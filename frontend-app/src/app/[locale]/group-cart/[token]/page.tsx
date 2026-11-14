import { GroupCartPanel } from '@/ui/group-cart-panel';

export default async function SharedGroupCartPage({ params }: { params: Promise<{ token: string }> }) { return <main className="px-6 py-16"><GroupCartPanel token={(await params).token} /></main>; }
