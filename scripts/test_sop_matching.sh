#!/bin/bash

# 测试 SOP 匹配功能的脚本
# 使用不同的告警来验证 SOP 文件是否能正确匹配

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
ALERTS_DIR="$PROJECT_DIR/alerts/dev"

echo "=== 测试 SOP 匹配功能 ==="
echo "项目目录: $PROJECT_DIR"
echo "告警目录: $ALERTS_DIR"
echo

# 检查告警文件是否存在
if [ ! -d "$ALERTS_DIR" ]; then
    echo "错误: 告警目录不存在: $ALERTS_DIR"
    exit 1
fi

# 测试函数
test_alert() {
    local alert_file="$1"
    local alert_name="$(basename "$alert_file" .json)"
    
    echo "--- 测试告警: $alert_name ---"
    
    if [ ! -f "$alert_file" ]; then
        echo "错误: 告警文件不存在: $alert_file"
        return 1
    fi
    
    # 使用 mock q 来测试 SOP 匹配
    echo "输入告警:"
    cat "$alert_file" | jq '.'
    echo
    
    echo "SOP 匹配结果:"
    cat "$alert_file" | \
        Q_SOP_DIR="$PROJECT_DIR/ctx/sop" \
        Q_SOP_PREPEND=1 \
        Q_BIN="/bin/cat" \
        go run "$PROJECT_DIR/cmd/runner/main.go" 2>&1 | \
        grep -A 10 -B 2 "SOP\|HISTORICAL\|FALLBACK" || echo "未找到 SOP 相关内容"
    
    echo
    echo "----------------------------------------"
    echo
}

# 测试所有告警文件
for alert_file in "$ALERTS_DIR"/*.json; do
    if [ -f "$alert_file" ]; then
        test_alert "$alert_file"
    fi
done

echo "=== 测试完成 ==="
