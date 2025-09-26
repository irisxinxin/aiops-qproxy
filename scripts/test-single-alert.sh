#!/bin/bash

# HTTP 模式测试单个告警脚本
# 用于快速测试单个告警文件

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
HTTP_PORT="${HTTP_PORT:-8080}"
ALERTS_DIR="$PROJECT_DIR/alerts/dev"

echo "=== HTTP 模式测试单个告警 ==="
echo "项目目录: $PROJECT_DIR"
echo "HTTP 端口: $HTTP_PORT"
echo "告警目录: $ALERTS_DIR"
echo

# 检查参数
if [ $# -eq 0 ]; then
    echo "用法: $0 <告警文件> [端口]"
    echo
    echo "可用的告警文件:"
    ls "$ALERTS_DIR"/*.json 2>/dev/null | xargs -n 1 basename | sed 's/^/  /'
    echo
    echo "示例:"
    echo "  $0 omada_central_cpu.json"
    echo "  $0 omada_api_gateway_latency.json 8081"
    exit 1
fi

ALERT_FILE="$1"
if [ $# -gt 1 ]; then
    HTTP_PORT="$2"
fi

# 检查告警文件是否存在
if [ ! -f "$ALERTS_DIR/$ALERT_FILE" ]; then
    echo "错误: 告警文件不存在: $ALERTS_DIR/$ALERT_FILE"
    echo
    echo "可用的告警文件:"
    ls "$ALERTS_DIR"/*.json 2>/dev/null | xargs -n 1 basename | sed 's/^/  /'
    exit 1
fi

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
./bin/qproxy-runner --http --listen=":$HTTP_PORT" &

SERVER_PID=$!
echo "HTTP 服务已启动，PID: $SERVER_PID"

# 等待服务启动
sleep 3

# 测试健康检查
echo "--- 健康检查 ---"
if curl -s "http://localhost:$HTTP_PORT/health" >/dev/null; then
    echo "✅ 服务启动成功"
else
    echo "❌ 服务启动失败"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

echo

# 测试告警处理
echo "--- 测试告警: $ALERT_FILE ---"
echo "告警内容:"
cat "$ALERTS_DIR/$ALERT_FILE" | jq '.' 2>/dev/null || cat "$ALERTS_DIR/$ALERT_FILE"

echo
echo "HTTP 响应:"
response=$(curl -s -X POST "http://localhost:$HTTP_PORT/alert" \
    -H "Content-Type: application/json" \
    -d @"$ALERTS_DIR/$ALERT_FILE")

echo "$response" | jq '.' 2>/dev/null || echo "$response"

# 检查响应
if echo "$response" | jq -e '.success' >/dev/null 2>&1; then
    echo
    echo "✅ 告警处理成功"
    echo "响应摘要:"
    echo "$response" | jq '{success: .success, key: .key, exit_code: .exit_code}' 2>/dev/null || echo "无法解析响应摘要"
else
    echo
    echo "❌ 告警处理失败"
fi

echo

# 停止服务
echo "--- 停止 HTTP 服务 ---"
kill $SERVER_PID 2>/dev/null || true
echo "HTTP 服务已停止"

echo
echo "=== 测试完成 ==="
