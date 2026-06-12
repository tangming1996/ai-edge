# 资源对象模型设计

## 1. 设计目标

本文件定义平台 V1 的核心资源对象。

目标：

```text
统一控制面资源语言
统一管理面与执行面的对象边界
为 API、数据库、控制器实现提供对象基线
```

V1 资源模型强调：

- 少而稳定
- 面向控制语义
- 不直接泄露底层实现细节

***

## 2. 设计原则

### 2.1 Edge 不是 Kubernetes Node

因此边缘设备对象独立建模为：

```text
EdgeNode
```

而不是复用：

```text
Node
```

### 2.2 状态与指标分离

资源对象只保留必要状态，不直接承载高频监控数据。

### 2.3 期望状态与实际状态分离

资源 `spec` 表达期望，
资源 `status` 表达当前摘要状态。

### 2.4 安全身份单独建模

身份不直接塞入 `EdgeNode`，单独定义：

```text
EdgeIdentity
```

便于处理：

```text
续签
吊销
审计
生命周期管理
```

***

## 3. 核心资源清单

V1 建议包含：

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

***

## 4. Gateway

## 4.1 语义

`Gateway` 表示一个逻辑上的区域控制中心。

它不是某个单独实例，而是一个区域级逻辑资源。

## 4.2 示例

```yaml
kind: Gateway

metadata:
  name: gw-shanghai

spec:
  region: shanghai
  labels:
    factory: factory-a

status:
  phase: Ready
  runtimeInstances: 3
  connectedNodes: 128
```

## 4.3 字段建议

`spec`：

```text
region
labels
endpoint
cachePolicy
autonomyPolicy
```

`status`：

```text
phase
runtimeInstances
connectedNodes
lastSyncAt
conditions
```

***

## 5. EdgeNode

## 5.1 语义

`EdgeNode` 表示一个受平台管理的边缘设备资源。

它描述的是：

```text
设备归属
设备静态资产
设备摘要状态
```

不直接承载完整身份信息。

## 5.2 示例

```yaml
kind: EdgeNode

metadata:
  name: edge-001

spec:
  gateway: gw-shanghai
  labels:
    factory: factory-a
    line: line-01
  hardware:
    cpu: arm64
    memory: 32Gi
    gpu:
      vendor: nvidia
      model: orin

status:
  phase: Online
  agentVersion: 1.0.0
  lastSeen: "2026-06-10T10:00:00Z"
```

## 5.3 字段建议

`spec`：

```text
gateway
labels
hostname
hardware
runtimeCapabilities
```

`status`：

```text
phase
agentVersion
lastSeen
runtimeSummary
conditions
```

***

## 6. EdgeIdentity

## 6.1 语义

`EdgeIdentity` 表示节点身份与证书生命周期对象。证书由 Control Plane Signer 用对应区域 Intermediate CA 签发，故 `issuer` 为区域 Intermediate（如 `CN=gw-shanghai-intermediate`），但签发动作与私钥都在 Control Plane，不在 Gateway。

## 6.2 示例

```yaml
kind: EdgeIdentity

metadata:
  name: node-6f98d7a1

spec:
  nodeID: node-6f98d7a1
  gateway: gw-shanghai
  serial: SN123456
  certFingerprint: xxx

status:
  phase: Active
  issuedAt: "2026-06-10T10:00:00Z"
  expireAt: "2026-09-08T10:00:00Z"
  lastSeenAt: "2026-06-10T10:05:00Z"
```

## 6.3 字段建议

`spec`：

```text
nodeID
gateway
serial
certFingerprint
subject
issuer
```

`status`：

```text
phase
issuedAt
expireAt
revokedAt
lastSeenAt
conditions
```

状态值建议：

```text
Pending
Active
Revoked
Expired
Suspended
```

***

## 7. BootstrapToken

## 7.1 语义

`BootstrapToken` 用于节点首次接入授权。

只允许用于：

```text
首次注册
```

不允许作为长期访问凭证。

## 7.2 示例

```yaml
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
```

## 7.3 字段建议

`spec`：

```text
gateway
expiresIn
maxUses
labels
```

