这是我基于我们整个讨论过程（包括 KubeEdge 问题、DaemonSet 争议、Gateway 角色、状态模型、Task System、Model Runtime、工业场景实际需求等）整理出来的一版 **V1 产品级架构设计文档**。

***

# EdgeAI Runtime Platform

详细设计文档见：

```text
docs/design/
```

## 本地最小启动

当前仓库已经可以启动以下长期运行进程：

- `apiserver`
- `controller`
- `gateway-runtime`
- `edge-agent`

最小联调路径：

```bash
docker compose up -d
make migrate-up
go build ./...
```

本地启动示例：

```bash
DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=postgres DB_NAME=edgeai \
  go run ./cmd/controller

GATEWAY_ID=local-gw CONTROL_PLANE_ADDR=localhost:9090 DB_HOST=localhost DB_PORT=5432 \
DB_USER=postgres DB_PASSWORD=postgres DB_NAME=edgeai HTTP_ADDR=:8081 \
  go run ./cmd/gateway-runtime

go run ./cmd/edge-agent --config /etc/edge-agent/config.json
```

说明：

- `controller` 负责 deployment reconcile loop，不再是占位程序。
- `gateway-runtime` 同时承载 gRPC 和 artifact HTTP 端口，默认 HTTP 端口为 `8081`。
- `edge-agent` 会先加载或 bootstrap 身份，再启动 heartbeat、task runner、证书续签等循环。
- 如未配置 `GATEWAY_TLS_CERT_PATH` / `GATEWAY_TLS_KEY_PATH` / `GATEWAY_CA_CERT_PATH`，`gateway-runtime` 会以非 mTLS 模式启动，便于本地开发联调。

## 一、项目定位

### 愿景

构建一个专门面向边缘 AI 场景的轻量级运行平台。

平台目标不是：

```text
Edge Kubernetes
```

而是：

```text
Edge AI Runtime Platform
```

让企业能够：

- 管理数百\~数万边缘设备
- 统一部署 AI 模型
- 统一升级模型
- 管理异构推理 Runtime
- 实现边缘自治运行

***

## 核心价值

解决当前企业边缘AI部署问题：

### 当前现状

```text
ssh node1
docker run xxx

ssh node2
docker run xxx

scp model.bin
```

规模达到：

```text
100+
```

后完全不可维护。

***

## 平台目标

实现：

```text
Model → Deployment → Runtime → Monitoring
```

全生命周期管理。

***

# 二、架构原则

## Principle 1

Edge Device 不加入 Kubernetes

即：

```text
EdgeNode ≠ Kubernetes Node
```

不使用：

- kubelet
- node registration
- cloudcore
- edgecore

***

## Principle 2

Kubernetes 仅作为 Control Plane

K8s负责：

```text
API
Controller
CRD
Dashboard
Auth
```

不负责：

```text
Edge Runtime
Edge Scheduling
Edge Networking
```

***

## Principle 3

Gateway 是区域控制中心

Gateway负责：

```text
区域接入入口（mTLS 终止）
逐请求身份状态校验
区域模型缓存与制品分发
区域任务投递（无状态 worker）
区域状态聚合
区域自治（在途任务 + 缓存服务）
```

不负责：

```text
CRD同步
Pod同步
Node同步
Bootstrap Token 校验（归 Control Plane）
证书签发（归 Control Plane Signer）
持有 CA 私钥
```

说明：

```text
gateway-runtime 是无状态执行实例
任务 claim 真相源在云端主库
区域中间 CA 私钥只在 Control Plane，不下发 gateway-runtime
```

***

## Principle 4

Task Driven

所有操作均任务化：

```text
InstallModel
StartModel
UpgradeModel
StopModel
DeleteModel
```

***

# 三、总体架构

