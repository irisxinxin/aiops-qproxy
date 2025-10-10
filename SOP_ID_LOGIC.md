# SOP ID 生成逻辑说明

## 📋 两种 sop_id

系统中有**两种** `sop_id`，它们的用途不同：

### 1️⃣ SOP 文件中的 `sop_id`（静态，预定义）

**位置**：`ctx/sop/*.jsonl` 文件中

**生成方式**：
```bash
# 基于 SOP 规则的语义名称生成
incident_key = "sdn5_cpu_high"  # 人类可读的标识
sop_id = "sop_" + SHA1(incident_key)[:12]
# 例如：sop_e0397b31b8d1
```

**用途**：
- SOP 规则的唯一标识
- 在 Prompt 中显示（让 Q CLI 知道使用了哪个 SOP）
- 用于 SOP 规则的版本管理和追踪

**示例**：
```json
{
  "sop_id": "sop_e0397b31b8d1",
  "keys": ["cat:cpu", "svc:sdn5"],
  "command": ["查询 CPU 指标..."]
}
```

---

### 2️⃣ 会话映射中的 `sop_id`（动态，运行时生成）

**位置**：`conversations/_sopmap.json`

**生成方式**：
```go
// internal/store/sopmap.go - GetOrCreate()
func (m *SOPMap) GetOrCreate(key string) (string, error) {
    if v, ok := m.Get(key); ok {
        return v, nil  // 已存在，直接返回
    }
    h := sha1.Sum([]byte(key))  // 基于 incident_key 哈希
    sop := "sop_" + hex.EncodeToString(h[:])[:12]
    m.data[key] = sop  // 保存映射
    return sop, m.saveLocked()
}
```

**输入**：`incident_key`（从告警 JSON 中提取）
- 例如：`sdn5_critical`、`omada_api_gateway_timeout`

**输出**：`sop_id`
- 例如：`sop_850eaa67789c`

**用途**：
- **会话持久化**：每个唯一的 `incident_key` 对应一个会话
- **历史记录**：保存到 `conversations/sop_xxxxx.jsonl`
- **上下文复用**：同一个 incident 的多次请求共享会话历史

**示例映射**：
```json
{
  "sdn5_critical": "sop_850eaa67789c",
  "omada_timeout_api_gateway": "sop_a1b2c3d4e5f6"
}
```

---

## 🔄 完整工作流程

### 步骤 1：接收告警
```json
{
  "service": "sdn5",
  "category": "cpu",
  "severity": "critical",
  "metadata": {
    "group_id": "sdn5_critical"
  }
}
```

### 步骤 2：提取 incident_key
```go
// cmd/incident-worker/main.go
incident_key = extractIncidentKey(alertJSON)
// 结果：incident_key = "sdn5_critical"
```

### 步骤 3：生成会话 sop_id
```go
// internal/runner/incident.go - Process()
sopID, err := o.sopmap.GetOrCreate(in.IncidentKey)
// 调用 internal/store/sopmap.go
// 结果：sopID = "sop_850eaa67789c"
```

### 步骤 4：匹配 SOP 规则
```go
// cmd/incident-worker/main.go - buildSopContext()
// 根据 service=sdn5, category=cpu 匹配到：
matched_sop = {
  "sop_id": "sop_e0397b31b8d1",  // 这是 SOP 规则的 ID
  "keys": ["cat:cpu", "svc:sdn5"],
  ...
}
```

### 步骤 5：生成 Prompt
```
### [SOP] Preloaded knowledge (high priority)
Matched SOP IDs: sop_e0397b31b8d1  ← SOP 规则的 ID

- Command: 查询 CPU 指标...
- FixAction: 扩容 deployment...
```

### 步骤 6：会话持久化
```go
// 使用会话 sop_id 保存历史
convPath = "conversations/sop_850eaa67789c.jsonl"  // 基于 incident_key 的 sop_id
s.Save(convPath, true)
```

---

## 🎯 两种 ID 的对应关系

| 概念 | 来源 | 生成输入 | 示例 | 用途 |
|------|------|----------|------|------|
| **SOP 规则 ID** | `ctx/sop/*.jsonl` | SOP 规则名称 | `sop_e0397b31b8d1` | 标识 SOP 规则 |
| **会话 ID** | `_sopmap.json` | Alert 的 incident_key | `sop_850eaa67789c` | 标识会话历史 |

**关键点**：
- 一个会话（`sop_850eaa67789c`）可能多次使用不同的 SOP 规则
- 一个 SOP 规则（`sop_e0397b31b8d1`）可能被多个不同的会话使用
- 它们是**多对多**的关系

---

## 📝 示例场景

### 场景：sdn5 服务的 CPU 告警

1. **告警进入**：
   - `incident_key` = `"sdn5_critical"`

2. **生成会话 ID**：
   ```
   SHA1("sdn5_critical")[:12] = "850eaa67789c"
   会话 sop_id = "sop_850eaa67789c"
   ```

3. **匹配 SOP 规则**：
   - 匹配到：`sdn5_cpu_high` 规则
   - SOP 规则的 `sop_id` = `"sop_e0397b31b8d1"`

4. **Prompt 中显示**：
   ```
   Matched SOP IDs: sop_e0397b31b8d1
   ```

5. **会话保存到**：
   ```
   conversations/sop_850eaa67789c.jsonl
   ```

6. **映射记录**：
   ```json
   {
     "sdn5_critical": "sop_850eaa67789c"
   }
   ```

---

## 🔍 为什么需要两种 ID？

### SOP 规则 ID 的作用
- ✅ 让 Q CLI 知道用了哪个知识库
- ✅ 便于追踪和调试（"这次用了哪个 SOP？"）
- ✅ SOP 版本管理和更新追踪

### 会话 ID 的作用
- ✅ 隔离不同告警的会话历史
- ✅ 同一个 incident 的多次请求共享上下文
- ✅ 防止不同告警的历史混淆

---

## 🛠️ 代码位置

### SOP 规则 ID
- **定义**：`ctx/sop/*.jsonl`
- **使用**：`cmd/incident-worker/main.go` - `buildSopContext()`
- **显示**：Prompt 中的 `Matched SOP IDs`

### 会话 ID
- **生成**：`internal/store/sopmap.go` - `GetOrCreate()`
- **映射**：`conversations/_sopmap.json`
- **使用**：`internal/runner/incident.go` - `Process()`
- **持久化**：`conversations/sop_xxxxx.jsonl`

---

## 📊 总结

```
Alert (incident_key: "sdn5_critical")
         ↓
  [会话映射层]
         ↓
    会话 sop_id: "sop_850eaa67789c"  ← 用于会话持久化
         ↓
  [SOP 匹配层]
         ↓
    SOP 规则 sop_id: "sop_e0397b31b8d1"  ← 显示在 Prompt 中
         ↓
    加载 SOP 内容到 Prompt
         ↓
    发送给 Q CLI 分析
         ↓
    保存响应到 conversations/sop_850eaa67789c.jsonl
```

**核心理念**：
- **会话 ID** = 告警实例的唯一标识（会话级别）
- **SOP 规则 ID** = 知识库条目的唯一标识（知识级别）
