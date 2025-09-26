# Omada - Login Rate Limit (临时&排障)

## 触发场景
- `login rate limit` 告警持续，需判定是否真实攻击/压测或短暂尖峰。

## 立刻动作（临时）
1. **临时静默**相关 Tel 告警，持续观察 Prometheus 指标是否自动恢复。

## 排查思路
1. 指标确认：观察5分钟窗口登录限速计数：
   ```promql
   sum(increase(omada_iam_login_rate_limit_times_total{namespace="omada-central",application="omada-iam"}[5m]))
   ```
2. 日志定位：按 `"/login"` 关键词检索，选择 TID，进一步根据 TID 反查 **用户 IP** 与 **accountId**。
3. 提升日志级别：临时将 `application-common.yaml` 的  
   `com.tplink.smb.omada.central.components.common.util=DEBUG`（**处理完记得恢复 INFO**）。

## 处置分支
- **单邮箱+单IP** 反复触发：邮件告知技术支持团队，沟通用户并**警告**；若仍未恢复，联系安全组 **封禁该 IP**。
- **多邮箱+单IP** 触发：联系安全组 **防火墙封禁该 IP**。

## 验证
- 指标回落、错误日志减少，恢复 INFO 日志级别。
