## ADDED Requirements

### Requirement: 状态四类分离
系统 SHALL 将节点信息分为四类并分别存储：Inventory（静态资产，极低频）、NodeState（设备状态，约 10 秒）、Metrics（资源监控，进 Prometheus）、RuntimeState（AI 运行状态）。Metrics MUST NOT 进入状态主表。

#### Scenario: 指标与状态分流
- **WHEN** Edge 上报 NodeState 与 Metrics
- **THEN** NodeState 摘要更新到主库节点状态，Metrics 写入 Prometheus

### Requirement: 心跳与在线状态
系统 SHALL 依据 `ReportHeartbeat` 维护节点 `online`、`agentVersion` 与 `last_seen_at`，并据此判定节点在线/离线。

#### Scenario: 超时判离线
- **WHEN** 某节点超过预期心跳窗口未上报
- **THEN** 该节点被标记为离线

### Requirement: 运行时状态采集
系统 SHALL 采集并存储 RuntimeState（如已加载模型列表、模型运行状态、latencyP95、tokensPerSec、qps、requests）。`edge_runtime_states` 只保存当前快照，历史时序不进此表。

#### Scenario: 查询节点已加载模型
- **WHEN** 查询某节点 RuntimeState
- **THEN** 返回当前已加载模型及其运行指标摘要

### Requirement: 指标存储与暴露
资源指标 SHALL 存入 `Prometheus`；指标采集 SHALL 经 Gateway 聚合上报路径，避免每节点直连云端主库。

#### Scenario: 指标进入 Prometheus
- **WHEN** 采集到 CPU/Memory/GPU/QPS/Latency 等指标
- **THEN** 这些指标进入 Prometheus 供查询与告警，而非写入主对象表
