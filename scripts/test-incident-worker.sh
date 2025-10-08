#!/bin/bash
# 测试 incident-worker 状态

echo "🧪 测试 incident-worker 状态..."

# 检查进程
if pgrep -f "incident-worker" > /dev/null; then
    WORKER_PID=$(pgrep -f "incident-worker")
    echo "✅ incident-worker 进程运行中 (PID: $WORKER_PID)"
else
    echo "❌ incident-worker 进程未运行"
    exit 1
fi

# 检查端口
if ss -tlnp | grep -q ":8080 "; then
    echo "✅ 端口 8080 正在监听"
else
    echo "❌ 端口 8080 未监听"
    exit 1
fi

# 测试健康检查
echo "🧪 测试健康检查..."
if curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "✅ 健康检查通过"
else
    echo "❌ 健康检查失败"
    echo "📝 查看最新日志："
    tail -10 ./logs/incident-worker-real.log
    exit 1
fi

# 测试 incident 端点
echo "🧪 测试 incident 端点..."
RESPONSE=$(curl -s -X POST http://127.0.0.1:8080/incident \
    -H 'content-type: application/json' \
    -d '{"incident_key":"test-auth","prompt":"Hello"}')

if echo "$RESPONSE" | grep -q "error\|failed\|broken pipe"; then
    echo "❌ incident 端点测试失败"
    echo "响应: $RESPONSE"
    echo "📝 查看最新日志："
    tail -20 ./logs/incident-worker-real.log
else
    echo "✅ incident 端点测试成功"
    echo "响应: $RESPONSE"
fi