```text
                        Kubernetes Cluster
┌─────────────────────────────────────────────┐
│                                             │
│              Control Plane                  │
│                                             │
│  API Server                                │
│  Controller Manager                        │
│  Task Engine                               │
│  Model Registry                            │
│  Dashboard                                 │
│  Node Onboarding Service                   │
│                                             │
└──────────────────┬──────────────────────────┘
                   │
                   │ gRPC / mTLS
                   │
          ┌────────▼────────┐
          │    Gateway      │
          │ Region Manager  │
          └───────┬─────────┘
                  │
    ┌─────────────┼─────────────┐
    │             │             │
┌───▼────┐ ┌──────▼───┐ ┌──────▼───┐
│ Edge-1 │ │ Edge-2   │ │ Edge-3   │
└────────┘ └──────────┘ └──────────┘
```

***

# 四、组件设计

***

# 4.1 Control Plane

运行于 Kubernetes。

部署形式：

```text
Deployment
StatefulSet
```

***

## API Server

职责：

- Gateway管理
- Edge管理
- Model管理
- Deployment管理

API：

```text
proto 定义
gRPC service
可选生成 HTTP/JSON Gateway
```

***

## Controller Manager

监听：

```text
ModelDeployment
```

生成：

```text
DeploymentTask
```

***

## Task Engine

整个系统核心。

维护：

```text
Task
TaskStatus
TaskRetry
TaskHistory
```

***

## Model Registry

建议：

```text
MinIO
```

存储：

```text
GGUF
TensorRT
ONNX
Safetensors
```

***

## Node Onboarding Service

这是 V1 必做能力。

职责：

```text
Bootstrap Token
Node Registration
Certificate Issue
Identity Management
Agent Upgrade
Node Revoke
```

它解决的问题不是：

```text
如何执行安装脚本
```

而是：

```text
如何让节点安全地接入平台并获得可持续管理的身份
```

设计原则：

```text
Bootstrap Token
+ 
Mutual TLS
+
Node Identity
```

三层模型。

***

## V1 约束

为了保证安全能力成立，同时不把系统做重，V1 先固定以下约束：

```text
单一平台根 CA
每区域一个 Intermediate CA，私钥只在 Control Plane Signer
Bootstrap Token 校验与证书签发统一在 Control Plane
gateway-runtime 不持有任何 CA 私钥，只做接入代理与 mTLS 终止
Bootstrap Token 只用于首次接入
Agent 后续通信复用长连接 mTLS，逐请求校验 identity 状态
节点主标识首次绑定后不可漂移
```

说明：保留区域 Intermediate CA 是为了让“某区域失陷只影响该区域”的信任隔离仍然成立；但签名动作集中在云端 Signer，避免把 CA 私钥分发到大量 gateway-runtime 实例。

这些约束优先保证：

```text
安全边界清晰
实现足够轻量
后续能力可扩展
```

***

# 4.2 Gateway

***

## Gateway定位

区域控制器。

例如：

```text
Factory-A

Gateway-A
 ├── Edge001
 ├── Edge002
 └── Edge003
```

***

## Gateway职责

### 1 模型缓存

```text
Cloud
 ↓
Gateway
 ↓
Edge
```

避免：

```text
100台设备
重复下载100次
```

***

### 2 状态聚合

收集：

```text
NodeState
RuntimeState
Metrics
```

聚合后上传。

***

### 3 Task Dispatcher

接收：

```text
DeploymentTask
```

转换：

```text
NodeTask
```

分发给 Edge。

***

### 4 区域自治

Cloud失联时（只读协调，不 claim 新任务）：

继续推进：

```text
失联前已 claim 的任务
本地 Restart / 已缓存模型分发
```

不允许失联期发起新的全局 Deploy / Rollback 意图。

***

# 4.3 Edge Agent

***

## 组成

```text
edge-agent

├── bootstrap
├── identity
├── heartbeat
├── downloader
├── runtime-manager
├── metrics
├── task-runner
└── updater
```

***

## 功能

### 注册

```text
NodeOnboardingService/Bootstrap
```

首次接入时使用：

