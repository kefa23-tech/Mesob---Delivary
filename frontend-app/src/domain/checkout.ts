export const COD_ORDER_LIMIT_MINOR = 200_000;

export type CODProfile = {
  completed_orders?: number;
  unpaid_cod_cancellations?: number;
  cod_suspended?: boolean;
};

export type CODEligibility = { eligible: true } | { eligible: false; reason: 'limit' | 'first_order' | 'unpaid_cancellation' | 'suspended' | 'profile_unavailable' };

export function getCODEligibility(totalMinor: number, profile: CODProfile | null): CODEligibility {
  if (totalMinor > COD_ORDER_LIMIT_MINOR) return { eligible: false, reason: 'limit' };
  if (!profile) return { eligible: false, reason: 'profile_unavailable' };
  if (profile.cod_suspended) return { eligible: false, reason: 'suspended' };
  if ((profile.unpaid_cod_cancellations ?? 0) > 0) return { eligible: false, reason: 'unpaid_cancellation' };
  if ((profile.completed_orders ?? 0) < 1) return { eligible: false, reason: 'first_order' };
  return { eligible: true };
}

export function checkoutTotal(subtotalMinor: number, deliveryFeeMinor: number, groupDiscountMinor = 0) {
  return subtotalMinor + Math.max(0, deliveryFeeMinor - groupDiscountMinor);
}

export function isAddisAbabaCoordinate(latitude: number, longitude: number) {
  return latitude >= 8.8 && latitude <= 9.2 && longitude >= 38.6 && longitude <= 39.0;
}
