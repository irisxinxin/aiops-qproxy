# Omada Cloud-Access 接口延迟(p90/p99)过高

## 触发条件
- `cloud-access` 接口 p90/p99 延迟过高且 **30min+ 未自动恢复**。

## 快速检查
1. **基础资源**：CloudAccess 服务 **CPU/内存/GC频率/老年代占比** 是否异常。
2. **定位高延迟URI**：
   - 指标：`omada_cloud_cloud_access_cost_time_seconds` 按 URI 前缀聚合观察。
   - 日志：Kibana 过滤 cloudaccess，`timeConsumed > 阈值` + 时间范围，定位具体 URI。
3. **流程复盘**（排查请求链）：Launch → Passthrough → Get Launch Status → Success。

## 行动建议
- 若为短时尖峰已回落：**无需操作**（可能是 Pod 重启等引起）。
- 长时不恢复：
  - 优先扩容/横向扩展相关服务（根据瓶颈）并关注下游依赖。
  - 审核近 1 周**变更**与**大文件导出**等重任务是否叠加。
- 扩容后**严控回收**：先删老 Pod，再删负载较低 Pod；分批次，避免一次性删除过多。

## 复盘与监控
- 重点关注 **Redis 性能指标**、整体基础设施负载、全链路错误日志。
