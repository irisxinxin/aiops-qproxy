#!/bin/bash
# 详细调试认证问题

echo "🔍 详细调试认证问题..."

# 检查当前服务状态
echo "📋 当前服务状态："
echo "ttyd 进程："
ps aux | grep "ttyd.*q chat" | grep -v grep || echo "ttyd 未运行"

echo ""
echo "incident-worker 进程："
ps aux | grep "incident-worker" | grep -v grep || echo "incident-worker 未运行"

# 检查端口
echo ""
echo "📋 端口状态："
if command -v ss >/dev/null 2>&1; then
    ss -tlnp | grep -E ":7682|:8080" || echo "没有相关端口在监听"
else
    netstat -an | grep -E ":7682|:8080" || echo "没有相关端口在监听"
fi

# 检查环境变量
echo ""
echo "📋 incident-worker 环境变量："
if pgrep -f "incident-worker" > /dev/null; then
    WORKER_PID=$(pgrep -f "incident-worker")
    echo "incident-worker PID: $WORKER_PID"
    if [ -f "/proc/$WORKER_PID/environ" ]; then
        echo "QPROXY_WS_URL: $(tr '\0' '\n' < /proc/$WORKER_PID/environ 2>/dev/null | grep '^QPROXY_WS_URL=' | cut -d'=' -f2 || echo '未设置')"
        echo "QPROXY_WS_USER: $(tr '\0' '\n' < /proc/$WORKER_PID/environ 2>/dev/null | grep '^QPROXY_WS_USER=' | cut -d'=' -f2 || echo '未设置')"
        echo "QPROXY_WS_PASS: $(tr '\0' '\n' < /proc/$WORKER_PID/environ 2>/dev/null | grep '^QPROXY_WS_PASS=' | cut -d'=' -f2 || echo '未设置')"
    fi
fi

# 测试认证头格式
echo ""
echo "🧪 测试认证头格式..."
echo "用户名: demo"
echo "密码: password123"
echo "组合: demo:password123"
AUTH_HEADER=$(echo -n "demo:password123" | base64)
echo "Base64 编码: $AUTH_HEADER"

# 手动测试 WebSocket 连接
echo ""
echo "🧪 手动测试 WebSocket 连接..."
echo "测试 1: 使用 Authorization header"
curl -i -N -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: $(echo -n "test" | base64)" \
     -H "Authorization: Basic $AUTH_HEADER" \
     http://127.0.0.1:7682/ws &
CURL_PID=$!

sleep 3
kill $CURL_PID 2>/dev/null

# 检查 ttyd 日志
echo ""
echo "📝 检查 ttyd 日志..."
echo "=== ttyd 最新日志 ==="
tail -20 ./logs/ttyd-q.log | grep -E "auth|Auth|AUTH|credential|not authenticated|WS|credential" || echo "没有发现认证相关日志"

# 检查 incident-worker 日志
echo ""
echo "📝 检查 incident-worker 日志..."
echo "=== incident-worker 最新日志 ==="
tail -20 ./logs/incident-worker-real.log | grep -E "error|Error|ERROR|auth|Auth|AUTH|not authenticated" || echo "没有发现认证相关日志"

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
    -d '{"incident_key":"debug-auth","prompt":"Hello"}')

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
echo "💡 分析结果："
echo "1. 如果 ttyd 日志显示 'credential: ZGVtbzpwYXNzd29yZDEyMw=='，说明 ttyd 认证配置正确"
echo "2. 如果 incident-worker 环境变量正确，说明环境变量传递正确"
echo "3. 如果手动 curl 测试返回 400 Bad Request，可能是 WebSocket 握手问题"
echo "4. 如果 incident 端点返回 'not authenticated'，说明 WebSocket 连接时认证失败"