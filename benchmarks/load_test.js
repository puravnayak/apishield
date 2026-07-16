import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 500 }, // Ramp-up to 500 Virtual Users over 30s
    { duration: '2m', target: 500 },  // Plateau: sustain 500 VUs for 2 minutes
  ],
};

export default function () {
  const url = 'http://localhost:8080/api/v1/payments';
  
  const payload = JSON.stringify({
    amount: 299.0,
    currency: 'usd',
    description: 'Stress test payment run'
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': 'pro-key',
    },
  };

  const res = http.post(url, payload, params);

  check(res, {
    'status is 200 or 429': (r) => r.status === 200 || r.status === 429,
  });

  // Small delay to simulate real client request pacing
  sleep(0.1);
}
