# CRD Schema 设计

## 1. 设计目标

本文件把前面的资源对象模型进一步收敛成接近实现的 `CRD Schema` 草案。

目标：

```text
统一字段命名
明确 required / optional 边界
明确 status 归属
为 Controller / API Server / Dashboard 提供一致对象结构
```

V1 的原则不是做很多 CRD，而是做少量稳定对象。

***

## 2. V1 CRD 清单

建议 V1 定义以下资源：

```text
Gateway
EdgeNode
EdgeIdentity
BootstrapToken
Model
RuntimeProfile
ModelDeployment
Task
```

说明：

- `Metrics` 不做 CRD
- `RuntimeState` 不做 CRD
- 高频状态不直接进入 K8s 风格对象

***

## 3. 通用 Schema 约定

### 3.1 metadata

所有资源默认包含：

```yaml
metadata:
  name: string
  labels: map[string]string
  annotations: map[string]string
```

### 3.2 spec 与 status

约定：

- `spec` 表示期望配置
- `status` 表示当前摘要状态
- 高频变化只保留摘要，不放完整明细

### 3.3 conditions

建议所有核心资源都支持：

```yaml
conditions:
  - type: Ready
    status: "True"
    reason: Healthy
    message: gateway is ready
    lastTransitionTime: "2026-06-10T10:00:00Z"
```

### 3.4 时间字段

统一使用：

```text
RFC3339 string
```

### 3.5 枚举字段

对 `phase`、`status`、`type` 尽量使用显式枚举，避免自由文本。

***

## 4. Gateway

## 4.1 Schema

```yaml
apiVersion: edge.ai/v1alpha1
kind: Gateway
metadata:
  name: gw-shanghai
spec:
  region: shanghai
  endpoint: https://gw-shanghai.example.com
  labels:
    factory: factory-a
  cachePolicy:
    maxVersions: 10
    evictionPolicy: LRU
  autonomyPolicy:
    enabled: true
    maxOfflineDuration: 24h
status:
  phase: Ready
  runtimeInstances: 3
  connectedNodes: 128
  lastSyncAt: "2026-06-10T10:00:00Z"
  conditions: []
```

## 4.2 Required 字段

`spec` 必填：

```text
region
endpoint
```

## 4.3 校验建议

- `metadata.name` 全局唯一
- `endpoint` 必须为 `https`
- `cachePolicy.maxVersions >= 1`

***

## 5. EdgeNode

## 5.1 Schema

```yaml
apiVersion: edge.ai/v1alpha1
kind: EdgeNode
metadata:
  name: edge-001
spec:
  gateway: gw-shanghai
  hostname: edge-001
  labels:
    factory: factory-a
    line: line-01
  hardware:
    cpu: arm64
    memory: 32Gi
    gpu:
      vendor: nvidia
      model: orin
  runtimeCapabilities:
    - tensorrt
    - onnxruntime
status:
  phase: Online
  agentVersion: 1.0.0
  lastSeen: "2026-06-10T10:00:00Z"
  runtimeSummary:
    loadedModels: 2
    runningRuntimes: 1
  conditions: []
```

## 5.2 Required 字段

`spec` 必填：

```text
gateway
hostname
hardware.cpu
hardware.memory
```

## 5.3 phase 枚举

```text
Pending
Registered
Online
Offline
Error
Draining
```

***

## 6. EdgeIdentity

## 6.1 Schema

```yaml
apiVersion: edge.ai/v1alpha1
kind: EdgeIdentity
metadata:
  name: node-6f98d7a1
spec:
  nodeID: node-6f98d7a1
  edgeNodeRef: edge-001
  gateway: gw-shanghai
  serial: SN123456
  certFingerprint: sha256:xxx
  subject: CN=node-6f98d7a1
  issuer: CN=gw-shanghai-intermediate
status:
  phase: Active
  issuedAt: "2026-06-10T10:00:00Z"
  expireAt: "2026-09-08T10:00:00Z"
  revokedAt: ""
  lastSeenAt: "2026-06-10T10:05:00Z"
  conditions: []
```

## 6.2 Required 字段

`spec` 必填：

```text
nodeID
gateway
serial
certFingerprint
```

## 6.3 phase 枚举

```text
Pending
Active
Revoked
Expired
Suspended
```

## 6.4 校验建议

- `nodeID` 全局唯一
- `serial` 在 Active / Suspended 范围内唯一
- `certFingerprint` 唯一

***

## 7. BootstrapToken

## 7.1 Schema

```yaml
apiVersion: edge.ai/v1alpha1
kind: BootstrapToken
metadata:
  name: factory-a
spec:
  gateway: gw-shanghai
  expiresIn: 24h
  maxUses: 100
  labels:
    region: shanghai
    factory: factory-a
status:
  phase: Active
  usedCount: 0
  expireAt: "2026-06-11T10:00:00Z"
  conditions: []
```

## 7.2 Required 字段

`spec` 必填：

```text
gateway
expiresIn
maxUses
```

## 7.3 phase 枚举

```text
Active
Frozen
Exhausted
Expired
Revoked
```

## 7.4 校验建议

- `maxUses >= 1`
- `expiresIn` 必须为正数时长

说明：

