'use client';

import type { components } from './generated';
import { apiClient } from './client';
import { authenticatedFetch } from '@/infrastructure/auth/session';

export type Order = components['schemas']['Order'];
export type DeliveryAddressInput = {
  label: string;
  line1: string;
  landmark: string;
  lat: number;
  lng: number;
};

const apiURL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1';

export async function createDeliveryAddress(address: DeliveryAddressInput): Promise<string> {
  const response = await authenticatedFetch(`${apiURL}/addresses`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(address),
  });
  if (!response.ok) throw new Error('address');
  const result = await response.json() as { id: string } | { address: { id: string } };
  return 'address' in result ? result.address.id : result.id;
}

export async function createCheckoutOrder(input: {
  addressId: string;
  paymentMethod: 'TELEBIRR' | 'CHAPA' | 'COD';
  groupToken?: string;
  items?: components['schemas']['OrderItemInput'][];
  idempotencyKey: string;
}) {
  return apiClient.POST('/orders', {
    params: { header: { 'Idempotency-Key': input.idempotencyKey } },
    body: {
      address_id: input.addressId,
      payment_method: input.paymentMethod,
      ...(input.groupToken ? { group_cart_token: input.groupToken } : { items: input.items ?? [] }),
    },
  });
}

export async function initializeCheckoutPayment(orderId: string, returnURL: string) {
  return apiClient.POST('/payments/{order_id}/initialize', {
    params: { path: { order_id: orderId } }, body: { return_url: returnURL },
  });
}

export async function getCheckoutOrder(orderId: string): Promise<Order> {
  const response = await authenticatedFetch(`${apiURL}/orders/${encodeURIComponent(orderId)}`);
  if (!response.ok) throw new Error(response.status === 404 ? 'not-found' : 'order');
  return response.json() as Promise<Order>;
}

export async function getCustomerCODProfile() {
  const response = await authenticatedFetch(`${apiURL}/profile`);
  if (!response.ok) return null;
  return response.json() as Promise<{ completed_orders?: number; unpaid_cod_cancellations?: number; cod_suspended?: boolean }>;
}
