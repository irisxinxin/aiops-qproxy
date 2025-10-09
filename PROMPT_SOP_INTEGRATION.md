# Prompt 生成和 SOP 集成方案

## 当前状态对比

### 旧 HTTP 版本 (`cmd/runner/main.go`)
✅ 完整的 prompt 生成逻辑：
1. **SOP 加载** - 从 `ctx/sop/*.jsonl` 读取并匹配
2. **任务指令** - 加载 `ctx/task_instructions.md`
3. **模板替换** - `replaceSOPTemplates()` 替换 {{service_name}} 等变量
4. **历史上下文** - 加载过去的成功处理记录
5. **Alert 规范化** - 将 threshold 等字段字符串化

### 新 WebSocket 版本 (`cmd/incident-worker/main.go`)  
❌ 简化版 prompt 生成：
1. **仅从 JSON 提取** - `buildPrompt()` 只从 JSON 或环境变量获取
2. **无 SOP** - 没有集成 SOP 知识库
3. **无任务指令** - 没有加载 task_instructions.md
4. **无历史** - 按设计排除（避免污染会话）

---

## 需要添加的功能

### 1. Alert 结构体定义
```go
type Alert struct {
	Service   string          `json:"service"`
	Category  string          `json:"category"`
	Severity  string          `json:"severity"`
	Region    string          `json:"region"`
	Path      string          `json:"path"`
	Metadata  json.RawMessage `json:"metadata"`
	Threshold json.RawMessage `json:"threshold"`
}
```

### 2. SOP 结构体定义
```go
type SopLine struct {
	Keys       []string `json:"keys"`        // 匹配条件: svc:omada cat:cpu
	Priority   string   `json:"priority"`    // HIGH/MIDDLE/LOW
	Command    []string `json:"command"`     // 诊断命令列表
	Metric     []string `json:"metric"`      // 需要检查的指标
	Log        []string `json:"log"`         // 需要检查的日志
	Parameter  []string `json:"parameter"`   // 需要检查的参数
	FixAction  []string `json:"fix_action"`  // 修复操作
}
```

### 3. 核心函数需要从 `cmd/runner/main.go` 复制

#### 必需函数：
- `parseSopJSONL(path string) ([]SopLine, error)` - 解析单个 JSONL 文件
- `collectSopLines(dir string) ([]SopLine, error)` - 收集目录下所有 SOP
- `keyMatches(keys []string, a Alert) bool` - SOP 匹配逻辑
- `wildcardMatch(patt, val string) bool` - 通配符匹配
- `buildSopContext(a Alert, dir string) (string, error)` - 构建 SOP 上下文
- `replaceSOPTemplates(sop string, a Alert) string` - 模板替换
- `loadTaskDocInline(path string, budget, minKeep int) string` - 加载任务指令
- `jsonRawToString(raw json.RawMessage) string` - JSON 字段字符串化

### 4. 增强的 buildPrompt 函数

需要支持两种模式：
1. **简单模式** - 当前的直接提取 (用于简单测试)
2. **完整模式** - 包含 SOP + 任务指令 (用于生产告警处理)

```go
buildPromptWithSOP := func(ctx context.Context, raw []byte, m map[string]any, sopDir string, workdir string) (string, error) {
    // 1. 先尝试解析为 Alert
    var alert Alert
    if err := json.Unmarshal(raw, &alert); err == nil && alert.Service != "" {
        // 这是一个完整的 Alert，使用完整的 prompt 构建
        
        // 1) 加载 SOP
        sopText, _ := buildSopContext(alert, sopDir)
        
        // 2) 加载任务指令
        taskPath := filepath.Join(workdir, "ctx", "task_instructions.md")
        taskDoc := loadTaskDocInline(taskPath, 4096, 800)
        
        // 3) 规范化 Alert JSON
        alertMap := make(map[string]any)
        json.Unmarshal(raw, &alertMap)
        if thStr := jsonRawToString(alert.Threshold); thStr != "" {
            alertMap["threshold"] = thStr
        }
        alertJSON, _ := json.MarshalIndent(alertMap, "", "  ")
        
        // 4) 组装完整 prompt
        var b strings.Builder
        b.WriteString("You are an AIOps root-cause assistant.\n")
        if taskDoc != "" {
            b.WriteString("## TASK INSTRUCTIONS\n")
            b.WriteString(taskDoc)
            b.WriteString("\n\n")
        }
        b.WriteString("## ALERT JSON\n")
        b.WriteString(string(alertJSON))
        b.WriteString("\n\n")
        if sopText != "" {
            b.WriteString(sopText)
            b.WriteString("\n")
        }
        return b.String(), nil
    }
    
    // 2. 回退到简单模式
    return buildPrompt(ctx, raw, m)
}
```

---

## 环境变量配置

添加新的环境变量：
```bash
QPROXY_SOP_DIR=./ctx/sop           # SOP 目录
QPROXY_WORKDIR=.                   # 工作目录（包含 ctx/）
QPROXY_SOP_ENABLED=1               # 是否启用 SOP（默认启用）
```

---

## 文件确认

SOP 文件已存在：
- ✅ `ctx/sop/omada_sop_full.jsonl`
- ✅ `ctx/sop/vigi_sop_full.jsonl`
- ✅ `ctx/task_instructions.md`

---

## 实现步骤

1. ✅ 复制 SOP 相关结构体和函数到 `incident-worker/main.go`
2. ✅ 修改 `buildPrompt` 函数支持 SOP
3. ✅ 添加环境变量读取
4. ✅ 在 `/incident` handler 中使用增强的 prompt 构建
5. ✅ 更新 `deploy-real-q.sh` 添加新的环境变量

---

## 注意事项

1. **保持向后兼容** - 简单的 JSON (只有 prompt 字段) 仍然可以工作
2. **SOP 可选** - 如果 `QPROXY_SOP_DIR` 未设置或为空，跳过 SOP 加载
3. **性能考虑** - SOP 文件在启动时预加载，避免每次请求都读取
4. **不添加历史上下文** - 按设计，WebSocket 版本使用会话持久化，不需要历史上下文