`status`：

```text
phase
usedCount
expireAt
conditions
```

状态值建议：

```text
Active
Frozen
Exhausted
Expired
Revoked
```

***

## 8. Model

## 8.1 语义

`Model` 表示一个可部署的模型版本资源。

## 8.2 示例

```yaml
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

status:
  phase: Published
```

## 8.3 字段建议

`spec`：

```text
name
version
format
checksum
size
artifactURI
signatureURI
framework
precision
labels
```

`status`：

```text
phase
publishedAt
conditions
```

状态值建议：

```text
Draft
Published
Deprecated
Archived
```

***

## 9. RuntimeProfile

## 9.1 语义

`RuntimeProfile` 用于把设备能力映射为推荐 Runtime。

## 9.2 示例

```yaml
kind: RuntimeProfile

metadata:
  name: orin-tensorrt

spec:
  selector:
    gpu: orin
  runtime: tensorrt
  priority: 100
```

## 9.3 字段建议

`spec`：

```text
selector
runtime
priority
runtimeConfig
```

`status`：

```text
phase
conditions
```

***

## 10. ModelDeployment

## 10.1 语义

`ModelDeployment` 表示用户声明的部署意图。

它不是节点级执行任务，而是控制面资源。

## 10.2 示例

```yaml
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
  rollout:
    maxUnavailable: 10%

status:
  phase: Progressing
  desiredNodes: 100
  readyNodes: 80
```

## 10.3 字段建议

`spec`：

```text
model
runtime
target
rollout
policy
```

`status`：

```text
phase
desiredNodes
readyNodes
failedNodes
taskRef
conditions
```

状态值建议：

```text
Pending
Progressing
Available
Degraded
Failed
```

***

## 11. Task

## 11.1 语义

`Task` 表示系统中的执行动作对象。

任务是执行语义，不是资源意图本身。

## 11.2 示例

```yaml
kind: Task

metadata:
  name: task-001

spec:
  type: InstallModel
  scope: Node
  target:
    gateway: gw-shanghai
    node: edge-001
  payload:
    model: qwen3-8b
    version: v1.0.0
  retryPolicy:
    maxRetries: 3
  timeoutSeconds: 1800

status:
  phase: Running
  retryCount: 1
  startedAt: "2026-06-10T10:00:00Z"
```

## 11.3 字段建议

`spec`：

```text
type
scope
parentTaskRef   # NodeTask 指向其 DeploymentTask；区域父任务为空
target
payload
retryPolicy
timeoutSeconds
createdBy
```

说明：`Task` 用 `scope` 区分区域父任务（`Region`）与节点子任务（`Node`），子任务用 `parentTaskRef` 关联父任务，父任务的汇总状态由子任务聚合。

`status`：

```text
phase
retryCount
startedAt
finishedAt
message
conditions
```

状态值建议：

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

***

## 12. 状态模型补充

为了避免对象膨胀，V1 继续保持四类状态拆分：

```text
Inventory
NodeState
Metrics
RuntimeState
```

处理方式：

- `Inventory` 放低频静态信息
- `NodeState` 放节点摘要状态
- `Metrics` 放入时序系统
- `RuntimeState` 放入运行态快照存储

不建议把所有信息都塞入 `EdgeNode.status`。

***

## 13. 资源关联关系

主要关联如下：

```text
Gateway 1 --- N EdgeNode
EdgeNode 1 --- 1 EdgeIdentity
Gateway 1 --- N BootstrapToken
ModelDeployment N --- 1 Model
RuntimeProfile N --- N EdgeNode
ModelDeployment 1 --- N Task(Region)
Task(Region) 1 --- N Task(Node)   # 经 parentTaskRef 关联
Task N --- 1 Gateway / EdgeNode
```

***

## 14. V1 非目标

V1 暂不引入：

- 过多 CRD 细分
- 复杂继承型对象模型
- 将监控指标直接写入资源状态
- 节点即工作负载的 Kubernetes 风格建模

***

## 15. 后续细化项

需要继续补充：

- 字段级 schema
- 默认值与校验规则
- 对象版本演进策略
- 数据库表映射关系
