#!/bin/bash

# Exit immediately if any command fails
set -e

echo "========================================="
echo " Starting Chaos & Degradation Tests..."
echo "========================================="

# 1. Stop Redis L2 Cache
echo "[Chaos] Stopping apishield-redis container..."
docker stop apishield-redis

echo "[Chaos] Sending request to gateway (should fallback to StubRateLimiter)..."
# Expect HTTP 202 Accepted because the rate limiter falls back to StubRateLimiter when Redis is down
RESPONSE_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/webhooks \
  -H "X-API-Key: pro-key" \
  -H "Content-Type: application/json" \
  -d '{"target_url": "https://httpbin.org/post", "payload": {"chaos": "redis_down"}}')

if [ "$RESPONSE_CODE" -eq 202 ]; then
  echo "✔ Fallback successful! Gateway returned status 202 (Accepted) while Redis is down."
else
  echo "✘ Fallback failed! Gateway returned status $RESPONSE_CODE instead of 202."
  exit 1
fi

echo "[Chaos] Restarting apishield-redis container..."
docker start apishield-redis
sleep 2

# 2. Stop RabbitMQ Message Queue
echo "[Chaos] Stopping apishield-rabbitmq container..."
docker stop apishield-rabbitmq

echo "[Chaos] Sending request to gateway (should return 500 Internal Server Error)..."
# Expect HTTP 500 because the gateway fails to publish event to queue when RabbitMQ is down
RESPONSE_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/webhooks \
  -H "X-API-Key: pro-key" \
  -H "Content-Type: application/json" \
  -d '{"target_url": "https://httpbin.org/post", "payload": {"chaos": "rabbitmq_down"}}')

if [ "$RESPONSE_CODE" -eq 500 ]; then
  echo "✔ Graceful failure successful! Gateway returned status 500 (Internal Server Error) while RabbitMQ is down."
else
  echo "✘ Failure assertion failed! Gateway returned status $RESPONSE_CODE instead of 500."
  exit 1
fi

echo "[Chaos] Restarting apishield-rabbitmq container..."
docker start apishield-rabbitmq
sleep 2

echo "========================================="
echo " Chaos Tests Complete!"
echo "========================================="
