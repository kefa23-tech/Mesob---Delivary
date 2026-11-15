import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { API, authHeaders, required, requireTokens } from './common.js';

const accepted = new Counter('dispatch_race_accepted');
const conflicted = new Counter('dispatch_race_conflicted');
const unexpected = new Counter('dispatch_race_unexpected');

export const options = {
  scenarios: { simultaneous_claims: { executor: 'per-vu-iterations', vus: 50, iterations: 1, maxDuration: '1m' } },
  thresholds: { dispatch_race_accepted: ['count==1'], dispatch_race_conflicted: ['count==49'], dispatch_race_unexpected: ['count==0'] },
};

export default function () {
  requireTokens(50);
  const orderId = required('ORDER_ID');
  const response = http.post(`${API}/courier/offers/${orderId}/accept`, null, { headers: authHeaders(__VU - 1), tags: { name: 'courier_offer_accept' } });
  if (response.status === 200) accepted.add(1);
  else if (response.status === 409) conflicted.add(1);
  else unexpected.add(1);
  check(response, { 'one winner or conflict': (r) => r.status === 200 || r.status === 409 });
}
