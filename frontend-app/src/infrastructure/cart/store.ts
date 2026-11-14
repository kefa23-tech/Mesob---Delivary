'use client';

import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { MenuItem } from '@/domain/menu';

export type CartOption = { group_id: string; group_name: string; option_id: string; option_name: string; price_delta_minor: number };
export type CartLine = { id: string; restaurantId: string; menuItemId: string; name: string; unitPriceMinor: number; quantity: number; options: CartOption[]; totalMinor: number };
type CartState = { restaurantId?: string; lines: CartLine[]; add: (restaurantId: string, item: MenuItem, selections: Record<string, string[]>) => void; remove: (id: string) => void; clear: () => void; subtotal: () => number };

export const useCartStore = create<CartState>()(persist((set, get) => ({
  lines: [],
  add: (restaurantId, item, selections) => set((state) => { if (state.restaurantId && state.restaurantId !== restaurantId) return state; const options = (item.option_groups ?? []).flatMap((group) => group.options.filter((option) => (selections[group.id] ?? []).includes(option.id)).map((option) => ({ group_id: group.id, group_name: group.name, option_id: option.id, option_name: option.name, price_delta_minor: option.price_delta }))); const totalMinor = item.price_minor + options.reduce((sum, option) => sum + option.price_delta_minor, 0); return { restaurantId: state.restaurantId ?? restaurantId, lines: [...state.lines, { id: `${item.id}-${Date.now()}`, restaurantId: state.restaurantId ?? restaurantId, menuItemId: item.id, name: item.name, unitPriceMinor: item.price_minor, quantity: 1, options, totalMinor }] }; }),
  remove: (id) => set((state) => ({ lines: state.lines.filter((line) => line.id !== id) })),
  clear: () => set({ restaurantId: undefined, lines: [] }),
  subtotal: () => get().lines.reduce((sum, line) => sum + line.totalMinor * line.quantity, 0),
}), { name: 'mesob-cart' }));
