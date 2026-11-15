'use client';

import { useEffect, useRef, useState } from 'react';
import { MapContainer, Marker, TileLayer, useMap } from 'react-leaflet';
import L from 'leaflet';
import type { TrackingSnapshot } from '@/infrastructure/api/tracking';

const pin = (color: string) => L.divIcon({ className: '', html: `<div style="width:18px;height:18px;border:3px solid white;border-radius:50%;background:${color};box-shadow:0 1px 6px #334155"></div>`, iconSize: [18,18], iconAnchor: [9,9] });
const restaurantPin=pin('#ea580c'), customerPin=pin('#2563eb'), courierPin=pin('#16a34a');
function SmoothCourier({ point }: { point: [number,number] }) { const [display,setDisplay]=useState(point);const current=useRef(point); useEffect(()=>{let frame=0;const start=current.current,started=performance.now(),duration=700;const step=(now:number)=>{const t=Math.min(1,(now-started)/duration),next:[number,number]=[start[0]+(point[0]-start[0])*t,start[1]+(point[1]-start[1])*t];current.current=next;setDisplay(next);if(t<1)frame=requestAnimationFrame(step)};frame=requestAnimationFrame(step);return()=>cancelAnimationFrame(frame)},[point[0],point[1]]);return <Marker position={display} icon={courierPin}/> }

export default function TrackingMap({ order }: { order: TrackingSnapshot }) {
  const courier: [number,number] | null = order.courier_lat == null || order.courier_lng == null ? null : [order.courier_lat,order.courier_lng];
  const center: [number,number] = courier ?? [order.restaurant_lat,order.restaurant_lng];
  const points=[center,[order.restaurant_lat,order.restaurant_lng] as [number,number],[order.delivery_lat,order.delivery_lng] as [number,number]];
  const bounds:[[number,number],[number,number]]=[[Math.min(...points.map(p=>p[0])),Math.min(...points.map(p=>p[1]))],[Math.max(...points.map(p=>p[0])),Math.max(...points.map(p=>p[1]))]];
  return <MapContainer center={center} bounds={bounds} scrollWheelZoom className="h-[360px] w-full rounded-2xl">
    <TileLayer attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors' url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
    <Marker position={[order.restaurant_lat,order.restaurant_lng]} icon={restaurantPin} />
    <Marker position={[order.delivery_lat,order.delivery_lng]} icon={customerPin} />
    {courier && <SmoothCourier point={courier} />}
  </MapContainer>;
}
