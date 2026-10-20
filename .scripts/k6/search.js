// Load test for M3's DoD: p99 < 50ms at 500 requests/s on real data.
//   docker run --rm --network host -v $PWD/scripts/k6:/k6 grafana/k6 run /k6/search.js
// Start the server with PLAYWORD_RATE_LIMIT_RPS=0: every request comes from one IP.
import http from 'k6/http';
import { check } from 'k6';
import { SharedArray } from 'k6/data';

const BASE = __ENV.BASE || 'http://127.0.0.1:18081';
const RATE = Number(__ENV.RATE || 500);
const queries = new SharedArray('queries', () => open('./queries.txt').split('\n').filter(Boolean));
const levels = ['L0', 'L1', 'L2', 'L25', 'L25', 'L25', 'L3'];
const dialects = ['bac', 'bac', 'bac', 'nam', 'trung'];

export const options = {
  scenarios: {
    search: {
      executor: 'constant-arrival-rate',
      rate: RATE, timeUnit: '1s', duration: __ENV.DURATION || '60s',
      preAllocatedVUs: 100, maxVUs: 400,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(99)<50'],
  },
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

const pick = (a) => a[Math.floor(Math.random() * a.length)];

export default function () {
  const q = encodeURIComponent(pick(queries));
  const params = `q=${q}&level=${pick(levels)}&dialect=${pick(dialects)}&min_syl=${pick([1, 1, 2])}`;
  // Mix of what clients do: JSON API, HTMX partials, and full pages.
  const r = Math.random();
  let res;
  if (r < 0.5) {
    res = http.get(`${BASE}/api/v1/rhymes?${params}&limit=50`, { tags: { name: 'api' } });
  } else if (r < 0.85) {
    res = http.get(`${BASE}/search?${params}`, { headers: { 'HX-Request': 'true' }, tags: { name: 'htmx' } });
  } else {
    res = http.get(`${BASE}/?${params}`, { tags: { name: 'page' } });
  }
  check(res, { 'status 200': (x) => x.status === 200 });
}