- 明文 `token` 不进入 CRD
- 明文 `token` 只在创建响应中返回一次

***

## 8. Model

## 8.1 Schema

```yaml
apiVersion: edge.ai/v1alpha1
kind: Model
metadata:
  name: qwen3-8b-v1-0-0
spec:
  name: qwen3-8b
  version: v1.0.0
  format: gguf
  checksum: sha256:xxx
  size: 8GB
  artifactURI: s3://models/qwen3-8b/v1.0.0/model.gguf
  signatureURI: s3://models/qwen3-8b/v1.0.0/model.sig
  framework: transformers
  precision: fp16
  labels:
    family: qwen
status:
  phase: Published
  publishedAt: "2026-06-10T10:00:00Z"
  conditions: []
```

## 8.2 Required 字段

`spec` 必填：

```text
name
version
format
checksum
artifactURI
```

## 8.3 phase 枚举

```text
Draft
Published
Deprecated
Archived
```

***

## 9. RuntimeProfile

## 9.1 Schema

```yaml
apiVersion: edge.ai/v1alpha1
kind: RuntimeProfile
metadata:
  name: orin-tensorrt
spec:
  selector:
    gpu: orin
  runtime: tensorrt
  priority: 100
  runtimeConfig:
    precision: fp16
status:
  phase: Active
  conditions: []
```

## 9.2 Required 字段

`spec` 必填：

```text
selector
runtime
```

## 9.3 phase 枚举

```text
Active
Disabled
Deprecated
```

***

## 10. ModelDeployment

## 10.1 Schema

```yaml
apiVersion: edge.ai/v1alpha1
kind: ModelDeployment
metadata:
  name: deploy-qwen3-prod
spec:
  model:
    name: qwen3-8b
    version: v1.0.0
  runtime: auto
  target:
    gateway: gw-shanghai
    labelSelector:
      factory: factory-a
  rollout:
    maxUnavailable: 10%
  policy:
    autoRollback: false
status:
  phase: Progressing
  desiredNodes: 100
  readyNodes: 80
  failedNodes: 2
  taskRef: task-001
  conditions: []
```

## 10.2 Required 字段

`spec` 必填：

```text
model.name
model.version
target
```

## 10.3 phase 枚举

```text
Pending
Progressing
Available
Degraded
Failed
```

***

## 11. Task

## 11.1 Schema

```yaml
apiVersion: edge.ai/v1alpha1
kind: Task
metadata:
  name: task-001
spec:
  type: InstallModel
  scope: Node
  parentTaskRef: task-deploy-001   # NodeTask 指向区域父任务；区域父任务留空
  target:
    gateway: gw-shanghai
    node: edge-001
  payload:
    model: qwen3-8b
    version: v1.0.0
    runtime: tensorrt              # runtime: auto 已在上游解析
  retryPolicy:
    maxRetries: 3
    backoff: exponential
  timeoutSeconds: 1800
  createdBy: admin
status:
  phase: Running
  retryCount: 1
  startedAt: "2026-06-10T10:00:00Z"
  finishedAt: ""
  message: installing model
  # 以下 claim 字段仅 NodeTask 使用，由 gateway-runtime 对云端主库原子更新
  ownerInstance: gw-rt-3
  claimExpireAt: "2026-06-10T10:05:00Z"
  dispatchStatus: Claimed
  conditions: []
```

## 11.2 Required 字段

`spec` 必填：

```text
type
scope
target
payload
```

## 11.3 phase 枚举

```text
Pending
Dispatching
Running
Success
Failed
Retrying
Timeout
Cancelled
PartiallySucceeded
```

## 11.4 type 枚举

```text
InstallModel
DeleteModel
StartRuntime
StopRuntime
RestartRuntime
UpgradeRuntime
UpgradeAgent
CollectLogs
RevokeNode
```

## 11.5 父子任务与 claim 字段

- `scope=Region` 为父任务（`DeploymentTask`），`scope=Node` 为子任务（`NodeTask`）
- 子任务用 `spec.parentTaskRef` 关联父任务；父任务的汇总状态由子任务聚合
- claim 字段（`ownerInstance / claimExpireAt / dispatchStatus`）仅 `NodeTask` 使用，是 `gateway-runtime` 在**云端主库**做原子 claim 的并发控制字段，不在 Gateway 本地维护
- `dispatchStatus` 建议枚举：`Unclaimed / Claimed / Dispatched / Done`

***

## 12. 控制器关注点

建议由控制器维护的核心关系：

- `ModelDeployment -> Task`
- `EdgeNode -> EdgeIdentity`
- `Gateway -> EdgeNode`
- `BootstrapToken -> Gateway`

控制器不应承担：

- 高频 metrics 写入
- Runtime 细粒度状态更新
- Agent 本地执行进度流

***

## 13. V1 实施建议

如果项目早期不直接落到真实 K8s CRD，也建议先保持相同对象结构。

原因：

- 后续迁移成本低
- API 与数据库字段能保持一致
- Dashboard 展示模型更稳定

***

## 14. 后续细化项

需要继续补充：

- proto message 与 CRD 字段映射
- 默认值策略
- Admission 校验规则
- Controller reconcile 逻辑清单
