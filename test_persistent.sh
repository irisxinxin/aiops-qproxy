#!/bin/bash

set -e

# Configuration
export QPROXY_POOL_SIZE=2
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_MEMLOG_SEC=30
export Q_BIN=q

# Ensure directories exist
mkdir -p conversations logs

# Kill any existing instance
if [ -f logs/incident-worker-persistent.pid ]; then
    PID=$(cat logs/incident-worker-persistent.pid)
    if kill -0 $PID 2>/dev/null; then
        echo "Stopping existing incident-worker (PID: $PID)..."
        kill $PID
        sleep 2
    fi
    rm -f logs/incident-worker-persistent.pid
fi

echo "Starting incident-worker with persistent pool..."
echo "Pool size: $QPROXY_POOL_SIZE"
echo "Conversation root: $QPROXY_CONV_ROOT"
echo "HTTP address: $QPROXY_HTTP_ADDR"

# Start the service in background
nohup ./bin/incident-worker-persistent > logs/persistent-test.log 2>&1 &
PID=$!
echo $PID > logs/incident-worker-persistent.pid

echo "Started incident-worker-persistent (PID: $PID)"
echo "Logs: tail -f logs/persistent-test.log"

# Wait for service to be ready
echo "Waiting for service to be ready..."
for i in {1..60}; do
    if curl -s http://localhost:8080/readyz > /dev/null 2>&1; then
        echo "Service is ready!"
        break
    fi
    if [ $i -eq 60 ]; then
        echo "Service failed to start within 60 seconds"
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
  -d '{"incident_key":"test|persistent|v1","prompt":"What is the root cause analysis process?"}' | jq .

echo "Test completed. Check logs/persistent-test.log for details."
echo "To stop: kill $(cat logs/incident-worker-persistent.pid)"
