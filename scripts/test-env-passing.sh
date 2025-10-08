#!/bin/bash
# 验证环境变量传递

echo "🔍 验证环境变量传递..."

# 停止现有的 incident-worker
if pgrep -f "incident-worker" > /dev/null; then
    echo "🛑 停止现有的 incident-worker..."
    pkill -f "incident-worker"
    sleep 2
fi

# 编译
echo "🔨 编译 incident-worker..."
if ! go build -o ./bin/incident-worker ./cmd/incident-worker; then
    echo "❌ 编译失败"
    exit 1
fi

# 测试环境变量传递
echo "🧪 测试环境变量传递..."
echo "使用 env 命令启动 incident-worker 并检查环境变量..."

# 启动 incident-worker
env \
QPROXY_WS_URL=http://127.0.0.1:7682/ws \
QPROXY_WS_USER=demo \
QPROXY_WS_PASS=password123 \
QPROXY_WS_POOL=1 \
QPROXY_CONV_ROOT=./conversations \
QPROXY_SOPMAP_PATH=./conversations/_sopmap.json \
QPROXY_HTTP_ADDR=:8080 \
QPROXY_WS_INSECURE_TLS=0 \
nohup ./bin/incident-worker > ./logs/incident-worker-test.log 2>&1 &

WORKER_PID=$!
echo "incident-worker PID: $WORKER_PID"

# 等待启动
sleep 5

# 检查环境变量
echo "📋 检查环境变量传递..."
if [ -f "/proc/$WORKER_PID/environ" ]; then
    echo "QPROXY_WS_URL: $(tr '\0' '\n' < /proc/$WORKER_PID/environ 2>/dev/null | grep '^QPROXY_WS_URL=' | cut -d'=' -f2 || echo '未设置')"
    echo "QPROXY_WS_USER: $(tr '\0' '\n' < /proc/$WORKER_PID/environ 2>/dev/null | grep '^QPROXY_WS_USER=' | cut -d'=' -f2 || echo '未设置')"
    echo "QPROXY_WS_PASS: $(tr '\0' '\n' < /proc/$WORKER_PID/environ 2>/dev/null | grep '^QPROXY_WS_PASS=' | cut -d'=' -f2 || echo '未设置')"
else
    echo "❌ 无法读取进程环境变量"
fi

# 检查日志
echo ""
echo "📝 检查启动日志..."
tail -20 ./logs/incident-worker-test.log

# 测试健康检查
echo ""
echo "🧪 测试健康检查..."
if curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "✅ 健康检查通过"
else
    echo "❌ 健康检查失败"
fi

# 测试 incident 端点
echo ""
echo "🧪 测试 incident 端点..."
RESPONSE=$(curl -s -X POST http://127.0.0.1:8080/incident \
    -H 'content-type: application/json' \
    -d '{"incident_key":"test-auth","prompt":"Hello"}')

if echo "$RESPONSE" | grep -q "not authenticated"; then
    echo "❌ 认证失败: $RESPONSE"
elif echo "$RESPONSE" | grep -q "broken pipe"; then
    echo "❌ 连接问题: $RESPONSE"
elif echo "$RESPONSE" | grep -q "error\|failed"; then
    echo "⚠️  其他错误: $RESPONSE"
else
    echo "✅ 测试成功: $RESPONSE"
fi

# 清理
echo ""
echo "🛑 停止测试进程..."
kill $WORKER_PID