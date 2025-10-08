#!/bin/bash
# 手动设置环境变量并启动 incident-worker

echo "🔧 设置环境变量并启动 incident-worker..."

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=3
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

echo "📋 环境变量已设置："
echo "  QPROXY_WS_URL: $QPROXY_WS_URL"
echo "  QPROXY_WS_USER: $QPROXY_WS_USER"
echo "  QPROXY_WS_PASS: $QPROXY_WS_PASS"
echo "  QPROXY_WS_POOL: $QPROXY_WS_POOL"
echo "  QPROXY_CONV_ROOT: $QPROXY_CONV_ROOT"
echo "  QPROXY_SOPMAP_PATH: $QPROXY_SOPMAP_PATH"
echo "  QPROXY_HTTP_ADDR: $QPROXY_HTTP_ADDR"
echo "  QPROXY_WS_INSECURE_TLS: $QPROXY_WS_INSECURE_TLS"

# 检查 incident-worker 是否已存在
if [ -f "./bin/incident-worker" ]; then
    echo "✅ 找到 incident-worker 二进制文件"
else
    echo "❌ incident-worker 二进制文件不存在，请先编译"
    exit 1
fi

# 启动服务
echo "▶️  启动 incident-worker..."
env \
QPROXY_WS_URL=http://127.0.0.1:7682/ws \
QPROXY_WS_USER=demo \
QPROXY_WS_PASS=password123 \
QPROXY_WS_POOL=5 \
QPROXY_CONV_ROOT=./conversations \
QPROXY_SOPMAP_PATH=./conversations/_sopmap.json \
QPROXY_HTTP_ADDR=:8080 \
QPROXY_WS_INSECURE_TLS=0 \
nohup ./bin/incident-worker > ./logs/incident-worker-real.log 2>&1 &
WORKER_PID=$!
echo $WORKER_PID > ./logs/incident-worker-real.pid
echo "incident-worker PID: $WORKER_PID"

# 等待服务启动
sleep 5

# 检查服务状态
if ss -tlnp | grep -q ":8080 "; then
    echo "✅ incident-worker 启动成功"
    echo "🧪 测试健康检查..."
    if curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
        echo "✅ 健康检查通过"
    else
        echo "❌ 健康检查失败"
        echo "📝 查看日志："
        tail -10 ./logs/incident-worker-real.log
    fi
else
    echo "❌ incident-worker 启动失败"
    echo "📝 查看日志："
    cat ./logs/incident-worker-real.log
fi
