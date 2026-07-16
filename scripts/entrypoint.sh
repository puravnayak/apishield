#!/bin/sh

# Start the Webhook Worker in the background
echo "🚀 Starting APIShield Webhook Worker..."
./worker &

# Start the API Gateway in the foreground
echo "🚀 Starting APIShield API Gateway..."
exec ./gateway