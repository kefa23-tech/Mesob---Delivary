'use client';

import dynamic from 'next/dynamic';
import { useEffect, useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocale } from 'next-intl';
import Link from 'next/link';
import { Check, MapPin, Phone, Radio, Bike } from 'lucide-react';
import { getTrackingSnapshot, openTrackingStream, type OrderEvent, type TrackingSnapshot } from '@/infrastructure/api/tracking';

const TrackingMap=dynamic(()=>import('./tracking-map'),{ssr:false,loading:()=> <div className="h-[360px] animate-pulse rounded-2xl bg-slate-100" />});
const steps=[{key:'confirmed',label:'Order Confirmed',states:['PAID','CONFIRMED_COD','confirmed','confirmed_cod']},{key:'preparing',label:'Kitchen Preparing',states:['ACCEPTED','PREPARING','READY_FOR_PICKUP','preparing','ready']},{key:'assigned',label:'Courier Assigned',states:['COURIER_ASSIGNED','PICKED_UP','DELIVERED','dispatched','delivered']},{key:'way',label:'On the Way',states:['PICKED_UP','DELIVERED','delivered']},{key:'delivered',label:'Delivered',states:['DELIVERED','delivered']}];

export function OrderTrackingClient({orderId}:{orderId:string}) {
  const locale=useLocale(), qc=useQueryClient(); const [connected,setConnected]=useState(false); const [pin,setPin]=useState(''); const [now,setNow]=useState(Date.now());
  const query=useQuery({queryKey:['order-tracking',orderId],queryFn:()=>getTrackingSnapshot(orderId),refetchInterval:connected?false:5000});
  const order=query.data;
  useEffect(()=>{setPin(sessionStorage.getItem(`mesob_delivery_pin:${orderId}`)??'')},[orderId]);
  useEffect(()=>{const timer=window.setInterval(()=>setNow(Date.now()),1000);return()=>window.clearInterval(timer)},[]);
  useEffect(()=>{
    let stopped=false, controller:AbortController|undefined, retry=1000;
    const run=async()=>{while(!stopped){controller=new AbortController();try{const response=await openTrackingStream(orderId,controller.signal);if(!response.ok||!response.body)throw new Error('stream unavailable');setConnected(true);retry=1000;const reader=response.body.getReader(),decoder=new TextDecoder();let buffer='';
      while(!stopped){const {value,done}=await reader.read();if(done)break;buffer+=decoder.decode(value,{stream:true});let cut;while((cut=buffer.indexOf('\n\n'))>=0){const frame=buffer.slice(0,cut);buffer=buffer.slice(cut+2);let eventName='',data='';for(const line of frame.split('\n')){if(line.startsWith('event:'))eventName=line.slice(6).trim();else if(line.startsWith('data:'))data+=line.slice(5).trim()}if(!data)continue;try{const payload=JSON.parse(data) as TrackingSnapshot|OrderEvent;if(eventName==='snapshot'){qc.setQueryData(['order-tracking',orderId],payload);continue}const evt=payload as OrderEvent;qc.setQueryData<TrackingSnapshot>(['order-tracking',orderId],old=>{if(!old)return old;const next={...old};if(evt.status)next.status=evt.status;if(evt.location){next.courier_lat=evt.location.lat;next.courier_lng=evt.location.lng;next.courier_heading=evt.location.heading;next.eta_at=evt.location.eta_at}return next});if(evt.status)void qc.invalidateQueries({queryKey:['order-tracking',orderId]})}catch{/* ignore malformed event */}}}
      setConnected(false);
    }catch{setConnected(false)}if(!stopped){await new Promise(resolve=>window.setTimeout(resolve,retry));retry=Math.min(retry*2,15000)}}};void run();return()=>{stopped=true;controller?.abort();setConnected(false)}
  },[orderId,qc]);
  const eta=useMemo(()=>{const etaAt=order?.eta_at;if(!etaAt)return null;return Math.max(0,Math.ceil((new Date(etaAt).getTime()-now)/60000))},[order,now]);
  if(query.isLoading)return <main className="mx-auto max-w-4xl p-6"><div className="h-8 w-56 animate-pulse rounded bg-slate-100"/></main>;
  if(query.isError||!order)return <main className="mx-auto max-w-3xl p-8 text-center"><h1 className="text-2xl font-semibold">We couldn’t load this order</h1><p className="mt-2 text-slate-600">Check your connection or return to your orders.</p><Link className="mt-5 inline-block rounded-xl bg-orange-600 px-5 py-3 text-white" href={`/${locale}/orders`}>My orders</Link></main>;
  const status=order.status.toUpperCase();const current=steps.reduce((index,step,i)=>step.states.includes(status)?Math.max(index,i):index,-1);const assigned=Boolean(order.courier_id);
  return <main className="mx-auto max-w-5xl space-y-6 px-4 py-8 sm:px-6">
    <header className="flex flex-wrap items-start justify-between gap-3"><div><p className="text-sm font-medium text-orange-700">Order tracking</p><h1 className="mt-1 text-3xl font-bold">Your food is on its way</h1><p className="mt-2 text-sm text-slate-500">Order #{order.id.slice(0,8)}</p></div><div className="flex items-center gap-2 rounded-full bg-slate-100 px-3 py-2 text-sm"><Radio size={16} className={connected?'text-green-600':'text-slate-400'}/>{connected?'Live updates':'Reconnecting…'}</div></header>
    <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm"><div className="mb-5 flex items-center justify-between"><h2 className="font-semibold">Delivery progress</h2>{eta!==null&&<span className="rounded-full bg-orange-50 px-3 py-1 text-sm font-semibold text-orange-800">ETA {eta} min</span>}</div><ol className="grid gap-3 sm:grid-cols-5">{steps.map((step,index)=><li key={step.key} className="flex items-center gap-3 sm:block"><span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-full ${index<=current?'bg-orange-600 text-white':'bg-slate-100 text-slate-400'}`}>{index<current?<Check size={18}/>:index+1}</span><span className={`text-sm ${index<=current?'font-semibold text-slate-900':'text-slate-500'}`}>{step.label}</span></li>)}</ol></section>
    <section className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm"><div className="flex items-center gap-2 p-4 font-semibold"><MapPin size={18} className="text-orange-600"/>Live delivery map</div><TrackingMap order={order}/><div className="grid gap-3 p-4 text-sm sm:grid-cols-2"><p><b>Restaurant:</b> {order.restaurant_name}</p><p><b>Delivering to:</b> {order.delivery_address}</p></div></section>
    {assigned&&<section className="grid gap-4 md:grid-cols-2"><div className="rounded-2xl border border-orange-200 bg-orange-50 p-5"><p className="text-sm font-semibold text-orange-900">Delivery verification PIN</p>{pin?<p className="mt-2 text-4xl font-black tracking-[.3em] text-orange-950">{pin}</p>:<p className="mt-2 text-sm text-orange-900">Your PIN is available on the device used to place this order.</p>}<p className="mt-2 text-xs text-orange-900">Share this code with your courier only when your order arrives.</p></div><div className="rounded-2xl border border-slate-200 bg-white p-5"><div className="flex items-center gap-3"><span className="flex h-11 w-11 items-center justify-center rounded-full bg-green-100 text-green-700"><Bike/></span><div><p className="text-xs text-slate-500">Your courier</p><p className="font-semibold">{order.courier_name||'Courier assigned'}</p><p className="text-sm capitalize text-slate-600">{order.courier_vehicle||'Delivery vehicle'}</p></div></div>{order.courier_phone&&<a href={`tel:${order.courier_phone}`} className="mt-4 flex w-full items-center justify-center gap-2 rounded-xl bg-green-700 px-4 py-3 font-semibold text-white"><Phone size={17}/>Call courier</a>}</div></section>}
  </main>;
}
