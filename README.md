
# aiops-qproxy-v2.4-ec2-native (merged-final)

A small Go runner that drives **q** CLI headlessly, cleans ANSI/TUI noise, writes JSONL logs,
and persists **reusable context** files to reduce future token use.

## Build
```bash
./scripts/aiops-qproxy.sh build
```

## Run (one-shot)
```bash
# prepare alert.json and meta.json (optional)
./scripts/aiops-qproxy.sh run -- -alert alert.json -meta meta.json
# or read alert from stdin (systemd style):
cat alert.json | ./bin/qproxy-runner -alert - -meta meta.json
```

## What it does
- Adds base ctx (`ctx/schema.json`) and any matching reusable ctx from `data/ctx/`
- Dedups duplicate `/context add` lines (prevents 'Rule exists' spam)
- Forces NO_COLOR/TERM=dumb to reduce ANSI; strips remaining sequences
- Writes cleaned `stdout`/`stderr` into `logs/*.jsonl` and `logs/last_stdout.txt`
- If output contains a valid JSON with `confidence >= 0.6`, persists the built context under `data/ctx/<key>.<ts>.ctx.txt`

## systemd
Install to `/opt/aiops-qproxy`, then:
```bash
sudo cp -r . /opt/aiops-qproxy
sudo install -m755 bin/qproxy-runner /opt/aiops-qproxy/bin/qproxy-runner
sudo cp systemd/aiops-qproxy-runner.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now aiops-qproxy-runner
```

The unit reads alert JSON from STDIN; you can pipe your alerting bus into it or adapt ExecStart to point to your feeder process.

---

# WebSocket + 长连接池（Go 1.18 版） & 本地可运行 Mock

本补丁为 `aiops-qproxy` 增加：

- 通过 **ttyd** 的 **WebSocket 长连接池** 调用 `q chat`
- 会话持久化流程：`/load → 提问 → /compact → /save → /context clear → /clear`
- 可供 n8n 调用的最小 **HTTP 入口**：`POST /incident`
- **本地 Mock 版 ttyd**（无需真实 Q CLI 即可联调）

> 兼容 **Go 1.18**；未使用泛型/1.21+ 特性。

## 快速开始（Mock 本地联调）

### 1) 启动 Mock “ttyd + q chat” 服务（提供 `wss://.../ws`）

```bash
go run ./cmd/mock-ttyd \
  -addr :7682 \
  -user demo \
  -pass password123
```

说明：
- 监听 `:7682`，路径 `/ws`，**子协议**为 `tty`（与真实 ttyd 对齐）
- 需要 Basic Auth（默认 `demo:password123`）
- 模拟支持以下命令：
  - `/load <path>`：从本机文件系统读取 json 格式会话（`{"history":["..."]}`）
  - `/save <path> [-f]`：保存会话 json
  - `/compact`：把历史压缩为最近 10 条
  - `/clear`：清空当前会话历史
  - `/context add|rm|clear`：维护会话临时 context（仅记录，不影响回答）
  - `/usage`：返回一个估算值
  - `!<cmd>`：模拟 shell（仅 echo 回显）
  - 其他文本：视为**用户问题**，Mock 返回 `MOCK ANSWER: <你的问题>`

> 注意：Mock 会在每次响应末尾输出 `\n> ` 提示符，以便客户端以此作为“完成标记”。

### 2) 启动 Incident Worker（WebSocket 长连接池）

```bash
export QPROXY_WS_URL=http://127.0.0.1:7682/ws   # mock 是 http/ws；真实可用 https/wss
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=3
export QPROXY_CONV_ROOT=/tmp/conversations
export QPROXY_SOPMAP_PATH=/tmp/conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080

go mod tidy
go run ./cmd/incident-worker
```

### 3) 发起一次“报警处理”请求（n8n 可直接调用）

```bash
curl -sS -X POST http://127.0.0.1:8080/incident \
  -H 'content-type: application/json' \
  -d '{"incident_key":"v2|prd|omada-manager|cpu|thr=0.95|win=5m","prompt":"Return RCA and next steps."}'
```

流程：
1. `incident_key → sop_id`（若不存在则创建）
2. 从连接池租用一条长连接
3. 若存在 `$QPROXY_CONV_ROOT/<sop_id>.json`，执行 `/load`
4. 发送 `prompt`，收到回答
5. 执行 `/compact`，再 `/save -f` 到同一路径
6. 执行 `/context clear`、`/clear`，归还连接

---

## 连接真实 ttyd + Q CLI

### 快速部署（推荐）

```bash
# 1. 安装 Q CLI
pip install amazon-q-cli

# 2. 配置 AWS 凭证
aws configure
# 或设置环境变量：
# export AWS_ACCESS_KEY_ID=your_access_key
# export AWS_SECRET_ACCESS_KEY=your_secret_key
# export AWS_DEFAULT_REGION=us-east-1

# 3. 运行部署脚本
./scripts/deploy-real-q.sh

# 4. 测试真实环境
./scripts/test-real-q.sh
```

### 手动部署

在 Q CLI 所在机器启动 ttyd：

```bash
ttyd -p 7682 -W -c demo:password123 q chat
```

然后设置环境变量并启动 incident-worker：

```bash
export QPROXY_WS_URL=https://127.0.0.1:7682/ws
export QPROXY_WS_USER=demo
export QPROXY_WS_PASS=password123
export QPROXY_WS_POOL=3
export QPROXY_CONV_ROOT=/tmp/conversations
export QPROXY_SOPMAP_PATH=/tmp/conversations/_sopmap.json
export QPROXY_HTTP_ADDR=:8080
export QPROXY_WS_INSECURE_TLS=1  # 如果使用自签名证书

go run ./cmd/incident-worker
```

**重要**：`/save`、`/load` 读写的是 **Q 主机文件系统**。
确保 `QPROXY_CONV_ROOT` 在 Q 侧可读写（容器内建议挂卷）。

### 生产环境建议

- 将 `QPROXY_WS_URL` 改为 `https://<host>:7682/ws`
- 放到 Nginx/ALB 后面并开启 TLS/IP 白名单
- 使用 systemd 管理服务
- 配置日志轮转和监控

---

## 目录结构（新增）

- `cmd/mock-ttyd`：本地可运行的 ttyd+qchat 模拟器（WebSocket 服务）
- `cmd/incident-worker`：HTTP 服务，供 n8n 调用
- `internal/ttyd/wsclient.go`：最小 ttyd WebSocket 客户端
- `internal/qflow/session.go`：封装 `/load`、`/save`、`/compact`、`/clear`、`/context clear`
- `internal/pool/pool.go`：固定大小连接池
- `internal/store/convstore.go`：会话文件路径
- `internal/store/sopmap.go`：`incident_key → sop_id` 持久化