```text
Bootstrap Token + CSR
```

注册完成后切换为：

```text
mTLS
```

***

### 拉取任务

```text
AgentService/PullTasks
```

***

### 执行任务

```text
InstallModel
StartRuntime
UpgradeRuntime
DeleteModel
```

***

### 上报状态

```text
AgentService/ReportHeartbeat
```

***

### 上报指标

```text
AgentService/ReportMetrics
```

***

# 五、对象模型

***

# Gateway

```yaml
kind: Gateway

spec:
  region: shanghai
  labels:
    factory: factory-a
```

***

# EdgeNode

```yaml
kind: EdgeNode

spec:
  gateway: gw-shanghai

  hardware:
    cpu: arm64
    memory: 32Gi

    gpu:
      vendor: nvidia
      model: orin
```

***

# Model

```yaml
kind: Model

spec:
  name: qwen3-8b
  version: v1.0.0

  format: gguf

  checksum: xxx

  size: 8GB
```

***

# RuntimeProfile

```yaml
kind: RuntimeProfile

spec:
  selector:
    gpu: orin

  runtime: tensorrt
```

***

# ModelDeployment

```yaml
kind: ModelDeployment

spec:
  model:
    name: qwen3-8b
    version: v1

  target:
    gateway: shanghai

  rollout:
    maxUnavailable: 10%
```

***

# 六、状态模型设计

这里是系统设计重点。

不要把所有内容放进 Status。

拆分为四类。

***

# 6.1 Inventory

静态资产信息。

```json
{
  "cpu":"arm64",
  "memory":"32Gi",
  "gpu":"orin"
}
```

更新频率：

```text
极低
```

***

# 6.2 NodeState

设备状态。

```json
{
  "online":true,
  "agentVersion":"1.0.0",
  "lastSeen":"..."
}
```

更新频率：

```text
10秒
```

***

# 6.3 Metrics

资源监控。

```json
{
  "cpuUsage":35,

  "memoryUsage":50,

  "gpuUsage":80,

  "gpuMemoryUsage":72,

  "diskUsage":60
}
```

存储：

```text
Prometheus
```

不进入状态表。

***

# 6.4 RuntimeState

AI专属状态。

```json
{
  "loadedModels":[
    {
      "name":"qwen3-8b",
      "version":"v1"
    }
  ]
}
```

***

模型运行状态：

```json
{
  "model":"qwen3-8b",

  "runtime":"tensorrt",

  "state":"Running",

  "latencyP95":120,

  "tokensPerSec":42,

  "qps":18,

  "requests":23891
}
```

单独存储。

***

# 七、任务系统设计

系统核心。

***

# Task

```yaml
taskID: xxx

type: InstallModel

target: edge-001
```

***

# Task类型

```text
InstallModel

DeleteModel

StartRuntime

StopRuntime

RestartRuntime

UpgradeRuntime

UpgradeAgent

CollectLogs
```

***

# Task状态机

```text
Pending
 ↓
Dispatching
 ↓
Running
 ↓
Success
```

失败：

```text
Running
 ↓
Failed
 ↓
Retrying
```

***

# Task特性

必须支持：

```text
幂等
重试
审计
回滚
```

***

# 八、Node Onboarding & Security Architecture

这是整个平台的基础安全能力，而不是附加功能。

如果节点接入阶段只依赖：

```bash
curl install.sh | bash
```

或者长期 Token，那么任何拿到安装地址的设备都可能接入平台，这在真实工业场景中不可接受。

V1 必须从第一天开始具备：

```text
Bootstrap Token
+
Certificate Identity
+
mTLS
+
Node Revoke
```

***

## 8.1 接入目标

目标是让每个 Edge 节点在首次接入时完成：

```text
受控注册
身份签发
证书认证
后续可轮换
后续可吊销
```

并确保：

```text
安装脚本 != 节点身份
Bootstrap Token != 长期凭证
```

***

