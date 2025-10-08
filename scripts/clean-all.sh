#!/bin/bash

echo "🧹 彻底清理所有相关进程和端口..."

# 1. 停止所有相关进程
echo "🛑 停止所有相关进程..."
sudo pkill -f 'ttyd' || true
sudo pkill -f 'incident-worker' || true
sudo pkill -f 'mock-ttyd' || true
sudo pkill -f 'q chat' || true
sleep 3

# 2. 强制清理端口
echo "🔧 强制清理端口..."
sudo fuser -k 7682/tcp 2>/dev/null || true
sudo fuser -k 8080/tcp 2>/dev/null || true
sleep 2

# 3. 使用 lsof 强制清理
echo "💀 强制杀死占用端口的进程..."
sudo lsof -ti:7682 | xargs sudo kill -9 2>/dev/null || true
sudo lsof -ti:8080 | xargs sudo kill -9 2>/dev/null || true
sleep 2

# 4. 如果 8080 端口还在，用更强制的方法
if ss -tlnp | grep -q ":8080 "; then
    echo "🔥 8080 端口还在，使用更强制的方法..."
    # 找到占用 8080 的进程
    PID=$(sudo lsof -ti:8080 2>/dev/null)
    if [ ! -z "$PID" ]; then
        echo "   杀死进程 $PID"
        sudo kill -9 $PID 2>/dev/null || true
    fi
    # 使用 netstat 找到进程
    PID=$(sudo netstat -tlnp | grep ":8080 " | awk '{print $7}' | cut -d'/' -f1)
    if [ ! -z "$PID" ] && [ "$PID" != "-" ]; then
        echo "   杀死进程 $PID"
        sudo kill -9 $PID 2>/dev/null || true
    fi
    sleep 2
fi

# 4. 检查清理结果
echo "🔍 检查清理结果..."
echo "端口 7682:"
ss -tlnp | grep ":7682 " || echo "  ✅ 端口 7682 已释放"
echo "端口 8080:"
ss -tlnp | grep ":8080 " || echo "  ✅ 端口 8080 已释放"

# 5. 检查进程
echo "🔍 检查相关进程:"
ps aux | grep -E 'ttyd|incident-worker|q chat' | grep -v grep || echo "  ✅ 没有相关进程在运行"

echo ""
echo "✅ 清理完成！现在可以运行部署脚本了"
