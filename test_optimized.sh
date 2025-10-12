#!/bin/bash

set -e

# Configuration
export QPROXY_POOL_SIZE=2
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_MEMLOG_SEC=30
export Q_BIN=q
export Q_MCP_TIMEOUT=10

# Ensure directories exist
mkdir -p conversations logs

# Kill any existing instance
pkill -f incident-worker-optimized || true
sleep 2

echo "Starting incident-worker with optimized pool..."
echo "Pool size: $QPROXY_POOL_SIZE"
echo "Conversation root: $QPROXY_CONV_ROOT"
echo "HTTP address: $QPROXY_HTTP_ADDR"

# Start the service in background
nohup ./bin/incident-worker-optimized > logs/optimized-test.log 2>&1 &
PID=$!
echo $PID > logs/incident-worker-optimized.pid

echo "Started incident-worker-optimized (PID: $PID)"
echo "Logs: tail -f logs/optimized-test.log"

# Wait for service to be ready
echo "Waiting for service to be ready..."
for i in {1..90}; do
    if curl -s http://localhost:8080/readyz > /dev/null 2>&1; then
        echo "Service is ready!"
        break
    fi
    if [ $i -eq 90 ]; then
        echo "Service failed to start within 90 seconds"
        echo "Last 20 lines of log:"
        tail -20 logs/optimized-test.log
        exit 1
    fi
    sleep 1
done

# Test health endpoint
echo "Testing health endpoint..."
curl -s http://localhost:8080/healthz | jq .

# Test incident processing
echo "Testing incident processing..."
curl -s -X POST http://localhost:8080/incident \
  -H 'Content-Type: application/json' \
  -d '{"incident_key":"test|optimized|v1","prompt":"What are the key steps for troubleshooting high CPU usage?"}' | jq .

echo "Test completed. Check logs/optimized-test.log for details."
echo "To stop: kill $(cat logs/incident-worker-optimized.pid)"
