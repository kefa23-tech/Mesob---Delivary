'use client';

import { authenticatedFetch } from '@/infrastructure/auth/session';

const apiURL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1';
export type TrackingSnapshot = {
  id: string; status: string; payment_status: string; restaurant_status?: string;
  restaurant_id: string; restaurant_name: string; restaurant_lat: number; restaurant_lng: number;
  delivery_lat: number; delivery_lng: number; delivery_address: string;
  courier_id?: string; courier_name?: string; courier_phone?: string; courier_vehicle?: string;
  courier_lat?: number; courier_lng?: number; courier_heading?: number;
  eta_at?: string;
};
export type OrderEvent = { type: string; order_id?: string; status?: string; location?: { lat: number; lng: number; heading: number; eta_at: string; eta_seconds: number } };

export async function getTrackingSnapshot(orderId: string): Promise<TrackingSnapshot> {
  const response = await authenticatedFetch(`${apiURL}/orders/${encodeURIComponent(orderId)}`);
  if (!response.ok) throw new Error(response.status === 404 ? 'not-found' : 'tracking');
  return response.json() as Promise<TrackingSnapshot>;
}

export async function openTrackingStream(orderId: string, signal: AbortSignal): Promise<Response> {
  return authenticatedFetch(`${apiURL}/orders/${encodeURIComponent(orderId)}/stream`, { headers: { Accept: 'text/event-stream' }, signal });
}