## 8.2 Bootstrap Token

管理员创建接入令牌：

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
```

生成：

```text
bt_7fd3d1a0a9b2c9c8
```

数据库建议：

```sql
bootstrap_tokens

id
token_hash

gateway_id

max_uses
used_count

expire_at

status
```

注意：

```text
不要存明文 Token
仅存 SHA256(token)
```

***

## 8.3 Agent 安装

安装命令：

```bash
curl https://gateway/install.sh | bash -s -- \
  --gateway gw-shanghai \
  --token bt_7fd3d1a0a9b2c9c8
```

Agent 本地配置：

```yaml
gateway: gw-shanghai

bootstrapToken: bt_7fd3d1a0a9b2c9c8
```

首次启动：

```bash
systemctl start edge-agent
```

***

## 8.4 首次注册流程

### Step 1

Agent 本地生成密钥对。

建议：

```text
ECDSA P256
```

生成：

```text
node.key
node.csr
```

### Step 2

Agent 向 Gateway 发起注册：

```text
rpc Bootstrap(BootstrapRequest) returns (BootstrapResponse)
```

请求：

```json
{
  "token":"bt_xxx",
  "csr":"-----BEGIN CSR-----",
  "hostname":"edge-001",
  "serial":"SN123456",
  "hardware":{
    "cpu":"arm64",
    "memory":"32Gi",
    "gpu":"orin"
  }
}
```

***

## 8.5 接入校验逻辑

Agent 的 `Bootstrap` 请求由 Gateway 接入并转发到 Control Plane 的 `NodeOnboardingService`，由 Control Plane 在一个事务内完成以下校验与签发（Gateway 不读写 Token / Identity 主表）：

### Token 是否存在

```text
token_hash 匹配
```

### Token 是否过期

```text
expire_at > now
```

### Token 是否超限

```text
used_count < max_uses
```

### 接入区域是否匹配

例如：

```text
factory-a
```

只能注册到：

```text
gw-shanghai
```

***

### 节点主标识是否已绑定

V1 必须至少选择一个稳定主标识作为注册唯一性约束。

建议优先：

```text
serial
```

要求：

```text
首次注册成功后，serial 与 nodeID 绑定
后续不能被其他 nodeID 复用
重复注册必须进入续签或显式重置流程
```

这样可以避免：

```text
镜像克隆
重复抢注
身份漂移
```

***

## 8.6 节点身份签发

注册成功后，Control Plane（Signer）生成：

```text
NodeIdentity
```

例如：

```text
node-6f98d7a1
```

并签发：

```text
client certificate
```

例如：

```text
node.crt
```

返回：

```text
nodeID
certificate
ca
```

Agent 保存到：

```text
/etc/edge-agent/

node.key
node.crt
ca.crt
```

***

### CA 层级

为了保证失陷范围可控，同时不引入复杂 PKI，建议：

```text
平台持有单一 Root CA
每区域一个 Intermediate CA
所有 Intermediate CA 私钥只保存在 Control Plane Signer
节点证书由 Control Plane Signer 用对应区域 Intermediate CA 签发
```

这样：

```text
证书链仍带区域 Intermediate，区域信任隔离成立
某区域 Intermediate 失陷时，影响只局限于该区域
CA 私钥不下发到任何 gateway-runtime，降低私钥泄露面
```

***

## 8.7 后续通信模型

Bootstrap Token 在首次注册成功后立即废弃。

后续通信统一使用：

```text
mTLS
```

认证方式是：

```text
client certificate
```

而不是：

```text
token
```

因此以下 RPC 都自动带有设备身份：

```text
ReportHeartbeat
ReportMetrics
PullTasks
```

***

### V1 通信约束

为了让吊销逻辑真正生效、同时在数万节点规模下可扩展，V1 采用：

```text
复用 mTLS 长连接（避免每请求重新握手）
逐请求做身份状态校验（不只在握手时）
```

也就是说，连接在握手时校验证书链，但：

```text
每次 heartbeat
每次 metrics 上报
每次 tasks 拉取
```

都额外检查一次：

```text
certificate fingerprint
identity status（命中本地/区域缓存，短 TTL）
gateway binding
```

吊销不依赖重新握手，而是依赖：

```text
NotifyIdentityEvent 主动推送吊销事件到 gateway-runtime
identity status 缓存短 TTL 兜底
```

这样可以先不依赖复杂的：

```text
OCSP
大规模 CRL 分发
```

也能保证 V1 的吊销语义足够可靠：吊销在“事件推送到达 或 缓存 TTL 到期”后的下一次请求即生效。

***

## 8.8 Node Identity 对象

新增对象：

```yaml
kind: EdgeIdentity
```

结构：

```yaml
spec:
  nodeID: node-6f98d7a1

  gateway: gw-shanghai

  serial: SN123456

  certFingerprint: xxx

  status: Active

  issuedAt: "2026-06-10T10:00:00Z"

  expireAt: "2026-09-08T10:00:00Z"
