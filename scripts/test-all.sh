#!/bin/bash

# 全面测试脚本
# 支持 CLI 和 HTTP 模式测试

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
HTTP_PORT="${HTTP_PORT:-8081}"

echo "=== AIOps QProxy 全面测试 ==="
echo "项目目录: $PROJECT_DIR"
echo "HTTP 端口: $HTTP_PORT"
echo

# 检查程序是否已构建
if [ ! -f "$PROJECT_DIR/bin/qproxy-runner" ]; then
    echo "--- 构建程序 ---"
    cd "$PROJECT_DIR"
    go build -o bin/qproxy-runner ./cmd/runner
    echo "✅ 程序构建完成"
    echo
fi

# 显示测试选项
echo "=== 测试选项 ==="
echo "1. CLI 模式测试 (原有方式)"
echo "2. HTTP 模式测试 (新方式)"
echo "3. 混合模式测试 (两种都测试)"
echo "4. 快速 HTTP 测试"
echo "5. SOP 匹配测试"
echo "6. 清理日志"
echo

read -p "请选择测试模式 (1-6): " choice

case $choice in
    1)
        echo "=== CLI 模式测试 ==="
        echo "使用原有的 CLI 脚本进行测试..."
        
        # 测试 CPU 告警
        echo "--- 测试 CPU 告警 ---"
        ./scripts/run_cpu.sh
        
        echo
        echo "--- 测试延迟告警 ---"
        ./scripts/run_latency.sh
        
        echo
        echo "--- 测试 Mock Q ---"
        ./scripts/run_with_mock.sh
        
        echo
        echo "✅ CLI 模式测试完成"
        ;;
        
    2)
        echo "=== HTTP 模式测试 ==="
        ./scripts/test-http.sh
        ;;
        
    3)
        echo "=== 混合模式测试 ==="
        echo "1. 先进行 CLI 测试"
        ./scripts/run_with_mock.sh
        
        echo
        echo "2. 再进行 HTTP 测试"
        ./scripts/test-http.sh
        ;;
        
    4)
        echo "=== 快速 HTTP 测试 ==="
        # 启动 HTTP 服务
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
        curl -s "http://localhost:$HTTP_PORT/health" | jq '.'
        
        # 测试告警处理
        echo
        echo "--- 告警处理测试 ---"
        test_alert='{"service":"omada-central","region":"prd-nbu-aps1","category":"latency","severity":"critical"}'
        curl -s -X POST "http://localhost:$HTTP_PORT/alert" \
            -H "Content-Type: application/json" \
            -d "$test_alert" | jq '.'
        
        # 停止服务
        kill $SERVER_PID 2>/dev/null || true
        echo
        echo "✅ 快速 HTTP 测试完成"
        ;;
        
    5)
        echo "=== SOP 匹配测试 ==="
        echo "选择测试模式:"
        echo "  a) CLI 模式"
        echo "  b) HTTP 模式"
        echo "  c) 两种模式"
        read -p "请选择 (a/b/c): " sop_choice
        
        case $sop_choice in
            a)
                ./scripts/test_sop_matching.sh cli
                ;;
            b)
                ./scripts/test_sop_matching.sh http
                ;;
            c)
                ./scripts/test_sop_matching.sh both
                ;;
            *)
                echo "无效选择，使用默认模式 (both)"
                ./scripts/test_sop_matching.sh both
                ;;
        esac
        ;;
        
    6)
        echo "=== 清理日志 ==="
        ./scripts/clean-logs.sh
        ;;
        
    *)
        echo "无效选择"
        exit 1
        ;;
esac

echo
echo "=== 测试完成 ==="
echo "查看日志: ls -la logs/"
echo "清理日志: ./scripts/clean-logs.sh"
