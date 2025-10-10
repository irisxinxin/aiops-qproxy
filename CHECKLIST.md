# 部署前检查清单

## ✅ 准备工作

### 1. 文件完整性检查
```bash
# 检查必需文件
ls -lh ctx/task_instructions.md
ls -lh ctx/sop/omada_sop_full.jsonl
ls -lh ctx/sop/vigi_sop_full.jsonl
ls -lh alerts/dev/test_sop_integration.json
```

### 2. 代码拉取
```bash
cd ~/huixin/aiops/aiops-qproxy-v2.4
git pull
git log -3 --oneline
```

应该看到：
- ✅ `docs: add comprehensive changes summary`
- ✅ `feat: integrate task_instructions.md into all prompts`
- ✅ `feat: integrate SOP (Standard Operating Procedures) into incident-worker`

---

## 🚀 部署步骤

### 1. 清理旧环境
```bash
./scripts/clean-all.sh
```

### 2. 部署新版本
```bash
./scripts/deploy-real-q.sh
```

### 3. 检查服务状态
```bash
# 健康检查
curl -s http://localhost:8080/healthz | jq

# 就绪检查
curl -s http://localhost:8080/readyz

# 查看日志
tail -50 logs/incident-worker-real.log
tail -50 logs/ttyd-q.log
```

**预期输出**：
- healthz: `{"ready":1,"size":1}`
- readyz: `ok`
- 日志无错误

---

## 🧪 功能测试

### 测试 1: SOP 集成（Omada CPU Alert）
```bash
curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d @alerts/dev/test_sop_integration.json \
  2>/dev/null | jq -r '.answer' | head -20
```

**验证点**：
- ✅ 响应包含 CPU 分析
- ✅ 提到 Omada 服务
- ✅ 提供诊断建议
- ✅ 没有 ANSI 控制字符
- ✅ 没有 "Thinking..." spinner

### 测试 2: 简单 Prompt（包含 Task Instructions）
```bash
curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d '{"incident_key":"test-simple","prompt":"What is 2+2?"}' \
  2>/dev/null | jq -r '.answer'
```

**验证点**：
- ✅ 返回正确答案（4）
- ✅ 回答简洁明了
- ✅ 没有要求继续或追问

### 测试 3: SDN5 CPU Alert
```bash
curl -X POST http://localhost:8080/incident \
  -H "Content-Type: application/json" \
  -d '{
    "service": "sdn5",
    "category": "cpu",
    "severity": "critical",
    "region": "ap-southeast-1",
    "metadata": {"expression": "avg(cpu_usage) > 85"}
  }' \
  2>/dev/null | jq -r '.answer' | head -20
```

**验证点**：
- ✅ 响应包含 CPU 分析
- ✅ 提到 SDN5 服务
- ✅ 提供修复建议

### 测试 4: 完整测试套件
```bash
./scripts/test-sop-integration.sh
```

---

## 🔍 故障排查

### 问题 1: healthz 返回 ready:0
```bash
# 检查连接池状态
tail -100 logs/incident-worker-real.log | grep "pool:"

# 检查 ttyd 状态
ps aux | grep ttyd
tail -50 logs/ttyd-q.log
```

**常见原因**：
- ttyd 未启动或崩溃
- Q CLI 初始化超时
- WebSocket 连接失败

**解决方案**：
```bash
./scripts/clean-all.sh
./scripts/deploy-real-q.sh
```

### 问题 2: 响应包含 "Thinking..." 或 spinner
```bash
# 检查日志中的原始响应
grep "received OUTPUT" logs/incident-worker-real.log | tail -5
```

**验证清洗函数**：
- 已添加 spinner 清理正则
- 已压缩多余换行

### 问题 3: SOP 未加载
```bash
# 检查 SOP 文件是否存在
ls -lh ctx/sop/*.jsonl

# 检查 QPROXY_SOP_ENABLED
grep QPROXY_SOP_ENABLED scripts/deploy-real-q.sh
```

**验证**：
- `QPROXY_SOP_ENABLED=1`
- `QPROXY_SOP_DIR=./ctx/sop`

### 问题 4: Task Instructions 未包含
```bash
# 检查文件存在
cat ctx/task_instructions.md | head -20

# 查看实际发送的 prompt
grep "ttyd: sending prompt" logs/incident-worker-real.log | tail -1
```

---

## 📊 性能验证

### 连接池效率
```bash
# 连续发送 3 个请求
for i in {1..3}; do
  echo "Request $i:"
  time curl -s -X POST http://localhost:8080/incident \
    -H "Content-Type: application/json" \
    -d '{"incident_key":"perf-test-'$i'","prompt":"Hello"}' \
    | jq -r '.answer' | wc -l
  echo ""
done
```

**预期**：
- 第 1 次可能较慢（连接池初始化）
- 第 2-3 次应该显著加快（连接复用）

### 内存和 Goroutine
```bash
# 使用 pprof（如果启用）
curl -s http://localhost:6060/debug/pprof/goroutine?debug=1 | head -50
```

---

## ✅ 完成确认

全部通过后，确认以下内容：

- [x] 服务成功启动（healthz, readyz 正常）
- [x] SOP 正确加载和匹配
- [x] Task Instructions 包含在所有 prompt 中
- [x] 响应清洁（无 ANSI, spinner）
- [x] 连接池正常工作（ready >= 1）
- [x] 简单 prompt 和 Alert JSON 都正常
- [x] 日志无错误
- [x] 性能符合预期

---

## 📞 支持

如果遇到问题，收集以下信息：

```bash
# 1. 服务日志
cat logs/incident-worker-real.log > /tmp/incident-worker.log
cat logs/ttyd-q.log > /tmp/ttyd-q.log

# 2. 健康状态
curl -s http://localhost:8080/healthz > /tmp/healthz.json
curl -s http://localhost:8080/readyz > /tmp/readyz.txt

# 3. 进程状态
ps aux | grep -E "(ttyd|incident-worker)" > /tmp/processes.txt

# 4. 端口状态
ss -tlnp | grep -E "(7682|8080)" > /tmp/ports.txt

# 5. Git 状态
git log -5 --oneline > /tmp/git-log.txt
git diff HEAD~5 --stat > /tmp/git-diff.txt
```
