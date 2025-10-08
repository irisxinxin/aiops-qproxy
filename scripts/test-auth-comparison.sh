#!/bin/bash
# 对比手动 curl 和 Go 客户端的认证差异

echo "🔍 对比手动 curl 和 Go 客户端的认证差异..."

# 停止现有的 incident-worker
if pgrep -f "incident-worker" > /dev/null; then
    echo "🛑 停止现有的 incident-worker..."
    pkill -f "incident-worker"
    sleep 2
fi

# 清理 ttyd 日志
echo "📝 清理 ttyd 日志..."
> ./logs/ttyd-q.log

# 测试 1: 手动 curl 认证
echo ""
echo "🧪 测试 1: 手动 curl 认证"
AUTH_HEADER=$(echo -n "demo:password123" | base64)
echo "使用认证头: Authorization: Basic $AUTH_HEADER"

curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $AUTH_HEADER" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!

sleep 3
kill $CURL_PID 2>/dev/null

echo ""
echo "📝 curl 测试后的 ttyd 日志："
tail -10 ./logs/ttyd-q.log | grep -E "auth|Auth|AUTH|credential|not authenticated|WS" || echo "没有发现认证相关日志"

# 清理 ttyd 日志
echo ""
echo "📝 清理 ttyd 日志..."
> ./logs/ttyd-q.log

# 测试 2: Go 客户端认证
echo ""
echo "🧪 测试 2: Go 客户端认证"
echo "启动 incident-worker..."

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
sleep 10

echo ""
echo "📝 Go 客户端测试后的 ttyd 日志："
tail -10 ./logs/ttyd-q.log | grep -E "auth|Auth|AUTH|credential|not authenticated|WS" || echo "没有发现认证相关日志"

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
    -d '{"incident_key":"test-comparison","prompt":"Hello"}')

if echo "$RESPONSE" | grep -q "not authenticated"; then
    echo "❌ 认证失败: $RESPONSE"
elif echo "$RESPONSE" | grep -q "broken pipe"; then
    echo "❌ 连接问题: $RESPONSE"
elif echo "$RESPONSE" | grep -q "error\|failed"; then
    echo "⚠️  其他错误: $RESPONSE"
else
    echo "✅ 测试成功: $RESPONSE"
fi

echo ""
echo "📝 最终 ttyd 日志："
tail -20 ./logs/ttyd-q.log

# 清理
echo ""
echo "🛑 停止测试进程..."
kill $WORKER_PID
