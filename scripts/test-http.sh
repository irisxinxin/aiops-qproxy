#!/bin/bash

# HTTP 服务测试脚本
# 快速测试 HTTP 服务功能

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
HTTP_PORT="${HTTP_PORT:-8081}"

echo "=== HTTP 服务测试 ==="
echo "项目目录: $PROJECT_DIR"
echo "HTTP 端口: $HTTP_PORT"
echo

# 检查端口是否被占用
if lsof -i ":$HTTP_PORT" >/dev/null 2>&1; then
    echo "端口 $HTTP_PORT 已被占用，尝试停止现有服务..."
    pkill -f "qproxy-runner.*$HTTP_PORT" || true
    sleep 2
fi

# 启动 HTTP 服务
echo "--- 启动 HTTP 服务 ---"
cd "$PROJECT_DIR"
Q_SOP_DIR="$PROJECT_DIR/ctx/sop" \
Q_SOP_PREPEND=1 \
Q_BIN="/bin/cat" \
./bin/qproxy-runner --listen=":$HTTP_PORT" &

SERVER_PID=$!
echo "HTTP 服务已启动，PID: $SERVER_PID"

# 等待服务启动
sleep 3

# 测试健康检查
echo "--- 测试健康检查 ---"
if curl -s "http://localhost:$HTTP_PORT/health" | jq '.'; then
    echo "✅ 健康检查通过"
else
    echo "❌ 健康检查失败"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

echo

# 测试告警处理
echo "--- 测试告警处理 ---"
test_alert='{"service":"omada-central","region":"prd-nbu-aps1","category":"latency","severity":"critical"}'

echo "发送测试告警:"
echo "$test_alert" | jq '.'

echo
echo "HTTP 响应:"
response=$(curl -s -X POST "http://localhost:$HTTP_PORT/alert" \
    -H "Content-Type: application/json" \
    -d "$test_alert")

echo "$response" | jq '.' 2>/dev/null || echo "$response"

# 检查响应
if echo "$response" | jq -e '.success' >/dev/null 2>&1; then
    echo "✅ 告警处理成功"
else
    echo "❌ 告警处理失败"
fi

echo

# 测试多个告警
echo "--- 测试多个告警 ---"
for alert_file in "$PROJECT_DIR/alerts/dev"/*.json; do
    if [ -f "$alert_file" ]; then
        alert_name="$(basename "$alert_file" .json)"
        echo "测试告警: $alert_name"
        
        response=$(curl -s -X POST "http://localhost:$HTTP_PORT/alert" \
            -H "Content-Type: application/json" \
            -d @"$alert_file")
        
        if echo "$response" | jq -e '.success' >/dev/null 2>&1; then
            echo "  ✅ 成功"
        else
            echo "  ❌ 失败"
            echo "  $response"
        fi
    fi
done

echo

# 停止服务
echo "--- 停止 HTTP 服务 ---"
kill $SERVER_PID 2>/dev/null || true
echo "HTTP 服务已停止"

echo
echo "=== 测试完成 ==="
