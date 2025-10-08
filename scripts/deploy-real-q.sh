#!/bin/bash
# 部署真实 Q CLI 环境的脚本

set -e

echo "🚀 部署真实 Q CLI 环境..."

# 检查并清理端口占用
echo "🔍 检查端口占用情况..."

# 统一清理所有相关进程
echo "🛑 停止所有相关服务..."
pkill -f 'ttyd.*q chat' || true
pkill -f 'incident-worker' || true
pkill -f 'mock-ttyd' || true
sleep 2

# 强制清理端口
echo "🔧 强制清理端口..."
sudo fuser -k 7682/tcp 2>/dev/null || true
sudo fuser -k 8080/tcp 2>/dev/null || true
sleep 1

echo "✅ 端口清理完成"

# 检查依赖
echo "📋 检查依赖..."
if ! command -v q &> /dev/null; then
    echo "❌ Q CLI 未安装，请先安装："
    echo "   pip install amazon-q-cli"
    exit 1
fi

if ! command -v ttyd &> /dev/null; then
    echo "❌ ttyd 未安装，请先安装："
    echo "   brew install ttyd  # macOS"
    echo "   apt install ttyd   # Ubuntu"
    exit 1
fi

# 设置环境变量
export QPROXY_WS_URL=http://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=3
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=0

# 创建会话目录和日志目录
echo "📁 检查目录..."
if [ ! -d "./conversations" ]; then
    echo "创建 conversations 目录..."
    mkdir -p ./conversations
    chmod 755 ./conversations
    echo "✅ conversations 目录已创建"
else
    echo "✅ conversations 目录已存在"
fi

if [ ! -d "./logs" ]; then
    echo "创建 logs 目录..."
    mkdir -p ./logs
    chmod 755 ./logs
    echo "✅ logs 目录已创建"
else
    echo "✅ logs 目录已存在"
fi


# 启动真实 ttyd + Q CLI
echo "🔌 启动真实 ttyd + Q CLI..."
nohup ttyd -p 7682 -c demo:password123 q chat > ./logs/ttyd-q.log 2>&1 &
TTYD_PID=$!
echo $TTYD_PID > ./logs/ttyd-q.pid
echo "ttyd PID: $TTYD_PID"

# 等待 ttyd 启动并检查
sleep 3
if ! ss -tlnp | grep -q ":7682 "; then
    echo "❌ ttyd 启动失败"
    cat ./logs/ttyd-q.log
    exit 1
fi
echo "✅ ttyd 启动成功"

# 启动 incident-worker
echo "🚀 启动 incident-worker..."
cd "$(dirname "$0")/.."

# 检查 Go 环境
if ! command -v go &> /dev/null; then
    echo "❌ Go 未安装"
    exit 1
fi

# 检查 Go 模块
echo "📦 检查 Go 模块..."
if ! go mod tidy; then
    echo "❌ Go 模块整理失败"
    exit 1
fi

# 尝试编译
echo "🔨 编译 incident-worker..."
if ! go build -o ./bin/incident-worker ./cmd/incident-worker; then
    echo "❌ 编译失败"
    exit 1
fi

# 启动服务
echo "▶️  启动 incident-worker 服务..."
nohup ./bin/incident-worker > ./logs/incident-worker-real.log 2>&1 &
WORKER_PID=$!
echo $WORKER_PID > ./logs/incident-worker-real.pid
echo "incident-worker PID: $WORKER_PID"

# 等待服务启动并检查
sleep 3
if ! ss -tlnp | grep -q ":8080 "; then
    echo "❌ incident-worker 启动失败"
    echo "📝 查看详细日志："
    cat ./logs/incident-worker-real.log
    echo ""
    echo "🔍 检查进程状态："
    ps aux | grep incident-worker | grep -v grep || echo "  没有 incident-worker 进程"
    echo ""
    echo "🔍 检查端口状态："
    ss -tlnp | grep -E ":7682|:8080" || echo "  没有相关端口在监听"
    exit 1
fi
echo "✅ incident-worker 启动成功"

# 测试连接
echo "🧪 测试连接..."
if curl -s http://127.0.0.1:8080/healthz | grep -q "ok"; then
    echo "✅ incident-worker 健康检查通过"
else
    echo "❌ incident-worker 健康检查失败"
    echo "📝 查看最新日志："
    tail -20 ./logs/incident-worker-real.log
    echo ""
    echo "🔍 手动测试健康检查："
    curl -v http://127.0.0.1:8080/healthz || echo "  连接失败"
    echo ""
    echo "💡 建议："
    echo "  1. 检查 incident-worker 是否真的在运行"
    echo "  2. 检查端口 8080 是否被占用"
    echo "  3. 查看完整日志: cat ./logs/incident-worker-real.log"
    exit 1
fi

echo "🎉 真实 Q CLI 环境部署完成！"
echo ""
echo "📊 服务状态："
echo "  - ttyd + Q CLI: PID $TTYD_PID (端口 7682)"
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
