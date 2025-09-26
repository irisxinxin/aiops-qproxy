# SOP JSONL (auto-extracted)

本目录包含从两份PDF文档自动抽取得到的 SOP 片段，每行一个 JSON：
- `omada_sop_full.jsonl`：以 Omada 为主（附带通用条目）。
- `vigi_sop_full.jsonl`：VIGI/VMS 相关。

字段：
- title, keys[], priority, prechecks[], actions[], grafana[], notes, refs[] (来源文件名/章节线索)

使用建议：
- 运行时读取 `Q_SOP_DIR` 下所有 `*.jsonl`，按 keys 精确/模糊命中后 prepend 到喂给 q 的 context。
- keys 约定：svc:*, cat:*, sev:* 可扩展到 env:/region:/method:/path: 等。
