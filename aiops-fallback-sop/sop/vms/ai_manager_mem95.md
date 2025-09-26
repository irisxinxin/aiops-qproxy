# VMS - AI-Manager 容器内存 >95%

## 原则
- **不可直接 redeploy**。需 **先扩容/引流**，确认新 Pod 接管连接后，再替换旧 Pod。

## 操作步骤
1. **扩容 HPA**：在 Rancher 提升 `vms-ai-manager` 的 **minReplicas**，启动**新 Pod**承载连接。
2. **从旧Pod踢掉AI流**（新Pod已就绪后在旧Pod内执行）：
   ```bash
   curl -sS 'http://localhost:10080/v1/internal/vms/ops/smartStream/close'      -H 'Content-Type: application/json'      --data '{"tag": true, "number": 10, "timeInterval": 2000 }'
   ```
3. **监控**（Grafana 看板：AI 连接数/新旧 Pod 连接转移）直到旧Pod连接**降为0**且总连接数**恢复稳定**。
4. **删除问题旧Pod** 并 **恢复 HPA** 至原值。

## 备注
- 单实例 CPU 异常时亦可按“**移除问题 Pod**，让设备重连”原则处理。
