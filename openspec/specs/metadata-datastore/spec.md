# metadata-datastore Specification

## Purpose
TBD - created by archiving change bootstrap-v1-platform. Update Purpose after archive.
## Requirements
### Requirement: 云端主库为全局唯一真相源
系统 SHALL 使用云端 `PostgreSQL` 作为全局元数据真相源，承载资源对象、任务全局状态、`EdgeIdentity`、`BootstrapToken`、缓存元数据与审计记录。Gateway/Edge 本地状态 MUST NOT 被视为真相源，只可作为可重建的缓存与恢复现场。

#### Scenario: 本地状态丢失不影响正确性
- **WHEN** 某个 Gateway 本地状态库整体丢失
- **THEN** 区域正确性不受影响（最多重拉/重扫）
- **AND** 恢复后仍以云端主库为最终准

### Requirement: 核心表与约束
主库 SHALL 包含核心表：`gateways`、`edge_nodes`、`edge_identities`、`bootstrap_tokens`、`models`、`runtime_profiles`、`model_deployments`、`tasks`、`task_runs`、`task_events`、`gateway_runtime_instances`、`gateway_cache_entries`、`edge_runtime_states`，并满足以下唯一约束：`edge_identities.node_id` 唯一、`edge_identities.fingerprint` 唯一、`bootstrap_tokens.token_hash` 唯一、`models(name,version)` 唯一、`gateway_cache_entries(gateway_id,model_id,version)` 唯一。

#### Scenario: 证书指纹不可重复注册
- **WHEN** 尝试以已存在的 `fingerprint` 创建 EdgeIdentity
- **THEN** 唯一索引拒绝写入

#### Scenario: 同一设备主标识不可被多个有效身份复用
- **WHEN** 同一 `serial` 已存在状态为 `Active` 或 `Suspended` 的身份，再次尝试为其创建另一有效身份
- **THEN** 部分唯一索引（`where status in ('Active','Suspended')`）拒绝写入

### Requirement: 关键查询索引
主库 SHALL 为关键查询路径建立索引，至少包括：按 `(target_gateway_id, status, created_at)` 查待分发任务、按 `(target_node_id, status, created_at)` 查节点任务、按 `(task_id, created_at)` 回放任务历史、按 `(gateway_id, last_access_at)` 支持缓存 LRU 回收、以及 NodeTask 调度的部分索引 `(target_gateway_id, dispatch_status) where scope='Node'`。

#### Scenario: Gateway 待分发任务查询走索引
- **WHEN** gateway-runtime 查询本区域待分发 NodeTask
- **THEN** 查询命中 `(target_gateway_id, dispatch_status)` 部分索引而非全表扫描

### Requirement: 关键事务边界
系统 SHALL 在单事务内保证以下一致性：(a) Bootstrap 注册时校验 token、`used_count + 1`、创建/更新 `EdgeIdentity` 与 `EdgeNode` 同事务完成，且 token 行使用 `for update`；(b) 节点吊销时更新 `EdgeIdentity.status` 与写入审计事件同事务；(c) Deployment 创建时写入 `model_deployments`、`tasks`、`task_events` 同事务。

#### Scenario: Bootstrap 计数与身份创建原子化
- **WHEN** 两个请求并发使用同一 BootstrapToken 注册
- **THEN** `used_count` 自增与身份创建在同一事务内串行化
- **AND** `used_count` 不会超过 `max_uses`

### Requirement: 首批 Migration 顺序
系统 SHALL 提供有序的 SQL migration，首批最小闭环至少覆盖 `gateways`、`edge_nodes`、`edge_identities`、`bootstrap_tokens`、`tasks`、`task_runs`、`task_events`，随后补充 `models`、`runtime_profiles`、`model_deployments`、`gateway_runtime_instances`、`gateway_cache_entries`、`edge_runtime_states`。每个 migration MUST 提供 up 与 down 脚本。

#### Scenario: 第一阶段可运行最小闭环
- **WHEN** 仅应用首批 migration
- **THEN** 系统已能支撑 `BootstrapToken → EdgeIdentity → EdgeNode → Task → TaskResult` 闭环所需的表结构

### Requirement: 状态与时序分离
高频数据 SHALL NOT 写入主对象表：`metrics` 进入时序系统，`task_events` 单独成表，runtime 历史轨迹不放 `edge_nodes`。`edge_runtime_states` 只保存当前快照，不存历史时序。

#### Scenario: 指标不进主库
- **WHEN** Edge 上报 CPU/GPU/QPS 等高频指标
- **THEN** 指标写入 Prometheus 而非 `edge_nodes` 或其他主对象表

