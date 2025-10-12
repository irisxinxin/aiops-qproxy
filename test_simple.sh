#!/bin/bash

set -e

echo "Testing simple incident-worker..."

# 设置环境变量
export Q_BIN=q
export QPROXY_WS_POOL=2
export QPROXY_CONV_ROOT=./conversations
export QPROXY_HTTP_ADDR=:8080
export QPROXY_MEMLOG_SEC=10

# 确保目录存在
mkdir -p conversations

# 启动服务器（后台运行）
echo "Starting incident-worker-simple..."
./bin/incident-worker-simple > logs/test-simple.log 2>&1 &
SERVER_PID=$!

# 保存 PID
echo $SERVER_PID > logs/incident-worker-simple.pid

# 等待服务器启动
echo "Waiting for server to start..."
sleep 5

# 检查服务器是否运行
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "ERROR: Server failed to start"
    cat logs/test-simple.log
    exit 1
fi

echo "Server started with PID: $SERVER_PID"

# 测试健康检查
echo "Testing health check..."
if curl -s http://localhost:8080/healthz | jq . > /dev/null; then
    echo "✓ Health check passed"
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
    "incident_key": "test|simple|cpu|high",
    "prompt": "What is 2+2?"
  }')

if echo "$RESPONSE" | jq -e '.answer' > /dev/null 2>&1; then
    echo "✓ Incident processing test passed"
    echo "Response: $(echo "$RESPONSE" | jq -r '.answer')"
else
    echo "✗ Incident processing test failed"
    echo "Response: $RESPONSE"
fi

# 显示日志的最后几行
echo ""
echo "Recent logs:"
tail -20 logs/test-simple.log

echo ""
echo "Test completed. Server is still running with PID: $SERVER_PID"
echo "To stop the server: kill $SERVER_PID"
echo "To view logs: tail -f logs/test-simple.log"
