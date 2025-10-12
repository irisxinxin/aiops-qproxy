#!/bin/bash

# 测试修复后的 aiops-qproxy
set -e

echo "=== 测试修复后的 aiops-qproxy ==="

# 设置环境变量
export QPROXY_MODE="exec-pool"
export QPROXY_WS_POOL="2"
export QPROXY_CONV_ROOT="./conversations"
export QPROXY_SOPMAP_PATH="./conversations/_sopmap.json"
export QPROXY_SOP_DIR="./ctx/sop"
export QPROXY_HTTP_ADDR=":8080"
export QPROXY_WARMUP="1"
export QPROXY_MEMLOG_SEC="30"
export QPROXY_QSTREAM_DEBUG="1"
export Q_BIN="q"

# 启动服务
echo "启动 incident-worker..."
./bin/incident-worker > logs/test-fixed.log 2>&1 &
WORKER_PID=$!

echo "Worker PID: $WORKER_PID"

# 等待服务启动
echo "等待服务启动..."
sleep 10

# 检查健康状态
echo "检查健康状态..."
curl -s http://localhost:8080/healthz | jq .

# 测试简单请求
echo "测试简单请求..."
curl -s -X POST http://localhost:8080/incident \
  -H 'Content-Type: application/json' \
  -d '{
    "incident_key": "test_fixed_proxy",
    "prompt": "Hello, this is a test. Please respond with a simple greeting."
  }' | jq .

echo "测试完成，查看日志..."
tail -20 logs/test-fixed.log

# 清理
echo "清理进程..."
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

echo "=== 测试结束 ==="