```

数据库建议：

```sql
edge_identities

id

node_id

serial

fingerprint

gateway_id

created_at

status

issued_at

expire_at

revoked_at

last_seen_at
```

***

## 8.9 证书轮换

不要使用永久证书。

建议：

```text
证书有效期 90 天
```

Agent 在：

```text
到期前 30 天
```

自动续签。

流程：

```text
Agent
 ↓
CSR
 ↓
Gateway
 ↓
Renew Cert
```

***

## 8.10 节点吊销

管理员执行：

```bash
edgectl revoke node-001
```

Gateway 将节点加入：

```text
CRL
```

或：

```text
revoked identities
```

列表。

结果：

```text
新请求立即失效
```

说明：

```text
吊销事件经 NotifyIdentityEvent 推送到 gateway-runtime
gateway-runtime 更新本地 revoked / identity 缓存
生效点是事件到达或缓存 TTL 到期后的下一次请求鉴权
```

***

## 8.11 Agent 升级安全

升级过程不能只依赖下载地址。

升级任务：

```yaml
kind: UpgradeAgentTask

spec:
  version: 1.2.0
```

Agent 下载：

```text
agent.tar.gz
agent.sha256
agent.sig
```

校验：

```text
sha256
signature
version policy
```

签名建议使用：

```text
cosign
```

或者：

```text
ed25519
```

验证通过后再执行：

```text
systemd restart
```

为了避免已签名旧版本被重复下发，建议再增加：

```text
release manifest
```

至少包含：

```text
version
sha256
signature
minAllowedVersion
```

Agent 必须拒绝：

```text
低于当前安全基线的回滚版本
```

***

## 8.12 企业增强能力

后续可以扩展：

### Device Attestation

```text
TPM
```

例如：

```text
Jetson TPM
```

### Secure Boot

```text
验证 boot chain
```

### Hardware Fingerprint

绑定：

```text
serial
mac
uuid
```

***

## 8.13 最终接入安全模型

```text
Admin
  ↓
Create Bootstrap Token

Bootstrap Token
  ↓

Install Agent
  ↓

Generate Key Pair
  ↓

CSR
  ↓

Gateway Verify Token
  ↓

Issue Certificate
  ↓

Create Node Identity
  ↓

mTLS Connection
  ↓

Heartbeat
Metrics
Tasks
```

这个章节应作为正式设计文档中的标准能力，而不是后续补丁。

***

# 九、模型缓存体系

***

## Cloud

保留全部版本

***

## Gateway

默认：

```text
最近10版本
```

***

## Edge

默认：

```text
最近3版本
```

***

## 缓存策略

LRU

***

## 制品下载通道

模型 / Agent 升级包属于大文件（可达数 GB），不走 gRPC 控制面，而是走独立的制品通道：

```text
Gateway 暴露支持 HTTP Range 的鉴权文件端点
Edge 用同一份 mTLS 客户端身份访问
支持断点续传
下载后校验 sha256 + signature 才入缓存
```

链路：

```text
Cloud Object Store
  ↓ (Gateway 未命中则回源)
