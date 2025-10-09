#!/bin/bash
# 部署真实 Q CLI 环境的脚本

set -e

echo "🚀 部署真实 Q CLI 环境..."

# 检查并清理端口占用
echo "🔍 检查端口占用情况..."

# 先运行彻底清理脚本
if [ -f "./scripts/clean-all.sh" ]; then
    echo "🧹 运行彻底清理..."
    ./scripts/clean-all.sh
else
    echo "🛑 手动清理..."
    sudo pkill -f 'ttyd\|incident-worker\|mock-ttyd' || true
    sudo fuser -k 7682/tcp 2>/dev/null || true
    sudo fuser -k 8080/tcp 2>/dev/null || true
    sleep 2
fi

# 特别处理 8080 端口（如果还在占用）
if ss -tlnp | grep -q ":8080 "; then
    echo "🔥 8080 端口还在占用，强制清理..."
    # 使用多种方法清理 8080
    sudo lsof -ti:8080 | xargs sudo kill -9 2>/dev/null || true
    # 避免依赖 netstat：仅用 lsof/fuser 处理
    sleep 2
fi

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
export QPROXY_WS_URL=ws://127.0.0.1:7682/ws
# 使用 NoAuth 模式，不设置认证信息
# export QPROXY_WS_USER=demo
# export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=1
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


# 启动真实 ttyd + Q CLI (NoAuth 模式)
echo "🔌 启动真实 ttyd + Q CLI (NoAuth 模式)..."
# 关闭颜色/动效并开启 Q 自动信任，避免 TUI 控制序列
nohup ttyd -p 7682 env NO_COLOR=1 CLICOLOR=0 TERM=dumb \
  Q_MCP_AUTO_TRUST=true Q_MCP_SKIP_TRUST_PROMPTS=true Q_TOOLS_AUTO_TRUST=true \
  q chat > ./logs/ttyd-q.log 2>&1 &
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

# 强制重新编译（确保使用最新的超时设置）
echo "🔨 重新编译 incident-worker..."
rm -f ./bin/incident-worker
if ! go build -o ./bin/incident-worker ./cmd/incident-worker; then
    echo "❌ 编译失败"
    exit 1
fi
echo "✅ 编译成功（使用新的超时设置）"

# 启动服务
echo "▶️  启动 incident-worker 服务 (NoAuth 模式)..."
# 设置环境变量并启动服务
env \
QPROXY_WS_URL=ws://127.0.0.1:7682/ws \
QPROXY_WS_NOAUTH=1 \
QPROXY_WS_POOL=1 \
QPROXY_CONV_ROOT=./conversations \
QPROXY_SOPMAP_PATH=./conversations/_sopmap.json \
QPROXY_HTTP_ADDR=:8080 \
QPROXY_WS_INSECURE_TLS=0 \
QPROXY_PPROF=1 \
nohup ./bin/incident-worker > ./logs/incident-worker-real.log 2>&1 &
WORKER_PID=$!
echo $WORKER_PID > ./logs/incident-worker-real.pid
echo "incident-worker PID: $WORKER_PID"

# 先等待端口 8080 打开（最多 60s）
echo "⏳ 等待 incident-worker 端口打开..."
for i in $(seq 1 60); do
  if ss -tlnp | grep -q ":8080 "; then
    break
  fi
  sleep 1
  if [ $i -eq 60 ]; then
    echo "❌ 端口 8080 未打开"
    tail -50 ./logs/incident-worker-real.log || true
    exit 1
  fi
done

# 再等待服务就绪（最多 120s）
echo "⏳ 等待 incident-worker 就绪..."
ok=false
for i in $(seq 1 120); do
  code=$(curl -sS -o /tmp/qproxy_ready.$$ -w '%{http_code}' http://127.0.0.1:8080/readyz || true)
  if [ "$code" = "200" ]; then
    ok=true
    rm -f /tmp/qproxy_ready.$$ 2>/dev/null || true
    break
  fi
  sleep 1
done
if [ "$ok" != true ]; then
  echo "❌ incident-worker 就绪超时"
  echo "📝 查看详细日志："; tail -50 ./logs/incident-worker-real.log || true
  echo "🔍 端口状态："; ss -tlnp | grep -E ":7682|:8080" || true
  exit 1
fi
echo "✅ incident-worker 就绪"

# 测试连接
echo "🧪 测试连接..."
HZ=$(curl -sS http://127.0.0.1:8080/healthz || true)
echo "healthz: $HZ"
echo "$HZ" | grep -q '"ready":[1-9]' && echo "✅ incident-worker 健康检查通过" || {
  echo "❌ incident-worker 健康检查未就绪"; tail -20 ./logs/incident-worker-real.log; exit 1; }

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
