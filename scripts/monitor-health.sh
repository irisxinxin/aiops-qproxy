#!/bin/bash
# 实时监控 incident-worker 的健康状态

set -e

echo "🔍 监控 incident-worker 健康状态..."
echo "按 Ctrl+C 停止"
echo ""

while true; do
    clear
    echo "====== 时间: $(date '+%Y-%m-%d %H:%M:%S') ======"
    echo ""
    
    # 检查进程
    echo "📊 进程状态:"
    if ps aux | grep -v grep | grep incident-worker > /dev/null; then
        ps aux | grep -v grep | grep incident-worker | awk '{printf "  PID: %s, CPU: %s%%, MEM: %s%%, VSZ: %s, RSS: %s\n", $2, $3, $4, $5, $6}'
    else
        echo "  ❌ incident-worker 未运行"
    fi
    echo ""
    
    # 检查端口
    echo "🌐 端口状态:"
    if ss -tlnp 2>/dev/null | grep ":8080 " > /dev/null || netstat -tlnp 2>/dev/null | grep ":8080 " > /dev/null; then
        echo "  ✅ 8080 已监听"
    else
        echo "  ❌ 8080 未监听"
    fi
    if ss -tlnp 2>/dev/null | grep ":6060 " > /dev/null || netstat -tlnp 2>/dev/null | grep ":6060 " > /dev/null; then
        echo "  ✅ 6060 (pprof) 已监听"
    else
        echo "  ⚠️  6060 (pprof) 未监听"
    fi
    echo ""
    
    # 健康检查
    echo "❤️  健康检查:"
    if curl -sS -f -m 2 http://127.0.0.1:8080/healthz > /tmp/hz.$$ 2>&1; then
        echo "  ✅ /healthz: $(cat /tmp/hz.$$)"
        rm -f /tmp/hz.$$
    else
        echo "  ❌ /healthz: 失败"
    fi
    
    if curl -sS -f -m 2 http://127.0.0.1:8080/readyz > /dev/null 2>&1; then
        echo "  ✅ /readyz: OK"
    else
        echo "  ⚠️  /readyz: 未就绪"
    fi
    echo ""
    
    # Goroutine 数量 (如果 pprof 可用)
    if curl -sS -m 2 http://127.0.0.1:6060/debug/pprof/goroutine?debug=1 2>/dev/null > /tmp/goroutine.$$; then
        GOROUTINES=$(grep "goroutine profile:" /tmp/goroutine.$$ | awk '{print $4}')
        rm -f /tmp/goroutine.$$
        echo "🔧 Goroutine 数量: $GOROUTINES"
        if [ "$GOROUTINES" -gt 100 ]; then
            echo "  ⚠️  警告：goroutine 数量过多！"
        fi
    else
        echo "🔧 Goroutine 数量: N/A (pprof 未启用)"
    fi
    echo ""
    
    # 最近日志 (最后 3 行)
    echo "📝 最新日志 (最后 3 行):"
    if [ -f "./logs/incident-worker-real.log" ]; then
        tail -3 ./logs/incident-worker-real.log | sed 's/^/  /'
    else
        echo "  (无日志文件)"
    fi
    
    sleep 3
done

