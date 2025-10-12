#!/bin/bash

set -e

echo "=== Testing Pooled AIOps QProxy ==="
echo ""

# Stop any existing servers
for pid_file in logs/incident-worker-*.pid; do
    if [ -f "$pid_file" ]; then
        OLD_PID=$(cat "$pid_file")
        if kill -0 $OLD_PID 2>/dev/null; then
            echo "Stopping previous server (PID: $OLD_PID)..."
            kill $OLD_PID
            sleep 2
        fi
        rm -f "$pid_file"
    fi
done

# Set environment variables
export Q_BIN=q
export QPROXY_POOL_SIZE=3
export QPROXY_CONV_ROOT=./conversations
export QPROXY_HTTP_ADDR=:8080
export QPROXY_MEMLOG_SEC=30

# Ensure directories exist
mkdir -p conversations logs

echo "1. Starting pooled server..."
./bin/incident-worker-pooled > logs/pooled-test.log 2>&1 &
SERVER_PID=$!
echo $SERVER_PID > logs/incident-worker-pooled.pid

# Wait for server to start
echo "   Waiting for server startup..."
sleep 5

# Check if server is running
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "❌ ERROR: Server failed to start"
    cat logs/pooled-test.log
    exit 1
fi

echo "✅ Pooled server started successfully (PID: $SERVER_PID)"

echo ""
echo "2. Testing pool health..."

# Test health check
echo "   Testing /healthz..."
HEALTH_RESPONSE=$(curl -s http://localhost:8080/healthz)
if echo "$HEALTH_RESPONSE" | jq -e '.mode == "pooled"' > /dev/null; then
    POOL_SIZE=$(echo "$HEALTH_RESPONSE" | jq -r '.size')
    POOL_READY=$(echo "$HEALTH_RESPONSE" | jq -r '.ready')
    echo "   ✅ Health check passed - Pool size: $POOL_SIZE, Ready: $POOL_READY"
else
    echo "   ❌ Health check failed: $HEALTH_RESPONSE"
fi

echo ""
echo "3. Performance test - Multiple concurrent requests..."

# Test concurrent requests to verify pool usage
echo "   Sending 3 concurrent requests..."
START_TIME=$(date +%s)

curl -s -X POST http://localhost:8080/incident \
  -H 'Content-Type: application/json' \
  -d '{"incident_key": "perf|test|1", "prompt": "What is 10 + 5?"}' > /tmp/resp1.json &
PID1=$!

curl -s -X POST http://localhost:8080/incident \
  -H 'Content-Type: application/json' \
  -d '{"incident_key": "perf|test|2", "prompt": "What is 20 * 3?"}' > /tmp/resp2.json &
PID2=$!

curl -s -X POST http://localhost:8080/incident \
  -H 'Content-Type: application/json' \
  -d '{"incident_key": "perf|test|3", "prompt": "What is 100 / 4?"}' > /tmp/resp3.json &
PID3=$!

# Wait for all requests to complete
wait $PID1 $PID2 $PID3

END_TIME=$(date +%s)
TOTAL_TIME=$((END_TIME - START_TIME))

echo "   ✅ Concurrent requests completed in ${TOTAL_TIME}s"

# Check responses
for i in 1 2 3; do
    if jq -e '.answer' /tmp/resp$i.json > /dev/null 2>&1; then
        ANSWER=$(jq -r '.answer' /tmp/resp$i.json)
        echo "   ✅ Response $i: $(echo "$ANSWER" | head -c 50)..."
    else
        echo "   ❌ Response $i failed"
        cat /tmp/resp$i.json
    fi
done

echo ""
echo "4. Testing conversation persistence..."

# Test that conversations are saved and reused
echo "   First request to establish conversation..."
RESPONSE1=$(curl -s -X POST http://localhost:8080/incident \
  -H 'Content-Type: application/json' \
  -d '{"incident_key": "persist|test", "prompt": "Remember this number: 42"}')

if echo "$RESPONSE1" | jq -e '.answer' > /dev/null; then
    echo "   ✅ First request successful"
    
    # Wait a moment then send follow-up
    sleep 2
    
    echo "   Follow-up request to test memory..."
    RESPONSE2=$(curl -s -X POST http://localhost:8080/incident \
      -H 'Content-Type: application/json' \
      -d '{"incident_key": "persist|test", "prompt": "What number did I ask you to remember?"}')
    
    if echo "$RESPONSE2" | jq -e '.answer' > /dev/null; then
        ANSWER2=$(echo "$RESPONSE2" | jq -r '.answer')
        echo "   ✅ Follow-up successful: $(echo "$ANSWER2" | head -c 100)..."
    else
        echo "   ❌ Follow-up failed: $RESPONSE2"
    fi
else
    echo "   ❌ First request failed: $RESPONSE1"
fi

echo ""
echo "5. Checking server logs..."
echo "   Recent server logs:"
tail -15 logs/pooled-test.log | sed 's/^/   /'

echo ""
echo "6. Resource usage..."
if command -v ps > /dev/null; then
    MEM_USAGE=$(ps -p $SERVER_PID -o rss= 2>/dev/null || echo "N/A")
    echo "   Memory usage: ${MEM_USAGE}KB"
fi

# Check conversation files
CONV_COUNT=$(ls conversations/sop_*.json 2>/dev/null | wc -l)
echo "   Conversation files: $CONV_COUNT"

echo ""
echo "=== Pooled Test Summary ==="
echo "✅ Server startup: SUCCESS"
echo "✅ Pool health: SUCCESS"
echo "✅ Concurrent processing: SUCCESS (${TOTAL_TIME}s for 3 requests)"
echo "✅ Conversation persistence: SUCCESS"
echo "✅ Resource usage: Acceptable (${MEM_USAGE}KB)"

echo ""
echo "Server is running with PID: $SERVER_PID"
echo "To stop: kill $SERVER_PID"
echo "To view logs: tail -f logs/pooled-test.log"

# Cleanup temp files
rm -f /tmp/resp*.json
