#!/bin/bash

set -e

echo "Testing ultra-simple incident-worker..."

# 停止之前的服务器
if [ -f logs/incident-worker-simple.pid ]; then
    OLD_PID=$(cat logs/incident-worker-simple.pid)
    if kill -0 $OLD_PID 2>/dev/null; then
        echo "Stopping previous server (PID: $OLD_PID)..."
        kill $OLD_PID
        sleep 2
    fi
    rm -f logs/incident-worker-simple.pid
fi

# 设置环境变量
export Q_BIN=q
export QPROXY_CONV_ROOT=./conversations
export QPROXY_HTTP_ADDR=:8080

# 确保目录存在
mkdir -p conversations

# 启动服务器（后台运行）
echo "Starting incident-worker-ultra-simple..."
./bin/incident-worker-ultra-simple > logs/test-ultra-simple.log 2>&1 &
SERVER_PID=$!

# 保存 PID
echo $SERVER_PID > logs/incident-worker-ultra-simple.pid

# 等待服务器启动
echo "Waiting for server to start..."
sleep 3

# 检查服务器是否运行
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "ERROR: Server failed to start"
    cat logs/test-ultra-simple.log
    exit 1
fi

echo "Server started with PID: $SERVER_PID"

# 测试健康检查
echo "Testing health check..."
if curl -s http://localhost:8080/healthz | jq . > /dev/null; then
    echo "✓ Health check passed"
    curl -s http://localhost:8080/healthz | jq .
else
    echo "✗ Health check failed"
    curl -s http://localhost:8080/healthz
fi

# 测试就绪检查
echo "Testing readiness check..."
if curl -s http://localhost:8080/readyz | grep -q "ready"; then
    echo "✓ Readiness check passed"
else
    echo "✗ Readiness check failed"
fi

# 测试简单的事件处理
echo "Testing incident processing..."
RESPONSE=$(curl -s -X POST http://localhost:8080/incident \
  -H 'Content-Type: application/json' \
  -d '{
    "incident_key": "test|ultra|simple|cpu|high",
    "prompt": "What is 2+2? Please give a brief answer."
  }')

echo "Raw response: $RESPONSE"

if echo "$RESPONSE" | jq -e '.answer' > /dev/null 2>&1; then
    echo "✓ Incident processing test passed"
    ANSWER=$(echo "$RESPONSE" | jq -r '.answer')
    echo "Answer: $ANSWER"
else
    echo "✗ Incident processing test failed"
    echo "Response: $RESPONSE"
fi

# 显示日志的最后几行
echo ""
echo "Recent logs:"
tail -20 logs/test-ultra-simple.log

echo ""
echo "Test completed. Server is still running with PID: $SERVER_PID"
echo "To stop the server: kill $SERVER_PID"
echo "To view logs: tail -f logs/test-ultra-simple.log"
