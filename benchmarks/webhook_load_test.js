import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '10s', target: 50 },  // Ramp-up to 50 Virtual Users over 10s
    { duration: '30s', target: 50 },  // Sustain 50 VUs for 30s
    { duration: '10s', target: 0 },   // Ramp-down
  ],
};

export default function () {
  const url = 'http://localhost:8080/v1/webhooks';
  
  // Alternating between a local path that succeeds and a localhost path that fails
  const targetUrl = __VU % 2 === 0 
    ? 'http://localhost:8080/api/v1/payments' 
    : 'http://localhost:9999/fail';

  const payload = JSON.stringify({
    target_url: targetUrl,
    payload: {
      event: 'webhook.stress_test',
      timestamp: new Date().toISOString(),
      vu: __VU,
      iter: __ITER
    }
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': 'ent-key', // Enterprise key has a higher limit of 5000 req/min
    },
  };

  const res = http.post(url, payload, params);

  check(res, {
    'status is 202 or 429': (r) => r.status === 202 || r.status === 429,
  });

  // Small sleep to control request generation pace
  sleep(0.1);
}
