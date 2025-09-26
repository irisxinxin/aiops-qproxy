#!/bin/bash

# 环境配置设置脚本
# 用于快速配置本地和生产环境

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== AIOps QProxy 环境配置 ==="
echo "项目目录: $PROJECT_DIR"
echo

# 检查当前环境
if [[ -f "/opt/aiops-qproxy/bin/qproxy-runner" ]]; then
    ENV_TYPE="production"
    echo "检测到生产环境"
else
    ENV_TYPE="local"
    echo "检测到本地开发环境"
fi

setup_local() {
    echo "--- 配置本地环境 ---"
    
    # 创建 .env.local 文件
    cat > "$PROJECT_DIR/.env.local" << 'EOF'
# 本地开发环境配置
# 这个文件会被 run-qproxy.sh 自动加载

# 核心配置
Q_BIN=/usr/local/bin/q
QWORKDIR=/Users/xin/Desktop/aiops-qproxy/aiops-qproxy
QCTX_DIR=/Users/xin/Desktop/aiops-qproxy/aiops-qproxy/ctx/final
QLOG_DIR=/Users/xin/Desktop/aiops-qproxy/aiops-qproxy/logs
QDATA_DIR=/Users/xin/Desktop/aiops-qproxy/aiops-qproxy/data

# SOP 配置
Q_SOP_DIR=/Users/xin/Desktop/aiops-qproxy/aiops-qproxy/ctx/sop
Q_SOP_PREPEND=1
Q_FALLBACK_CTX=/Users/xin/Desktop/aiops-qproxy/aiops-qproxy/aiops-fallback-sop/fallback_ctx/fallback_ctx.jsonl

# 输出控制
NO_COLOR=1
CLICOLOR=0
TERM=dumb
EOF

    echo "✅ 已创建 .env.local 文件"
    
    # 确保目录存在
    mkdir -p "$PROJECT_DIR/ctx/sop" "$PROJECT_DIR/ctx/final" "$PROJECT_DIR/logs" "$PROJECT_DIR/data"
    echo "✅ 已创建必要目录"
    
    # 检查 SOP 文件
    if [[ -f "$PROJECT_DIR/ctx/sop/omada_sop_full.jsonl" ]]; then
        echo "✅ SOP 文件已存在"
    else
        echo "⚠️  SOP 文件不存在，请确保已复制到 ctx/sop/ 目录"
    fi
    
    # 检查 fallback 文件
    if [[ -f "$PROJECT_DIR/aiops-fallback-sop/fallback_ctx/fallback_ctx.jsonl" ]]; then
        echo "✅ Fallback 上下文文件已存在"
    else
        echo "⚠️  Fallback 上下文文件不存在"
    fi
}

setup_production() {
    echo "--- 配置生产环境 ---"
    
    # 检查 systemd 服务文件
    if [[ -f "$PROJECT_DIR/systemd/aiops-qproxy-runner.service" ]]; then
        echo "✅ Systemd 服务文件已存在"
        
        # 显示服务配置
        echo "服务配置:"
        grep -E "^Environment=" "$PROJECT_DIR/systemd/aiops-qproxy-runner.service" | sed 's/^/  /'
    else
        echo "❌ Systemd 服务文件不存在"
        return 1
    fi
    
    # 检查生产目录
    if [[ -d "/opt/aiops-qproxy" ]]; then
        echo "✅ 生产目录已存在"
    else
        echo "⚠️  生产目录不存在，请先部署到 /opt/aiops-qproxy"
    fi
    
    echo
    echo "生产环境部署步骤:"
    echo "1. sudo cp -r $PROJECT_DIR /opt/aiops-qproxy"
    echo "2. sudo cp $PROJECT_DIR/systemd/aiops-qproxy-runner.service /etc/systemd/system/"
    echo "3. sudo systemctl daemon-reload"
    echo "4. sudo systemctl enable aiops-qproxy-runner"
    echo "5. sudo systemctl start aiops-qproxy-runner"
}

test_config() {
    echo "--- 测试配置 ---"
    
    if [[ "$ENV_TYPE" == "local" ]]; then
        # 测试本地配置
        if [[ -f "$PROJECT_DIR/.env.local" ]]; then
            source "$PROJECT_DIR/.env.local"
            echo "✅ 环境变量已加载"
            
            # 测试 SOP 匹配
            echo "测试 SOP 匹配..."
            if echo '{"service":"omada-central","region":"dev-nbu-aps1","category":"latency","severity":"critical"}' | \
               Q_SOP_DIR="$Q_SOP_DIR" Q_SOP_PREPEND=1 Q_BIN=/bin/cat go run "$PROJECT_DIR/cmd/runner/main.go" 2>&1 | \
               grep -q "SOP.*knowledge"; then
                echo "✅ SOP 匹配功能正常"
            else
                echo "❌ SOP 匹配功能异常"
            fi
        else
            echo "❌ .env.local 文件不存在"
        fi
    else
        echo "生产环境测试需要手动执行"
    fi
}

case "${1:-auto}" in
    local) setup_local ;;
    production) setup_production ;;
    test) test_config ;;
    auto)
        if [[ "$ENV_TYPE" == "local" ]]; then
            setup_local
        else
            setup_production
        fi
        test_config
        ;;
    *) 
        echo "用法: $0 {local|production|test|auto}"
        echo "  local      - 配置本地环境"
        echo "  production - 配置生产环境"
        echo "  test       - 测试当前配置"
        echo "  auto       - 自动检测并配置"
        exit 1
        ;;
esac

echo
echo "=== 配置完成 ==="
