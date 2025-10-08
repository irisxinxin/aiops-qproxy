#!/bin/bash
# 快速修复真实 Q CLI 环境问题的脚本

set -e

echo "🔧 快速修复真实 Q CLI 环境..."

# 停止所有相关服务
echo "🛑 停止所有相关服务..."
pkill -f "mock-ttyd\|incident-worker\|ttyd.*q chat" || true
sleep 3

# 检查并安装依赖
echo "📋 检查依赖..."

# 检查 Q CLI
if ! command -v q &> /dev/null; then
    echo "❌ Q CLI 未安装，尝试安装..."
    pip3 install amazon-q-cli || {
        echo "pip 安装失败，尝试其他方法..."
        # 尝试下载二进制文件
        wget -q https://github.com/aws/amazon-q-cli/releases/latest/download/amazon-q-cli-linux-x86_64.tar.gz -O /tmp/qcli.tar.gz
        if [ $? -eq 0 ]; then
            tar -xzf /tmp/qcli.tar.gz -C /tmp/
            sudo mv /tmp/amazon-q-cli /usr/local/bin/q
            sudo chmod +x /usr/local/bin/q
            echo "✅ Q CLI 安装成功"
        else
            echo "❌ Q CLI 安装失败，请手动安装"
            exit 1
        fi
    }
else
    echo "✅ Q CLI 已安装"
fi

# 检查 ttyd
if ! command -v ttyd &> /dev/null; then
    echo "❌ ttyd 未安装，尝试安装..."
    sudo apt update && sudo apt install -y ttyd || {
        echo "❌ ttyd 安装失败，请手动安装"
        exit 1
    }
else
    echo "✅ ttyd 已安装"
fi

# 创建必要目录
echo "📁 创建必要目录..."
mkdir -p ./conversations
mkdir -p ./logs
chmod 755 ./conversations
chmod 755 ./logs

# 设置环境变量
export QPROXY_WS_URL=https://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=3
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=1

# 测试 Q CLI
echo "🧪 测试 Q CLI..."
if q --version >/dev/null 2>&1; then
    echo "✅ Q CLI 工作正常"
else
    echo "❌ Q CLI 测试失败"
    echo "尝试配置 Q CLI..."
    q configure || echo "Q CLI 配置失败，可能需要 AWS 凭证"
fi

# 启动 ttyd (使用 HTTP 而不是 HTTPS)
echo "🔌 启动 ttyd (HTTP 模式)..."
ttyd -p 7682 -W -c demo:password123 q chat > ./logs/ttyd-q.log 2>&1 &
TTYD_PID=$!
echo $TTYD_PID > ./logs/ttyd-q.pid
echo "ttyd PID: $TTYD_PID"

# 等待 ttyd 启动
sleep 5

# 测试 ttyd 连接
echo "🧪 测试 ttyd 连接..."
if curl -s http://127.0.0.1:7682/ws >/dev/null 2>&1; then
    echo "✅ ttyd HTTP 连接正常"
    # 更新环境变量为 HTTP
    export QPROXY_WS_URL=http://127.0.0.1:7682/ws
    export QPROXY_WS_INSECURE_TLS=0
else
    echo "❌ ttyd 连接失败"
    echo "查看 ttyd 日志:"
    tail -10 ./logs/ttyd-q.log
    exit 1
fi

# 启动 incident-worker
echo "🚀 启动 incident-worker..."
go run ./cmd/incident-worker > ./logs/incident-worker-real.log 2>&1 &
WORKER_PID=$!
echo $WORKER_PID > ./logs/incident-worker-real.pid
echo "incident-worker PID: $WORKER_PID"

# 等待服务启动
sleep 5

# 测试连接
echo "🧪 测试连接..."
if curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "✅ incident-worker 健康检查通过"
    echo ""
    echo "🎉 修复完成！"
    echo ""
    echo "📊 服务状态："
    echo "  - ttyd + Q CLI: PID $TTYD_PID (端口 7682, HTTP)"
    echo "  - incident-worker: PID $WORKER_PID (端口 8080)"
    echo ""
    echo "🧪 测试命令："
    echo "  curl -sS -X POST http://127.0.0.1:8080/incident \\"
    echo "    -H 'content-type: application/json' \\"
    echo "    -d '{\"incident_key\":\"test-real-q\",\"prompt\":\"Hello Q CLI!\"}'"
    echo ""
    echo "📝 日志文件："
    echo "  - ttyd: ./logs/ttyd-q.log"
    echo "  - incident-worker: ./logs/incident-worker-real.log"
    echo ""
    echo "🛑 停止服务："
    echo "  kill $TTYD_PID $WORKER_PID"
else
    echo "❌ incident-worker 健康检查失败"
    echo "查看 incident-worker 日志:"
    tail -10 ./logs/incident-worker-real.log
    exit 1
fi
