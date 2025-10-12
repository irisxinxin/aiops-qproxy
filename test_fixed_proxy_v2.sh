#!/bin/bash

# 测试修复后的 aiops-qproxy
set -e

echo "=== 测试修复后的 aiops-qproxy v2 ==="

# 清理之前的进程
pkill -f incident-worker || true
sleep 2

# 设置环境变量 - 使用 exec 模式
export QPROXY_MODE="exec"
export QPROXY_WS_POOL="2"
export QPROXY_CONV_ROOT="./conversations"
export QPROXY_SOPMAP_PATH="./conversations/_sopmap.json"
export QPROXY_HTTP_ADDR=":8080"
export QPROXY_WARMUP="1"
export QPROXY_MEMLOG_SEC="30"
export Q_BIN="q"

# 确保目录存在
mkdir -p conversations logs

# 启动服务
echo "启动 incident-worker (exec mode)..."
./bin/incident-worker-fixed > logs/test-fixed-v2.log 2>&1 &
WORKER_PID=$!

echo "Worker PID: $WORKER_PID"

# 等待服务启动
echo "等待服务启动..."
sleep 15

# 检查进程是否还在运行
if ! kill -0 $WORKER_PID 2>/dev/null; then
    echo "ERROR: Worker process died during startup"
    cat logs/test-fixed-v2.log
    exit 1
fi

# 检查健康状态
echo "检查健康状态..."
if curl -s --connect-timeout 5 http://localhost:8080/healthz; then
    echo -e "\n健康检查成功"
else
    echo "健康检查失败"
    cat logs/test-fixed-v2.log
    kill $WORKER_PID 2>/dev/null || true
    exit 1
fi

# 检查就绪状态
echo -e "\n检查就绪状态..."
if curl -s --connect-timeout 5 http://localhost:8080/readyz; then
    echo -e "\n就绪检查成功"
else
    echo "就绪检查失败，可能还在预热中..."
fi

# 测试简单请求
echo -e "\n测试简单请求..."
RESPONSE=$(curl -s --connect-timeout 10 -X POST http://localhost:8080/incident \
  -H 'Content-Type: application/json' \
  -d '{
    "incident_key": "test_fixed_proxy_v2",
    "prompt": "Hello, this is a test. Please respond with a simple greeting."
  }')

if [ $? -eq 0 ] && [ -n "$RESPONSE" ]; then
    echo "请求成功，响应："
    echo "$RESPONSE" | jq . 2>/dev/null || echo "$RESPONSE"
else
    echo "请求失败"
    echo "响应: $RESPONSE"
fi

echo -e "\n查看最近的日志..."
tail -20 logs/test-fixed-v2.log

# 清理
echo -e "\n清理进程..."
kill $WORKER_PID 2>/dev/null || true
wait $WORKER_PID 2>/dev/null || true

echo "=== 测试结束 ==="
