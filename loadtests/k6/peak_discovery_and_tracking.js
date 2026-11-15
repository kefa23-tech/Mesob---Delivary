import http from 'k6/http';
import { check, sleep } from 'k6';
import { API, authHeaders, required } from './common.js';

export const options = {
  scenarios: {
    discovery: { executor: 'constant-vus', vus: Number(__ENV.SEARCH_VUS || 40), duration: __ENV.DURATION || '3m', exec: 'search' },
    courier_gps: { executor: 'constant-vus', vus: Number(__ENV.GPS_VUS || 20), duration: __ENV.DURATION || '3m', exec: 'track' },
  },
  thresholds: { http_req_duration: ['p(95)<150'], http_req_failed: ['rate==0'] },
};

export function search() {
  const query = `lat=${encodeURIComponent(required('LAT'))}&lng=${encodeURIComponent(required('LNG'))}`;
  const response = http.get(`${API}/restaurants?${query}`, { headers: authHeaders(__VU - 1), tags: { name: 'restaurant_discovery' } });
  check(response, { 'restaurant discovery succeeds': (r) => r.status >= 200 && r.status < 300 });
  sleep(0.2);
}

export function track() {
  const headers = authHeaders(__VU - 1);
  const response = http.post(`${API}/courier/location`, JSON.stringify({ lat: Number(required('LAT')), lng: Number(required('LNG')), heading: (__ITER * 17) % 360 }), { headers, tags: { name: 'courier_location' } });
  check(response, { 'courier location accepted': (r) => r.status >= 200 && r.status < 300 });
  sleep(0.2);
}
