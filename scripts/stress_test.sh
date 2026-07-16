#!/usr/bin/env bash
set -euo pipefail

echo "========================================="
echo " APIShield Stress Test Automated Suite   "
echo "========================================="

# Ensure benchmarks directory exists
mkdir -p benchmarks

# Start CPU profile download in the background after a 10-second delay
(
  echo "Waiting 10s for stress load to build..."
  sleep 10
  echo "Capturing 30-second Go CPU profile..."
  if curl -s -o benchmarks/cpu.prof "http://localhost:6060/debug/pprof/profile?seconds=30"; then
    echo "✔ CPU profile successfully saved to benchmarks/cpu.prof"
  else
    echo "✘ Warning: Failed to download CPU profile. Ensure Gateway is running with '-profile' flag on port 6060."
  fi
) &
BG_PID=$!

echo "Starting k6 load test..."
k6 run benchmarks/load_test.js --summary-export=benchmarks/summary.json

# Wait for background profile capture to complete
wait $BG_PID || true

echo "========================================="
echo " Stress Test Completed successfully.     "
echo " - Summary export: benchmarks/summary.json"
echo " - Go CPU profile: benchmarks/cpu.prof"
echo "========================================="
