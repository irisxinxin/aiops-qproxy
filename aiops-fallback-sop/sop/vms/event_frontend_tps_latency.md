# VMS - Event-Frontend TPS↑ 且时延升高

## 判断
- grafana：若 istio ingressgateway **duration >>** 组件平均 `avg costTime`，同时 **TPS 上升** → 服务承压。

## 行动
- **水平扩容** `vms-event-frontend` 副本数（Rancher/HPA）。
- 关注看板：吞吐、延迟、错误率，验证扩容成效。
