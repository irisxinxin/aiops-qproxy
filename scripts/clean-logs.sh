#!/bin/bash

# 清理 logs 文件夹的脚本
# 用于清理测试和生产环境产生的日志文件

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
LOGS_DIR="$PROJECT_DIR/logs"

echo "=== 清理 Logs 文件夹 ==="
echo "项目目录: $PROJECT_DIR"
echo "日志目录: $LOGS_DIR"
echo

# 检查 logs 目录是否存在
if [ ! -d "$LOGS_DIR" ]; then
    echo "日志目录不存在: $LOGS_DIR"
    echo "无需清理"
    exit 0
fi

# 显示当前日志文件
echo "--- 当前日志文件 ---"
if [ "$(ls -A "$LOGS_DIR" 2>/dev/null)" ]; then
    echo "找到以下日志文件:"
    ls -la "$LOGS_DIR"
    echo
    echo "文件统计:"
    echo "  总文件数: $(find "$LOGS_DIR" -type f | wc -l)"
    echo "  总大小: $(du -sh "$LOGS_DIR" | cut -f1)"
else
    echo "日志目录为空，无需清理"
    exit 0
fi

echo

# 确认删除
read -p "确定要删除所有日志文件吗？(y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "取消清理操作"
    exit 0
fi

# 删除日志文件
echo "--- 删除日志文件 ---"
rm -rf "$LOGS_DIR"/*

# 验证删除结果
if [ "$(ls -A "$LOGS_DIR" 2>/dev/null)" ]; then
    echo "⚠️  部分文件删除失败"
    echo "剩余文件:"
    ls -la "$LOGS_DIR"
else
    echo "✅ 所有日志文件已删除"
fi

echo
echo "=== 清理完成 ==="
