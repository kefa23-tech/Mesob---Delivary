'use client';

import { useQuery } from '@tanstack/react-query';
import { apiClient } from './client';

export function useRestaurants(params: { lat: number; lng: number; cuisine?: string; q?: string; ecoPackaging?: boolean }) {
  return useQuery({
    queryKey: ['restaurants', params],
    queryFn: async () => {
      const { data, error } = await apiClient.GET('/restaurants', { params: { query: {
        lat: params.lat, lng: params.lng, cuisine: params.cuisine || undefined, q: params.q || undefined,
        eco_packaging: params.ecoPackaging, limit: 24,
      } } });
      if (error) throw new Error('Unable to load restaurants');
      return data;
    },
  });
}

export function useRestaurantMenu(id: string) {
  return useQuery({
    queryKey: ['restaurant-menu', id],
    queryFn: async () => {
      const { data, error } = await apiClient.GET('/restaurants/{id}/menu', { params: { path: { id } } });
      if (error) throw new Error('Unable to load menu');
      return data;
    },
    enabled: Boolean(id),
  });
}