Gateway 本地缓存 + Range 文件端点
  ↓ (Edge 优先就近拉取)
Edge 本地缓存
```

控制面（gRPC）只负责下发“下载哪个制品、校验值是多少”，不负责搬运字节。

***

# 十、Runtime抽象

用户永远不直接操作：

```text
vLLM
TensorRT
llama.cpp
ONNX Runtime
```

统一：

```yaml
runtime: auto
```

***

## Runtime Mapping

```text
Orin
  ↓
TensorRT

A100
  ↓
vLLM

CPU
  ↓
llama.cpp
```

***

# 十一、断网自治

***

## Edge失联Gateway

继续推理。

依赖：

```text
本地模型缓存
本地Runtime
```

***

## Gateway失联Cloud

由于 claim 真相源在云端主库，失联期 gateway-runtime 降级为只读协调，仅继续：

```text
已 claim / 已缓存任务的继续投递与执行
已缓存模型的继续分发
节点状态本地缓冲
基于本地 revoked 快照拒绝失效身份
```

失联期不允许：

```text
claim 新任务
新节点 bootstrap / 证书签发（依赖云端 Signer）
修改全局资源意图
```

***

恢复后：

```text
增量同步：补传结果、刷新身份、重新开放 claim
```

***

# 十二、Gateway部署模式

这里回答之前的DaemonSet问题。

***

## Gateway属于Kubernetes节点

例如：

```text
工厂边缘服务器
```

加入中心K8s。

***

## Gateway 与 gateway-runtime

这里需要区分：

```text
Gateway = 逻辑上的区域控制中心
gateway-runtime = Gateway 的运行实例
```

也就是说：

```text
一个区域对应一个逻辑 Gateway
多个 gateway-runtime 实例共同承载该 Gateway 的执行能力
```

共同负责：

```text
Edge 接入
任务分发
状态聚合
模型缓存
区域自治
```

***

部署：

```yaml
kind: DaemonSet

metadata:
  name: gateway-runtime
```

原因：

```text
统一升级
统一生命周期管理
自动恢复
```

补充约束：

```text
DaemonSet 只运行在专用 gateway node pool
Edge 连接的是逻辑 Gateway 的统一接入地址
而不是任意漂移到未知区域
gateway-runtime 是无状态执行实例
```

gateway-runtime 之间不共享进程状态，协调一致性靠云端主库承载：

```text
任务 claim 真相源在云端主库（原子 claim）
identity / revoked 状态由云端推送并在本地缓存
cache index 可由实例本地维护，元数据以云端为准
```

也就是说，任意实例宕机或替换都不影响正确性：未完成的 claim 超时后可被其他实例接管，结果回传按 task_id + node_id 幂等归并。

***

## Edge Agent

不使用DaemonSet。

安装方式：

```bash
curl install-edge.sh | bash
```

注册到Gateway。

***

# 十三、MVP路线

## Phase 1

- Bootstrap Token
- Edge 安全注册
- Heartbeat

***

## Phase 2

- Model Registry
- 模型缓存

***

## Phase 3

- Deployment
- Task Engine

***

## Phase 4

- Runtime Adapter
- Runtime Monitoring

***

## Phase 5

- Rollout
- Rollback
- Agent Upgrade
- Cert Rotation / Revoke

***

# 最终定位

这不是：

```text
KubeEdge Lite
```

也不是：

```text
Another Kubernetes
```

而是：

```text
Edge AI Runtime Platform
```

核心竞争力在于：

```text
Task Engine
+
Gateway Architecture
+
Model Lifecycle
+
Runtime Abstraction
+
Edge Autonomy
```

这五个部分共同构成一个真正为边缘 AI 而设计，而不是从 Kubernetes 演化过来的平台。
