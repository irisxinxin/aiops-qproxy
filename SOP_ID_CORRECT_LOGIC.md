# SOP ID 正确逻辑

## 🎯 核心理念

**同一个 incident_key 生成的 sop_id 应该在两处保持一致：**
1. 会话映射（`_sopmap.json`）
2. SOP 规则文件（`ctx/sop/*.jsonl`）

## 📋 正确的设计

### 场景：sdn5 CPU 告警

1. **Alert 进来**，提取 `incident_key`：
   ```
   incident_key = "sdn5_cpu_high"
   ```

2. **生成 sop_id**（通过 SHA1 哈希）：
   ```
   sop_id = "sop_" + SHA1("sdn5_cpu_high")[:12]
   结果：sop_e0397b31b8d1
   ```

3. **这个 sop_id 同时用于**：
   - ✅ 会话持久化：`conversations/sop_e0397b31b8d1.jsonl`
   - ✅ 映射存储：`_sopmap.json` → `{"sdn5_cpu_high": "sop_e0397b31b8d1"}`
   - ✅ SOP 规则标识：`ctx/sop/sdn5_sop_full.jsonl` 中的 `sop_id` 字段
   - ✅ Prompt 中显示：`Matched SOP IDs: sop_e0397b31b8d1`

## 🔄 正确的工作流程

```
Alert (incident_key: "sdn5_cpu_high")
         ↓
   [哈希生成 sop_id]
         ↓
    sop_id: "sop_e0397b31b8d1"  ← 唯一标识
         ↓
    ┌─────┴─────┐
    ↓           ↓
[会话持久化]  [SOP匹配]
    ↓           ↓
保存到         匹配到
sop_e0397b31b8d1.jsonl   sop_e0397b31b8d1 规则
    ↓           ↓
  同一个 ID！
```

## 📝 正确的 SOP 文件结构

### ctx/sop/sdn5_sop_full.jsonl
```json
{
  "sop_id": "sop_e0397b31b8d1",
  "keys": ["cat:cpu", "svc:sdn5"],
  "command": ["查询 CPU..."],
  "fix_action": ["扩容..."]
}
```

### conversations/_sopmap.json
```json
{
  "sdn5_cpu_high": "sop_e0397b31b8d1"
}
```

### conversations/sop_e0397b31b8d1.jsonl
```
会话历史内容...
```

## ✅ 一致性保证

**关键点**：
- `incident_key` 作为输入
- 通过 SHA1 哈希生成唯一的 `sop_id`
- 这个 `sop_id` 在整个系统中保持一致：
  - SOP 规则文件中预定义
  - 会话映射中记录
  - 会话文件名使用
  - Prompt 中显示

## 🎯 为什么这样设计？

1. **一致性**：同一个 incident_key 在任何地方都对应同一个 sop_id
2. **可追溯**：通过 sop_id 可以找到对应的 SOP 规则和会话历史
3. **简洁性**：只需要一个 ID 就能关联所有相关信息
4. **确定性**：SHA1 哈希保证相同输入产生相同输出

## 📊 完整示例

### 输入
```json
{
  "service": "sdn5",
  "category": "cpu",
  "metadata": {
    "group_id": "sdn5_cpu_high"
  }
}
```

### 处理流程
```
1. extractIncidentKey() → "sdn5_cpu_high"
2. SHA1("sdn5_cpu_high")[:12] → "e0397b31b8d1"
3. sop_id = "sop_e0397b31b8d1"

4. 查找 SOP 规则（ctx/sop/sdn5_sop_full.jsonl）:
   ✓ 找到 sop_id = "sop_e0397b31b8d1" 的规则
   
5. 加载 SOP 内容到 Prompt:
   Matched SOP IDs: sop_e0397b31b8d1
   
6. 会话持久化:
   conversations/sop_e0397b31b8d1.jsonl
   
7. 更新映射:
   _sopmap.json: {"sdn5_cpu_high": "sop_e0397b31b8d1"}
```

### 输出
- Prompt 包含 SOP 内容
- 会话保存在正确的文件中
- 映射关系已记录
- 所有地方使用同一个 sop_id

---

## 🔧 当前实现需要调整

**问题**：当前 `incident_key` 的提取逻辑可能不够统一

**解决方案**：
1. 确保 `extractIncidentKey()` 提取的 key 和 SOP 规则名称一致
2. 或者，SOP 匹配后直接使用匹配到的 SOP 的 `sop_id` 作为会话 ID

**推荐方式**：
```
匹配 SOP 规则 → 获取规则的 sop_id → 用这个 sop_id 做会话 ID
```

这样可以保证：
- 同一类告警（如 sdn5 CPU）使用同一个 sop_id
- SOP 文件中的 sop_id 是唯一的真实来源
- 会话 ID 直接来源于匹配到的 SOP
