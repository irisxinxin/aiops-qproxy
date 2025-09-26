# AIOps Q Proxy Testkit

## 目录
- alerts/: 两个示例报警 JSON，直接通过 stdin 喂给 runner
- meta/: 与 latency 用例相关的元数据（可用于上下文增强）
- ctx/: 预置 SOP、schema（用于 /context add）
- data/ctx/: 运行后若判定可复用，会把清洗后的上下文落到这里
- logs/: 运行时按次数写入清洗后的 stdout/stderr（便于 debug）
- scripts/: 一键本地跑的脚本

## 使用
1. 在你的仓库根（含 `bin/qproxy-runner`）下解压本包：
   ```bash
   unzip aiops-qproxy-testkit.zip -d .
   ```

2. 运行示例：
   ```bash
   ./scripts/run_cpu.sh
   ./scripts/run_latency.sh
   ```

3. 查看产物：
   - logs/: 每次运行的清洗后 stdout/stderr
   - data/ctx/: 若本次 Q 返回可复用的上下文，会自动落盘到此
