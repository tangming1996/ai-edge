# model-management Specification

## Purpose
TBD - created by archiving change bootstrap-v1-platform. Update Purpose after archive.
## Requirements
### Requirement: 模型元数据与制品管理
Model Registry SHALL 管理模型元数据（name、version、format、checksum、size、artifact_uri、signature_uri 等），制品文件存储于对象存储（建议 `MinIO`），支持 GGUF / TensorRT / ONNX / Safetensors 等格式。`(name, version)` MUST 唯一，`checksum` 一经发布 MUST NOT 变更。

#### Scenario: 注册模型版本
- **WHEN** 用户调用 `CreateModel` 提交模型元数据与制品引用
- **THEN** 系统记录元数据并保证 `(name,version)` 唯一
- **AND** 制品字节存于对象存储，不落主库

#### Scenario: 拒绝篡改已发布 checksum
- **WHEN** 尝试修改一个已发布模型版本的 checksum
- **THEN** 系统拒绝该变更

### Requirement: 制品签名与校验
Model Registry SHALL 为制品保存校验值与签名信息；下游（Gateway/Edge）落盘入缓存前 MUST 校验 `sha256 + signature`，校验失败 MUST NOT 入缓存。

#### Scenario: 校验失败拒绝入缓存
- **WHEN** Edge 下载的模型制品 sha256 或签名校验失败
- **THEN** 该制品不进入本地缓存，对应任务以不可恢复错误失败

### Requirement: 部署意图声明
`ModelDeployment` SHALL 表达用户声明的部署意图，包含目标模型版本、`target`（gateway + labelSelector）、`runtime`（可为 `auto`）与 `rollout` 策略。

#### Scenario: 创建部署
- **WHEN** 用户调用 `CreateDeployment` 指定模型版本与目标 gateway/labels
- **THEN** 系统在单事务内写入 `model_deployments`、生成父 `DeploymentTask` 与 `task_events`

### Requirement: 目标选择与计数对账
Controller Manager SHALL 依据 `target`（gateway + labelSelector）从已知 `EdgeNode` 计算 `desiredNodes` 写入 `ModelDeployment.status` 与父任务；`readyNodes`/`failedNodes` SHALL 由子任务结果聚合，并以云端主库为准对账。V1 目标选择 SHALL 仅基于 Gateway 归属、标签选择与 `RuntimeProfile` 匹配，不引入复杂调度器。

#### Scenario: 计数对账
- **WHEN** 部署的若干 NodeTask 陆续完成
- **THEN** `readyNodes`/`failedNodes` 按子任务结果实时聚合
- **AND** 与控制面定义的 `desiredNodes` 对账得出部署整体状态

### Requirement: Rollout 与回滚
ModelDeployment SHALL 支持基于 `maxUnavailable` 等约束的渐进式 rollout；回滚通过下发指向旧版本的部署意图实现（V1 以新部署任务覆盖，不要求原地状态回退）。

#### Scenario: 受控渐进部署
- **WHEN** 部署设置 `maxUnavailable: 10%`
- **THEN** rollout 过程中同时不可用的节点比例不超过该约束

