# 最终部署和测试指南

## ✅ 已完成的改进

### 1. Incident Key 规范化
- 所有字段（service, category, severity, region, alert_name, group_id）都进行规范化
- 统一替换 `-` 和空格为 `_`，统一小写
- 示例：`dev-nbu-aps1` → `dev_nbu_aps1`

### 2. SOP ID 和 Incident Key 分离
- **incident_key**：从 Alert 生成的完整标识符
  - 格式：`service_category_severity_region_alertname_groupid`
  - 示例：`sdn5_cpu_critical_dev_nbu_aps1_sdn5_container_cpu_usage_is_too_high_sdn5_critical`
- **sop_id**：SHA1 hash 的前 12 位
  - 格式：`sop_xxxxx`
  - 示例：`sop_1c3f1042b179`
- **_sopmap.json**：记录 `incident_key → sop_id` 映射，便于追溯

### 3. 完整的日志输出
日志中会记录：
```
incident: received request - incident_key=xxx, sop_id=xxx, prompt_len=xxx
=== PROMPT START (incident_key=xxx, sop_id=xxx) ===
<完整的 prompt 内容>
=== PROMPT END ===

runner: processing incident_key=xxx → sop_id=xxx, conv_path=xxx

=== RESPONSE START (incident_key=xxx, sop_id=xxx) ===
<Q CLI 的完整响应>
=== RESPONSE END ===
```

### 4. Prompt 结构
对于 Alert JSON，Prompt 包含：
1. **System Instructions**：单轮对话指令
2. **Task Instructions**：来自 `ctx/task_instructions.md`（约 4KB）
3. **Alert JSON**：完整的告警数据
4. **SOP Context**：匹配的 SOP 规则
   - Commands：诊断命令列表
   - Metrics：关键指标
   - Logs：日志路径
   - FixActions：修复建议

---

## 🚀 部署步骤（远程服务器）

### 1. 拉取最新代码
```bash
cd ~/huixin/aiops/aiops-qproxy-v2.4
git pull
```

### 2. 部署
```bash
cd aiops-qproxy
./scripts/deploy-real-q.sh
```

脚本会自动：
- 清理旧进程和端口（7682, 8080）
- 创建 `conversations/` 目录（如果不存在）
- 启动 ttyd（NoAuth 模式）
- 编译并启动 incident-worker
- 等待服务就绪（最多 120 秒）

### 3. 验证服务状态
```bash
# 检查服务健康状态
curl -s http://localhost:8080/healthz | jq

# 预期输出：
# {
#   "ready": 1,
#   "size": 1
# }
```

---

## 🧪 测试

### 1. 测试 SDN5 CPU 告警
```bash
cd ~/huixin/aiops/aiops-qproxy-v2.4/aiops-qproxy

curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d @alerts/dev/sdn5_cpu.json
```

### 2. 查看日志
```bash
# 查看完整日志（包括 PROMPT 和 RESPONSE）
tail -f logs/incident-worker-real.log

# 查看 ttyd 日志
tail -f logs/ttyd-q.log
```

### 3. 检查生成的文件
```bash
# 查看 sopmap（incident_key → sop_id 映射）
cat conversations/_sopmap.json

# 查看对话历史
ls -lh conversations/sop_*.jsonl

# 查看具体的对话内容
cat conversations/sop_1c3f1042b179.jsonl | jq
```

---

## 📋 日志示例

### 完整的日志流程
```
2025/10/09 15:00:00 incident: received request - incident_key=sdn5_cpu_critical_dev_nbu_aps1_sdn5_container_cpu_usage_is_too_high_sdn5_critical, sop_id=sop_1c3f1042b179, prompt_len=5234

2025/10/09 15:00:00 === PROMPT START (incident_key=sdn5_cpu_critical_dev_nbu_aps1_sdn5_container_cpu_usage_is_too_high_sdn5_critical, sop_id=sop_1c3f1042b179) ===
You are an AIOps root-cause assistant.
This is a SINGLE-TURN request. All data is COMPLETE below.
...
### [SOP] Preloaded knowledge (high priority)
Matched SOP ID: sop_1c3f1042b179
...
2025/10/09 15:00:00 === PROMPT END ===

2025/10/09 15:00:00 runner: processing incident_key=sdn5_cpu_critical_dev_nbu_aps1_sdn5_container_cpu_usage_is_too_high_sdn5_critical → sop_id=sop_1c3f1042b179, conv_path=conversations/sop_1c3f1042b179.jsonl

2025/10/09 15:00:30 incident: processing completed for sdn5_cpu_critical_dev_nbu_aps1_sdn5_container_cpu_usage_is_too_high_sdn5_critical, raw_response_len=2345, cleaned_len=2200

2025/10/09 15:00:30 === RESPONSE START (incident_key=sdn5_cpu_critical_dev_nbu_aps1_sdn5_container_cpu_usage_is_too_high_sdn5_critical, sop_id=sop_1c3f1042b179) ===
{
  "tool_calls": [...],
  "root_cause": "...",
  "evidence": [...],
  "confidence": 0.85,
  "suggested_actions": [...]
}
2025/10/09 15:00:30 === RESPONSE END ===
```

