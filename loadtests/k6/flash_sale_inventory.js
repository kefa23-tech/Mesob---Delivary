import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';
import { API, authHeaders, required, requireTokens } from './common.js';

const created = new Counter('flash_sale_created');
const depleted = new Counter('flash_sale_stock_conflict');
const invalid = new Counter('flash_sale_unexpected');

export const options = {
  scenarios: { last_items_race: { executor: 'per-vu-iterations', vus: 200, iterations: 1, maxDuration: '2m' } },
  thresholds: { flash_sale_created: ['count==5'], flash_sale_stock_conflict: ['count==195'], flash_sale_unexpected: ['count==0'] },
};

export default function () {
  requireTokens(200);
  const itemId = required('ITEM_ID');
  const body = JSON.stringify({ address_id: required('ADDRESS_ID'), payment_method: __ENV.PAYMENT_METHOD || 'COD', items: [{ menu_item_id: itemId, quantity: 1 }] });
  const response = http.post(`${API}/orders/`, body, { headers: { ...authHeaders(__VU - 1), 'Idempotency-Key': `flash-${__VU}-${__ITER}` }, tags: { name: 'flash_sale_checkout' } });
  if (response.status === 201) created.add(1);
  else if (response.status === 409 || response.status === 422) depleted.add(1);
  else invalid.add(1);
  check(response, { 'checkout is created or rejected for depleted stock': (r) => r.status === 201 || r.status === 409 || r.status === 422 });
  sleep(0.01);
}
