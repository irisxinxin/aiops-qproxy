#!/bin/bash

echo "🔍 检查会话文件存储位置和内容..."

cd "$(dirname "$0")/.."

echo "📁 检查 conversations 目录："
if [ -d "./conversations" ]; then
    echo "✅ conversations 目录存在"
    echo "目录内容："
    ls -la ./conversations/
    echo ""
    echo "目录大小："
    du -sh ./conversations/
else
    echo "❌ conversations 目录不存在"
fi

echo ""
echo "📝 检查 SOP 映射文件："
if [ -f "./conversations/_sopmap.json" ]; then
    echo "✅ SOP 映射文件存在"
    echo "文件内容："
    cat ./conversations/_sopmap.json
    echo ""
    echo "文件大小："
    ls -la ./conversations/_sopmap.json
else
    echo "❌ SOP 映射文件不存在"
fi

echo ""
echo "📝 检查会话文件："
if [ -f "./conversations/sop_f995f055ba30.jsonl" ]; then
    echo "✅ sdn5 会话文件存在"
    echo "文件内容："
    cat ./conversations/sop_f995f055ba30.jsonl
    echo ""
    echo "文件大小："
    ls -la ./conversations/sop_f995f055ba30.jsonl
else
    echo "❌ sdn5 会话文件不存在"
fi

echo ""
echo "📝 检查所有会话文件："
find ./conversations/ -name "*.jsonl" -type f | while read file; do
    echo "会话文件: $file"
    echo "大小: $(ls -la "$file" | awk '{print $5}') 字节"
    echo "最后修改: $(ls -la "$file" | awk '{print $6, $7, $8}')"
    echo "内容预览:"
    head -3 "$file" | sed 's/^/  /'
    echo ""
done

echo ""
echo "💡 会话文件说明："
echo "  - 位置: ./conversations/"
echo "  - SOP 映射: ./conversations/_sopmap.json"
echo "  - 会话文件: ./conversations/sop_*.jsonl"
echo "  - 格式: JSONL (每行一个 JSON 对象)"
echo "  - 内容: 包含问题和回答的完整对话历史"
