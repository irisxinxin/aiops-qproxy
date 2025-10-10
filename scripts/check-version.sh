#!/bin/bash
# 检查当前版本是否包含 keepalive 功能

echo "=== 检查 Git 版本 ==="
git log --oneline -1

echo ""
echo "=== 检查代码中是否有 keepalive 功能 ==="
if grep -q "keepalive started" internal/ttyd/wsclient.go; then
    echo "✓ 代码中包含 keepalive 功能"
else
    echo "✗ 代码中没有 keepalive 功能，需要 git pull！"
    exit 1
fi

echo ""
echo "=== 检查 binary 是否包含 keepalive 日志 ==="
if strings bin/incident-worker | grep -q "keepalive started"; then
    echo "✓ Binary 已包含 keepalive 功能"
else
    echo "✗ Binary 未包含 keepalive 功能，需要重新编译！"
    echo "   运行: go build -o bin/incident-worker ./cmd/incident-worker"
    exit 1
fi

echo ""
echo "=== 检查 KeepAlive 参数设置 ==="
grep -A 5 "KeepAlive:" cmd/incident-worker/main.go | head -6

echo ""
echo "✅ 版本检查通过！"
echo ""
echo "请确保在远程服务器上："
echo "1. git pull"
echo "2. 重新运行 ./scripts/deploy-real-q.sh"

