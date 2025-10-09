# SOP 集成说明

## ✅ 已实现功能

### 1. SOP 自动加载和匹配

**支持的匹配字段**：
- `service` / `svc` - 服务名称
- `category` / `cat` - 告警类别（如 cpu, memory, latency）
- `severity` / `sev` - 严重级别（如 critical, warning）
- `region` - 区域

**匹配示例**：
```json
{
  "keys": ["svc:omada", "cat:cpu", "sev:critical"]
}
```
- 支持精确匹配：`svc:omada`
- 支持通配符：`svc:omada-*`
- 支持全匹配：`sev:*`
- 多个条件为 AND 关系

### 2. SOP 优先级

按优先级排序：
- `HIGH` - 高优先级
- `MIDDLE` - 中等优先级
- `LOW` - 低优先级

### 3. SOP 内容类型

每个 SOP 可以包含：
- `command` - 诊断命令列表
- `metric` - 需要检查的指标
- `log` - 需要检查的日志
- `parameter` - 需要检查的参数
- `fix_action` - 修复操作

### 4. 模板变量替换

支持在 SOP 中使用模板变量：
- `{{service_name}}` / `{{service名}}` - 服务名称
- `{{alert_path}}` - 告警路径
- `{{expression}}` - 告警表达式
- `{{alert_start_time}}` - 告警开始时间
- `{{alert_end_time}}` - 告警结束时间
- `{{alert_time}}` - 告警时间点

**示例**：
```json
{
  "command": [
    "Check CPU usage for {{service_name}} from {{alert_start_time}} to {{alert_end_time}}"
  ]
}
```

会被替换为：
```
Check CPU usage for omada from 2025-10-09T14:00:00Z to 2025-10-09T15:00:00Z
```

---

## 📁 文件结构

```
aiops-qproxy/
├── ctx/
│   └── sop/
│       ├── omada_sop_full.jsonl   # Omada 相关 SOP
│       └── vigi_sop_full.jsonl    # Vigi 相关 SOP
├── alerts/
│   └── dev/
│       └── test_sop_integration.json  # 测试用 Alert
└── scripts/
    └── test-sop-integration.sh    # SOP 集成测试脚本
```

---

## 🔧 环境变量配置

### 新增环境变量

```bash
QPROXY_SOP_DIR=./ctx/sop           # SOP 文件目录
QPROXY_SOP_ENABLED=1               # 是否启用 SOP（1=启用，0=禁用）
```

### 完整配置示例

```bash
export QPROXY_WS_URL=ws://127.0.0.1:7682/ws
export QPROXY_WS_NOAUTH=1
export QPROXY_WS_POOL=1
export QPROXY_CONV_ROOT=./conversations
export QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_Q_WAKE=newline
export QPROXY_SOP_DIR=./ctx/sop    # SOP 目录
export QPROXY_SOP_ENABLED=1         # 启用 SOP
```

---

## 🎯 使用方式

### 方式 1：发送 Alert JSON（自动触发 SOP）

```bash
curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d '{
    "service": "omada",
    "category": "cpu",
    "severity": "critical",
    "region": "us-west-2",
    "metadata": {
      "expression": "avg(cpu_usage) > 90"
    },
    "threshold": {
      "value": 90,
      "unit": "percent"
    }
  }'
```

**效果**：
1. 自动匹配 `omada_sop_full.jsonl` 中的 CPU 相关 SOP
2. 提取诊断命令、需检查的指标和日志
3. 替换模板变量
4. 将 SOP 内容添加到 prompt 中
5. 发送给 Q CLI 进行分析

### 方式 2：简单 Prompt（不触发 SOP）

```bash
curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d '{
    "incident_key": "test-simple",
    "prompt": "What is 1+1?"
  }'
```

**效果**：
- 直接提取 `prompt` 字段
- 不加载 SOP
- 适用于简单问答

### 方式 3：使用外部 Prompt 构建器（保留优化）

```bash
export QPROXY_PROMPT_BUILDER_CMD='python3 scripts/build_prompt.py'

curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d @alerts/dev/omada_api_gateway_latency.json
```

**效果**：
- 调用外部脚本生成 prompt
- 完全自定义 prompt 生成逻辑
- 优先级最高

---

## 🧪 测试

### 运行 SOP 集成测试

```bash
cd ~/huixin/aiops/aiops-qproxy-v2.4
./scripts/test-sop-integration.sh
```

### 测试场景

1. **Omada CPU 告警** - 应该匹配并加载 Omada CPU SOP
2. **简单 Prompt** - 不应该触发 SOP
3. **SDN5 CPU 告警** - 应该匹配并加载 SDN5 CPU SOP

---

## 📊 Prompt 生成优先级

```
1. QPROXY_PROMPT_BUILDER_CMD（外部构建器）
   ↓
2. Alert JSON + SOP（如果 QPROXY_SOP_ENABLED=1）
   ↓
3. 简单 Prompt 提取（JSON 中的 prompt 字段）
```

---

## 🔍 生成的 Prompt 格式

### 包含 SOP 的 Prompt 示例

```
You are an AIOps root-cause assistant.
Analyze the alert below and provide actionable remediation steps.

## ALERT JSON
{
  "service": "omada",
  "category": "cpu",
  "severity": "critical",
  ...
}

### [SOP] Preloaded knowledge (high priority)
- Command: Check CPU usage for omada from 2025-10-09T14:00:00Z to 2025-10-09T15:00:00Z
- Command: Review omada process list and identify high CPU consumers
- Metric: cpu_usage_percent
- Metric: cpu_load_average
- Log: /var/log/omada/application.log
- FixAction: Restart omada service if CPU usage > 95%
```

---

## 🎨 对比旧版本

### 旧 HTTP 版本 (`cmd/runner/main.go`)
- ✅ 完整的 SOP 集成
- ✅ 任务指令加载
- ✅ 历史上下文
- ❌ 每次请求都启动新的 Q CLI 进程

### 新 WebSocket 版本 (`cmd/incident-worker/main.go`)
- ✅ 完整的 SOP 集成（新增）
- ❌ 无任务指令（按当前优化的 buildPrompt 实现）
- ❌ 无历史上下文（使用会话持久化代替）
- ✅ 复用 Q CLI 连接池
- ✅ 保留外部 prompt 构建器支持

---

## ⚙️ 配置建议

### 生产环境

```bash
QPROXY_SOP_DIR=./ctx/sop
QPROXY_SOP_ENABLED=1
```

### 测试环境

```bash
QPROXY_SOP_ENABLED=0  # 禁用 SOP，使用简单 prompt
```

### 自定义 Prompt

```bash
QPROXY_PROMPT_BUILDER_CMD='python3 scripts/custom_prompt.py'
QPROXY_SOP_ENABLED=1  # 即使有外部构建器，也可以启用 SOP
```

---

## 🚀 部署

```bash
cd ~/huixin/aiops/aiops-qproxy-v2.4

# 1. 拉取最新代码
git pull

# 2. 重新部署（已包含 SOP 配置）
./scripts/deploy-real-q.sh

# 3. 测试 SOP 集成
./scripts/test-sop-integration.sh
```

---

## 📝 注意事项

1. **向后兼容** - 简单的 prompt JSON 仍然可以正常工作
2. **性能** - SOP 文件在第一次使用时加载，后续请求复用
3. **SOP 文件** - 已存在 `omada_sop_full.jsonl` 和 `vigi_sop_full.jsonl`
4. **可选功能** - 通过 `QPROXY_SOP_ENABLED=0` 可以禁用 SOP

