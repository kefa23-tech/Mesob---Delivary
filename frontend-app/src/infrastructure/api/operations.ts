'use client';

import { authenticatedFetch } from '@/infrastructure/auth/session';
import type { Delivery } from '@/domain/courier';
export type { Delivery } from '@/domain/courier';

const api = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1';
export type KitchenItem = { item_name: string; quantity: number; options?: { group_name?: string; option_name?: string; name?: string; value?: string; choice_name?: string }[] };
export type KitchenOrder = { id: string; status: string; restaurant_status?: string; payment_method: string; items: KitchenItem[]; placed_at: string; prep_minutes?: number; customer_name: string; customer_phone: string; courier_name?: string; courier_phone?: string; courier_vehicle?: string; pickup_code?: string };
export type CourierDashboard = { online: boolean; vehicle_type: 'bicycle'|'motorbike'|'car'; cash_balance_minor: number; reserved_cash_minor: number; cash_limit_minor: number; active_delivery?: Delivery };
export type OfferDetails = Delivery & { distance_km: number; expires_in_seconds: number };
export class OperationError extends Error { constructor(public status: number, message: string) { super(message); } }
async function request<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const response = await authenticatedFetch(`${api}${path}`, { method, headers: { 'Content-Type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
  if (!response.ok) { const payload = await response.json().catch(() => null); throw new OperationError(response.status, payload?.error?.message ?? `Request failed (${response.status})`); }
  return response.status === 204 ? undefined as T : response.json() as Promise<T>;
}
export async function kitchenOrders() {
  const result = await request<{data: KitchenOrder[]}>('/dashboard/restaurant/orders');
  return { data: normalizeKitchenOrders(result.data) };
}
export function normalizeKitchenOrders(orders: KitchenOrder[]) { return orders.map(order => ({ ...order, items: order.items?.map(item => ({ ...item, options: item.options?.map(option => ({ ...option, name: option.group_name, value: option.option_name })) })) })); }
export const kitchenAction = (id: string, action: 'accept'|'reject'|'preparing'|'ready', data?: unknown) => request(`/dashboard/restaurant/orders/${encodeURIComponent(id)}/${action}`, 'POST', data ?? {});
export const courierDashboard = () => request<CourierDashboard>('/courier');
export const setCourierStatus = (online: boolean, vehicle_type: CourierDashboard['vehicle_type']) => request('/courier/status','POST',{online,vehicle_type});
export const sendCourierLocation = (lat:number,lng:number,heading:number) => request('/courier/location','POST',{lat,lng,heading});
export const courierOffers = () => request<{data: string[]}>('/courier/offers');
export const courierOfferDetails = (id:string) => request<OfferDetails>(`/courier/offers/${encodeURIComponent(id)}`);
export const courierAction = (id:string, action:'accept'|'pickup'|'complete', body?:unknown) => request(`/courier/${action==='accept'?'offers':'deliveries'}/${encodeURIComponent(id)}/${action}`,'POST',body ?? {});

export async function consumeEventStream(url:string, signal:AbortSignal, onEvent:(type:string,data:any)=>void) {
  const response=await authenticatedFetch(`${api}${url}`,{headers:{Accept:'text/event-stream'},signal});
  if(!response.ok || !response.body) throw new OperationError(response.status,'Event stream unavailable');
  const reader=response.body.getReader(), decoder=new TextDecoder(); let buffer='';
  while(true){const {value,done}=await reader.read();if(done)break;buffer+=decoder.decode(value,{stream:true});let split:number;while((split=buffer.indexOf('\n\n'))>=0){const block=buffer.slice(0,split);buffer=buffer.slice(split+2);let type='message',data='';for(const line of block.split(/\r?\n/)){if(line.startsWith('event:'))type=line.slice(6).trim();else if(line.startsWith('data:'))data+=line.slice(5).trim();}if(data){try{onEvent(type,JSON.parse(data));}catch{/* Ignore malformed event frames. */}}}}
}
