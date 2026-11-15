'use client';

import { MapContainer, Marker, TileLayer } from 'react-leaflet';
import L from 'leaflet';
import type { Delivery } from '@/domain/courier';
const icon=(color:string)=>L.divIcon({className:'',html:`<span style="display:block;width:18px;height:18px;border:3px solid white;border-radius:50%;background:${color};box-shadow:0 1px 7px #334155"></span>`,iconSize:[18,18],iconAnchor:[9,9]});
export default function CourierMap({delivery,point}:{delivery:Delivery;point:[number,number]|null}){const restaurant:[number,number]=[delivery.restaurant_lat,delivery.restaurant_lng],customer:[number,number]=[delivery.delivery_lat,delivery.delivery_lng],center=point??restaurant;const all=[restaurant,customer,...(point?[point]:[])];const bounds:[[number,number],[number,number]]=[[Math.min(...all.map(x=>x[0])),Math.min(...all.map(x=>x[1]))],[Math.max(...all.map(x=>x[0])),Math.max(...all.map(x=>x[1]))]];return <MapContainer center={center} bounds={bounds} scrollWheelZoom className="h-72 w-full rounded-xl"><TileLayer attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>' url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"/><Marker position={restaurant} icon={icon('#ea580c')}/><Marker position={customer} icon={icon('#2563eb')}/>{point&&<Marker position={point} icon={icon('#16a34a')}/>}</MapContainer>}
