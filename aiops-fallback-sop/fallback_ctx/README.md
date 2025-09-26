# fallback_ctx.jsonl

每行一个 JSON：
- `match`: 用于你现有 `ctx-loader` 的规则匹配（示例字段可按需调整/扩展）。
- `ctx_id`: 上下文片段ID。
- `priority`: 命中多条时的优先级（数值越大越优先）。
- `text`: 经过清洗可直接喂给 Q 的“兜底上下文”。

在进入 Q 前，先从 **ctx/**、**data/** 命中可复用 context；若无，则再加载本 `fallback_ctx.jsonl` 作为兜底。
