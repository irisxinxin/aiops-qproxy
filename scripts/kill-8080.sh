#!/bin/bash

echo "🔥 专门清理 8080 端口..."

# 方法1: 使用 lsof
echo "方法1: 使用 lsof"
PID=$(sudo lsof -ti:8080 2>/dev/null)
if [ ! -z "$PID" ]; then
    echo "找到进程 $PID，正在杀死..."
    sudo kill -9 $PID
else
    echo "lsof 没有找到占用 8080 的进程"
fi

# 方法2: 使用 netstat
echo "方法2: 使用 netstat"
PID=$(sudo netstat -tlnp | grep ":8080 " | awk '{print $7}' | cut -d'/' -f1)
if [ ! -z "$PID" ] && [ "$PID" != "-" ]; then
    echo "找到进程 $PID，正在杀死..."
    sudo kill -9 $PID
else
    echo "netstat 没有找到占用 8080 的进程"
fi

# 方法3: 使用 ss
echo "方法3: 使用 ss"
PID=$(sudo ss -tlnp | grep ":8080 " | grep -o 'pid=[0-9]*' | cut -d'=' -f2)
if [ ! -z "$PID" ]; then
    echo "找到进程 $PID，正在杀死..."
    sudo kill -9 $PID
else
    echo "ss 没有找到占用 8080 的进程"
fi

# 方法4: 使用 fuser
echo "方法4: 使用 fuser"
sudo fuser -k 8080/tcp 2>/dev/null || echo "fuser 没有找到占用 8080 的进程"

sleep 2

# 检查结果
echo "🔍 检查清理结果:"
if ss -tlnp | grep -q ":8080 "; then
    echo "❌ 8080 端口仍被占用"
    echo "占用详情:"
    ss -tlnp | grep ":8080 "
    echo ""
    echo "进程详情:"
    sudo lsof -i:8080 2>/dev/null || echo "lsof 无法查看"
else
    echo "✅ 8080 端口已释放"
fi
