# ttyd 1.7.4 协议完整修复清单

## 问题根因

ttyd 1.7.4 使用基于类型前缀的 WebSocket 协议：

### 客户端 → 服务器
```
'0' = INPUT (客户端输入)
'1' = PING  
'2' = RESIZE_TERMINAL
```

### 服务器 → 客户端
```
'0' = OUTPUT (终端输出)
'1' = SET_WINDOW_TITLE
'2' = SET_PREFERENCES
```

---

## 修复详情

### 1. ttyd readonly 模式 ✅
**问题**: ttyd 缺少 `--writable` 选项，以只读模式运行，拒绝所有客户端输入

**症状**:
```
[ttyd log] The --writable option is not set, will start in readonly mode
[ttyd log] W: ignored unknown message type: Y
```

**修复**: 在 `scripts/deploy-real-q.sh` 中添加 `-W` 选项
```bash
ttyd -W -p 7682 q chat
```

---

### 2. SendLine 消息格式错误 ✅
**问题**: 发送的消息缺少 ttyd 协议要求的 INPUT 类型前缀 `'0'`

**修复**: `internal/ttyd/wsclient.go`
```go
func (c *Client) SendLine(line string) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    // ttyd 1.7.4 协议：客户端输入需要以 "0" (INPUT 类型) 开头
    msg := "0" + line + "\n"
    return c.conn.WriteMessage(websocket.TextMessage, []byte(msg))
}
```

---

### 3. sendCtrlC 消息格式错误 ✅
**问题**: Ctrl-C 控制字符也缺少类型前缀

**修复**: `internal/ttyd/wsclient.go`
```go
func (c *Client) sendCtrlC() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    // ttyd 1.7.4 协议：Ctrl-C 也需要 INPUT 类型前缀
    return c.conn.WriteMessage(websocket.TextMessage, []byte{'0', 0x03})
}
```

---

### 4. 接收消息未去除类型前缀 ✅
**问题**: 从 ttyd 接收的消息包含 `'0'` 前缀，但代码直接写入了 buffer，导致：
- 提示符检测失败（实际内容是 `0Amazon Q>` 而不是 `Amazon Q>`）
- 返回的响应包含额外的 `'0'` 字符

**修复**: `internal/ttyd/wsclient.go` 中 `readUntilPrompt` 和 `readResponse`
```go
// 检查并去除类型前缀
var actualData []byte
if len(data) > 0 {
    msgType := data[0]
    if msgType == '0' {
        // OUTPUT 类型，写入实际内容（跳过类型前缀）
        if len(data) > 1 {
            actualData = data[1:]
            buf.Write(actualData)
        }
        msgCount++
    } else {
        // 其他类型（SET_WINDOW_TITLE 等），忽略
        log.Printf("ttyd: ignoring message type '%c'", msgType)
        continue
    }
}
```

---

### 5. 智能超时策略检查错误数据 ✅
**问题**: 在检查是否包含 `">"` 时，使用了包含类型前缀的原始 `data`，而不是去除前缀后的 `actualData`

**修复**: `internal/ttyd/wsclient.go`
```go
// 注意：这里检查去除前缀后的实际内容
if len(actualData) > 0 && strings.Contains(string(actualData), ">") || promptLikeSeen {
    promptLikeSeen = true
    _ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
} else {
    _ = c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
}
```

---

### 6. 连接池初始化逻辑错误 ✅
**问题**: 如果第一个连接创建失败，后续只会尝试创建 `size-1` 个连接，导致 pool size=1 时永远 ready=0

**修复**: `internal/pool/pool.go`
```go
// 所有连接都通过 fillOne 异步创建，有重试机制
go func() {
    for i := 0; i < size; i++ {  // 从 0 开始，而不是 1
        if i > 0 {
            time.Sleep(time.Duration(i) * time.Second)
        }
        p.fillOne(context.Background())
    }
}()
```

---

### 7. defer 清理超时问题 ✅
**问题**: `ContextClear()` 和 `Clear()` 内部使用 `context.Background()`，即使外层 defer 有 10s 超时，实际仍会等待最多 120s（2 × 60s）

**修复**: `internal/qflow/session.go` 和 `internal/runner/incident.go`
```go
// 添加带 context 参数的方法
func (s *Session) ContextClearWithContext(ctx context.Context) error {
    _, err := s.cli.Ask(ctx, "/context clear", s.opts.IdleTO)
    return err
}

func (s *Session) ClearWithContext(ctx context.Context) error {
    _, err := s.cli.Ask(ctx, "/clear", s.opts.IdleTO)
    return err
}

// defer 中使用 10s 超时的 context
defer func() {
    cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    if e := s.ContextClearWithContext(cleanupCtx); e != nil {
        if qflow.IsConnError(e) {
            lease.MarkBroken()
        }
    }
    if e := s.ClearWithContext(cleanupCtx); e != nil {
        if qflow.IsConnError(e) {
            lease.MarkBroken()
        }
    }
}()
```

---

## 验证步骤

在远程服务器上执行：

```bash
cd ~/huixin/aiops/aiops-qproxy-v2.4

# 1. 更新代码
git pull

# 2. 重新部署
./scripts/deploy-real-q.sh

# 3. 确认 ttyd 是 writable 模式
grep -i "writable\|readonly" logs/ttyd-q.log
# 应该看到: "listening in writable mode" 或类似信息

# 4. 测试
./scripts/test-sdn5.sh

# 5. 查看日志
tail -50 logs/ttyd-q.log
tail -50 logs/incident-worker-real.log

# 6. 确认没有错误
grep -i "ignored unknown message type" logs/ttyd-q.log
# 应该没有输出
```

---

## 预期结果

- ✅ ttyd 日志不再有 `ignored unknown message type` 警告
- ✅ incident-worker 健康检查返回 `{"ready":1,"size":1}`
- ✅ `/incident` 请求能正常返回 Q CLI 的响应
- ✅ 响应内容干净，不包含额外的 `'0'` 字符
- ✅ defer 清理最多阻塞 10s 而不是 120s

