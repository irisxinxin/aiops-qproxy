#!/usr/bin/env bash
set -euo pipefail

CTX_DIR="${QCTX_DIR:-$(pwd)/ctx}"
echo "CTX_DIR = $CTX_DIR"

# 1) 删除明显空目录和无 final_ctx.md 的残片
find "$CTX_DIR" -type d -empty -print -delete || true
find "$CTX_DIR" -type d -name "history" -empty -print -delete || true

# 2) 删除没有 meta.json 或 final_ctx.md 的半成品
while IFS= read -r -d '' d; do
  if [ ! -f "$d/final_ctx.md" ] || [ ! -f "$d/meta.json" ]; then
    echo "Prune broken: $d"
    rm -rf "$d"
  fi
done < <(find "$CTX_DIR" -type d -mindepth 4 -maxdepth 4 -print0)

# 3) 基于内容 hash 去重（同键同 hash 只保留最新修改时间）
tmpfile="$(mktemp)"
declare -A seen
while IFS= read -r -d '' meta; do
  dir="$(dirname "$meta")"
  key="$(jq -r '.key // empty' "$meta" 2>/dev/null || true)"
  hash="$(jq -r '.hash // empty' "$meta" 2>/dev/null || true)"
  [[ -z "$key" || -z "$hash" ]] && continue
  k="$key::$hash"
  mtime="$(stat -c %Y "$dir/final_ctx.md" 2>/dev/null || stat -f %m "$dir/final_ctx.md" 2>/dev/null || echo 0)"
  echo -e "${mtime}\t${k}\t${dir}" >> "$tmpfile"
done < <(find "$CTX_DIR" -type f -name meta.json -print0)

sort -nr "$tmpfile" -o "$tmpfile" || true
declare -A keep
while IFS=$'\t' read -r _ k dir; do
  if [[ -n "${seen[$k]:-}" ]]; then
    echo "Prune duplicate: $dir"
    rm -rf "$dir"
  else
    seen[$k]=1
    keep["$dir"]=1
  fi
done < "$tmpfile"
rm -f "$tmpfile"

# 4) 重建 index.json
INDEX="$CTX_DIR/index.json"
echo '[]' > "$INDEX"
while IFS= read -r -d '' meta; do
  jq -c '.' "$meta" >> "$INDEX.tmp"
done < <(find "$CTX_DIR" -type f -name meta.json -print0)

if [ -f "$INDEX.tmp" ]; then
  jq -s '.' "$INDEX.tmp" > "$INDEX"
  rm -f "$INDEX.tmp"
fi

echo "Done. Index rebuilt at: $INDEX"