---

## 🐛 故障排查

### 1. 服务无法启动
```bash
# 检查端口占用
ss -tlnp | grep -E '7682|8080'

# 强制清理
sudo pkill -9 -f "ttyd|incident-worker"
sudo fuser -k 7682/tcp 8080/tcp
```

### 2. Q CLI 没有响应
```bash
# 查看 ttyd 日志
tail -50 logs/ttyd-q.log

# 检查 Q CLI 进程
ps aux | grep "q chat"

# 测试 Q CLI 直接调用
q chat
# 输入: hello
# 应该看到响应
```

### 3. 连接池问题
```bash
# 查看健康检查
watch -n 1 'curl -s http://localhost:8080/healthz | jq'

# 如果 ready 始终为 0，检查 ttyd 是否正常
curl -s http://localhost:7682/
```

### 4. Prompt 或 Response 异常
- 查看 `logs/incident-worker-real.log` 中的 `=== PROMPT START/END ===` 部分
- 检查 SOP 文件是否正确：`cat ctx/sop/sdn5_sop_full.jsonl | jq`
- 确认 `task_instructions.md` 存在：`cat ctx/task_instructions.md`

---

## 🎯 关键检查点

✅ **部署前**
- [ ] Git pull 完成，代码是最新的
- [ ] `conversations/` 目录存在（脚本会自动创建）
- [ ] `ctx/task_instructions.md` 存在
- [ ] SOP 文件存在且格式正确

✅ **部署后**
- [ ] ttyd 进程运行正常（端口 7682）
- [ ] incident-worker 进程运行正常（端口 8080）
- [ ] `/healthz` 返回 `{"ready": 1, "size": 1}`
- [ ] 日志文件正常写入

✅ **测试后**
- [ ] `_sopmap.json` 记录了正确的映射
- [ ] 对话历史文件生成（`conversations/sop_*.jsonl`）
- [ ] Response 是结构化的 JSON（而非 ANSI 控制码）
- [ ] 日志中能看到完整的 PROMPT 和 RESPONSE

---

## 📌 环境变量

当前配置（在 `deploy-real-q.sh` 中）：
```bash
QPROXY_WS_URL=ws://127.0.0.1:7682/ws
QPROXY_WS_POOL=1
QPROXY_WS_NOAUTH=1
QPROXY_WS_INSECURE_TLS=0
QPROXY_Q_WAKE=newline
QPROXY_CONV_ROOT=./conversations
QPROXY_SOPMAP_PATH=./conversations/_sopmap.json
QPROXY_SOP_DIR=./ctx/sop
QPROXY_SOP_ENABLED=1
QPROXY_PPROF=1
```

ttyd 环境变量：
```bash
TERM=dumb
NO_COLOR=1
CLICOLOR=0
FORCE_COLOR=0
Q_MCP_AUTO_TRUST=true
Q_MCP_SKIP_TRUST_PROMPTS=true
Q_TOOLS_AUTO_TRUST=true
```

---

## 🔍 性能监控

pprof 已启用（`QPROXY_PPROF=1`），访问：
```bash
# CPU profile
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof

# Heap profile
curl http://localhost:6060/debug/pprof/heap > heap.prof

# Goroutine profile
curl http://localhost:6060/debug/pprof/goroutine > goroutine.prof

# 可视化分析
go tool pprof -http=:8081 cpu.prof
```

---

## 💡 提示

1. **日志很大**：每个请求都会记录完整的 prompt 和 response，日志文件会快速增长。定期清理：
   ```bash
   ./scripts/clean-logs.sh
   ```

2. **调试 Prompt**：可以在本地使用 `scripts/show-prompt.sh` 预览生成的 prompt，无需部署。

3. **SOP 匹配**：确保 SOP 文件中的 `sop_id` 和 `incident_key` 一致，使用 SHA1 hash 生成。

4. **会话持久化**：每个 `sop_id` 对应一个对话文件，Q CLI 会加载历史上下文。

祝测试顺利！🚀

