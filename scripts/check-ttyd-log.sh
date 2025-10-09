#!/bin/bash
# 检查 ttyd 日志

echo "🔍 查看 ttyd 最新日志（最后 50 行）："
tail -50 ./logs/ttyd-q.log

echo ""
echo "🔍 查找错误和断开连接："
grep -i "error\|close\|disconnect\|timeout" ./logs/ttyd-q.log | tail -20

