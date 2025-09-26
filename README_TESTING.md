# AIOps QProxy 测试指南

## 概述

AIOps QProxy 现在支持两种运行模式：
- **CLI 模式**: 从 stdin 读取告警 JSON，输出到 stdout
- **HTTP 模式**: 提供 HTTP API 服务，通过 POST 请求处理告警

## 快速开始

### 1. 构建程序

```bash
go build -o bin/qproxy-runner ./cmd/runner
```

### 2. 运行测试

```bash
# 交互式测试菜单
./scripts/test-all.sh

# 或者直接运行特定测试
./scripts/test-http.sh          # HTTP 模式测试
./scripts/test_sop_matching.sh  # SOP 匹配测试
./scripts/clean-logs.sh         # 清理日志
```

## 测试模式详解

### CLI 模式测试

CLI 模式是原有的测试方式，通过管道传递告警 JSON：

```bash
# 使用 mock q 测试
./scripts/run_with_mock.sh

# 使用真实 q 测试
./scripts/run_with_q.sh

# 测试特定告警
./scripts/run_cpu.sh
./scripts/run_latency.sh

# 手动测试
echo '{"service":"omada-central","region":"prd-nbu-aps1","category":"latency","severity":"critical"}' | \
  Q_BIN=/bin/cat ./bin/qproxy-runner
```

### HTTP 模式测试

HTTP 模式提供 REST API 接口：

```bash
# 启动 HTTP 服务
./bin/qproxy-runner --http --listen=:8080

# 健康检查
curl http://localhost:8080/health

# 处理告警
curl -X POST http://localhost:8080/alert \
  -H "Content-Type: application/json" \
  -d '{"service":"omada-central","region":"prd-nbu-aps1","category":"latency","severity":"critical"}'
```

### SOP 匹配测试

测试 SOP 文件匹配功能：

```bash
# CLI 模式 SOP 测试
./scripts/test_sop_matching.sh cli

# HTTP 模式 SOP 测试
./scripts/test_sop_matching.sh http

# 两种模式都测试
./scripts/test_sop_matching.sh both
```

## 测试文件说明

### 现有测试脚本

| 脚本 | 模式 | 说明 |
|------|------|------|
| `run_cpu.sh` | CLI | 测试 CPU 告警 |
| `run_latency.sh` | CLI | 测试延迟告警 |
| `run_with_mock.sh` | CLI | 使用 mock q 测试 |
| `run_with_q.sh` | CLI | 使用真实 q 测试 |
| `test-http.sh` | HTTP | HTTP 服务测试 |
| `test_sop_matching.sh` | 混合 | SOP 匹配测试 |
| `test-all.sh` | 混合 | 交互式测试菜单 |
| `clean-logs.sh` | - | 清理日志文件 |

### 告警测试文件

测试告警文件位于 `alerts/dev/` 目录：

- `cpu_resolved.json` - CPU 告警
- `latency_firing.json` - 延迟告警
- `login_rate_limit.json` - 登录限流告警
- `omada_*.json` - Omada 服务相关告警
- `vms_*.json` - VMS 服务相关告警

## 环境变量

### 核心配置

```bash
Q_BIN=/usr/local/bin/q                    # q CLI 路径
QWORKDIR=/path/to/project                 # 工作目录
QCTX_DIR=/path/to/ctx/final              # 上下文目录
QLOG_DIR=/path/to/logs                   # 日志目录
QDATA_DIR=/path/to/data                  # 数据目录
```

### SOP 配置

```bash
Q_SOP_DIR=/path/to/ctx/sop               # SOP 文件目录
Q_SOP_PREPEND=1                          # 是否预加载 SOP
Q_FALLBACK_CTX=/path/to/fallback.jsonl   # 备用上下文
```

### 输出控制

```bash
NO_COLOR=1                               # 禁用颜色输出
CLICOLOR=0                               # 禁用彩色输出
TERM=dumb                                # 终端类型
```

## 生产环境部署

### 1. 部署 HTTP 服务

```bash
# 一键部署
./scripts/deploy-http.sh

# 手动部署
go build -o bin/qproxy-runner ./cmd/runner
sudo cp systemd/aiops-qproxy-runner.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl start aiops-qproxy-runner
```

### 2. 服务管理

```bash
# 查看状态
sudo systemctl status aiops-qproxy-runner

# 查看日志
sudo journalctl -u aiops-qproxy-runner -f

# 重启服务
sudo systemctl restart aiops-qproxy-runner

# 停止服务
sudo systemctl stop aiops-qproxy-runner
```

## 故障排除

### 常见问题

1. **端口被占用**
   ```bash
   # 查找占用端口的进程
   lsof -i :8080
   
   # 停止占用进程
   pkill -f qproxy-runner
   ```

2. **权限问题**
   ```bash
   # 确保脚本有执行权限
   chmod +x scripts/*.sh
   
   # 确保程序有执行权限
   chmod +x bin/qproxy-runner
   ```

3. **q CLI 未安装**
   ```bash
   # 使用 mock q 进行测试
   Q_BIN=/bin/cat ./bin/qproxy-runner --http
   ```

### 日志查看

```bash
# 查看应用日志
ls -la logs/

# 查看 systemd 服务日志
sudo journalctl -u aiops-qproxy-runner -f

# 清理日志
./scripts/clean-logs.sh
```

## 开发建议

1. **本地开发**: 使用 `./scripts/test-all.sh` 进行交互式测试
2. **CI/CD**: 使用 `./scripts/test-http.sh` 进行自动化测试
3. **生产部署**: 使用 `./scripts/deploy-http.sh` 进行一键部署
4. **日志管理**: 定期使用 `./scripts/clean-logs.sh` 清理日志

## 更新日志

- **v2.4**: 添加 HTTP 服务模式支持
- **v2.3**: 添加历史上下文和存储限制
- **v2.2**: 添加 SOP 匹配功能
- **v2.1**: 基础告警处理功能
