#!/bin/bash

# 测试 SOP 匹配功能的脚本
# 支持 CLI 模式和 HTTP 服务模式

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
ALERTS_DIR="$PROJECT_DIR/alerts/dev"
HTTP_PORT="${HTTP_PORT:-8081}"

echo "=== 测试 SOP 匹配功能 ==="
echo "项目目录: $PROJECT_DIR"
echo "告警目录: $ALERTS_DIR"
echo "HTTP 端口: $HTTP_PORT"
echo

# 检查告警文件是否存在
if [ ! -d "$ALERTS_DIR" ]; then
    echo "错误: 告警目录不存在: $ALERTS_DIR"
    exit 1
fi

# 测试函数 - CLI 模式
test_alert_cli() {
    local alert_file="$1"
    local alert_name="$(basename "$alert_file" .json)"
    
    echo "--- CLI 模式测试告警: $alert_name ---"
    
    if [ ! -f "$alert_file" ]; then
        echo "错误: 告警文件不存在: $alert_file"
        return 1
    fi
    
    # 使用 mock q 来测试 SOP 匹配
    echo "输入告警:"
    cat "$alert_file" | jq '.'
    echo
    
    echo "SOP 匹配结果:"
    cat "$alert_file" | \
        Q_SOP_DIR="$PROJECT_DIR/ctx/sop" \
        Q_SOP_PREPEND=1 \
        Q_BIN="/bin/cat" \
        go run "$PROJECT_DIR/cmd/runner/main.go" 2>&1 | \
        grep -A 10 -B 2 "SOP\|HISTORICAL\|FALLBACK" || echo "未找到 SOP 相关内容"
    
    echo
    echo "----------------------------------------"
    echo
}

# 测试函数 - HTTP 模式
test_alert_http() {
    local alert_file="$1"
    local alert_name="$(basename "$alert_file" .json)"
    
    echo "--- HTTP 模式测试告警: $alert_name ---"
    
    if [ ! -f "$alert_file" ]; then
        echo "错误: 告警文件不存在: $alert_file"
        return 1
    fi
    
    # 发送 HTTP 请求
    echo "输入告警:"
    cat "$alert_file" | jq '.'
    echo
    
    echo "HTTP 响应:"
    response=$(curl -s -X POST "http://localhost:$HTTP_PORT/alert" \
        -H "Content-Type: application/json" \
        -d @"$alert_file")
    
    echo "$response" | jq '.' 2>/dev/null || echo "$response"
    
    # 检查响应中的 SOP 相关内容
    if echo "$response" | grep -q "SOP\|HISTORICAL\|FALLBACK"; then
        echo "✅ 找到 SOP 相关内容"
    else
        echo "⚠️  未找到 SOP 相关内容"
    fi
    
    echo
    echo "----------------------------------------"
    echo
}

# 启动 HTTP 服务
start_http_server() {
    echo "--- 启动 HTTP 服务 ---"
    
    # 检查端口是否被占用
    if lsof -i ":$HTTP_PORT" >/dev/null 2>&1; then
        echo "端口 $HTTP_PORT 已被占用，尝试停止现有服务..."
        pkill -f "qproxy-runner.*$HTTP_PORT" || true
        sleep 2
    fi
    
    # 启动 HTTP 服务
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
    if curl -s "http://localhost:$HTTP_PORT/health" >/dev/null; then
        echo "✅ HTTP 服务启动成功"
    else
        echo "❌ HTTP 服务启动失败"
        kill $SERVER_PID 2>/dev/null || true
        exit 1
    fi
    
    echo
}

# 停止 HTTP 服务
stop_http_server() {
    echo "--- 停止 HTTP 服务 ---"
    if [ ! -z "$SERVER_PID" ]; then
        kill $SERVER_PID 2>/dev/null || true
        echo "HTTP 服务已停止"
    fi
    echo
}

# 主测试函数
run_tests() {
    local mode="$1"
    
    case "$mode" in
        "cli")
            echo "=== CLI 模式测试 ==="
            for alert_file in "$ALERTS_DIR"/*.json; do
                if [ -f "$alert_file" ]; then
                    test_alert_cli "$alert_file"
                fi
            done
            ;;
        "http")
            echo "=== HTTP 模式测试 ==="
            start_http_server
            
            # 设置退出时清理
            trap stop_http_server EXIT
            
            for alert_file in "$ALERTS_DIR"/*.json; do
                if [ -f "$alert_file" ]; then
                    test_alert_http "$alert_file"
                fi
            done
            
            stop_http_server
            ;;
        "both")
            echo "=== CLI 和 HTTP 模式测试 ==="
            
            # CLI 测试
            echo "1. CLI 模式测试"
            for alert_file in "$ALERTS_DIR"/*.json; do
                if [ -f "$alert_file" ]; then
                    test_alert_cli "$alert_file"
                fi
            done
            
            # HTTP 测试
            echo "2. HTTP 模式测试"
            start_http_server
            trap stop_http_server EXIT
            
            for alert_file in "$ALERTS_DIR"/*.json; do
                if [ -f "$alert_file" ]; then
                    test_alert_http "$alert_file"
                fi
            done
            
            stop_http_server
            ;;
        *)
            echo "用法: $0 {cli|http|both}"
            echo "  cli  - 只测试 CLI 模式"
            echo "  http - 只测试 HTTP 模式"
            echo "  both - 测试两种模式"
            exit 1
            ;;
    esac
}

# 检查参数
mode="${1:-both}"

# 运行测试
run_tests "$mode"

echo "=== 测试完成 ==="
