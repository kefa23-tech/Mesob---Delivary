import type { components } from './generated';

export type Restaurant = components['schemas']['Restaurant'];
export type RestaurantPage = components['schemas']['RestaurantPage'];
export type Menu = components['schemas']['Menu'];

const apiURL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1';

export async function getRestaurants(params: URLSearchParams): Promise<RestaurantPage> {
  const response = await fetch(`${apiURL}/restaurants?${params.toString()}`, { next: { revalidate: 30 } });
  if (!response.ok) return { restaurants: [] };
  return response.json() as Promise<RestaurantPage>;
}

export async function getMenu(id: string): Promise<Menu | null> {
  const response = await fetch(`${apiURL}/restaurants/${encodeURIComponent(id)}/menu`, { next: { revalidate: 60 } });
  if (!response.ok) return null;
  return response.json() as Promise<Menu>;
}
