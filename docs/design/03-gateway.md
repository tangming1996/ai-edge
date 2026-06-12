# Gateway 详细设计

## 1. 设计目标

Gateway 是区域级控制中心。

它的职责不是把 Kubernetes 控制面下沉到边缘，而是为一个区域内的大量 Edge 节点提供：

```text
统一接入
统一任务分发
统一状态聚合
统一模型缓存
区域自治
```

***

## 2. 核心概念

必须区分两个概念：

```text
Gateway = 逻辑上的区域控制中心
gateway-runtime = Gateway 的运行实例
```

也就是说：

```text
一个区域对应一个逻辑 Gateway
多个 gateway-runtime 实例可以共同承载该 Gateway
```

这也是 `DaemonSet` 部署语义成立的前提。

***

## 3. 设计范围

Gateway 负责：

```text
Edge 接入入口
节点身份校验
区域任务分发
区域状态聚合
模型缓存分发
断网自治
```

不负责：

```text
全局资源建模
全局任务编排
对象存储主仓
Kubernetes Node 语义
```

***

## 4. 部署模型

## 4.1 基本部署

`gateway-runtime` 以如下方式部署：

```yaml
kind: DaemonSet

metadata:
  name: gateway-runtime
```

但不是跑在所有节点上，而是只运行在：

```text
专用 gateway node pool
```

这样可以获得：

```text
统一升级
统一生命周期管理
实例自动恢复
就近接入能力
```

## 4.2 接入模型

Edge 节点连接的是：

```text
逻辑 Gateway 的统一接入地址
```

而不是某个临时实例地址。

这意味着：

- 实例可替换
- 接入地址稳定
- 实例扩缩容不影响节点归属语义

## 4.3 实例状态模型（无状态 worker）

V1 的关键决定：`gateway-runtime` 是**无状态执行实例**，多实例之间不共享进程状态，协调一致性由**云端主库**承载。

```text
任务 claim 真相源在云端主库
identity / revoked 状态由云端推送，实例本地只做缓存
缓存索引由实例本地维护，元数据以云端为准
```

这样可以避免为 V1 引入跨实例共享卷、区域分布式锁或选主协议。

### 4.3.1 任务 claim 放哪里

V1 把 `NodeTask` 的 claim 真相源放在**云端主库**，`gateway-runtime` 通过原子更新抢占：

```text
gateway-runtime 从云端主库按区域查询待分发 NodeTask
原子 claim（owner_instance / claim_expire_at / dispatch_status）
claim 成功才允许向目标节点投递
claim 超时后其他实例可接管
```

`NodeTask` 的 claim 字段落在云端 `tasks` 表（见 `11-database-schema.md`）：

```text
owner_instance
claim_expire_at
dispatch_status
```

### 4.3.2 实例本地只放什么

实例本地磁盘（可选 SQLite/bbolt）只放**可重建、可丢失**的缓存与现场：

```text
identity / revoked 状态缓存（短 TTL，云端推送刷新）
cache index（缓存文件索引，可由扫描重建）
pending upload buffer（与云端失联时的待补传结果）
sync watermark（增量同步游标）
```

原则：本地状态即使整体丢失，也不影响区域级正确性（最多重新拉取/重新扫描）。

### 4.3.3 哪些状态不在本地

V1 明确不依赖实例本地承载：

```text
任务 claim 真相源（在云端主库）
全局资源 / 身份主表（在云端主库）
跨实例共享卷
中心化分布式锁服务
```

### 4.3.4 任务归属与去重

多实例场景下最容易出问题的是重复分发，V1 通过云端 claim 解决：

- 某实例在云端主库原子 claim 成功后，才允许向目标节点分发
- claim 带 `claim_expire_at`，超时后允许其他实例接管
- 节点结果回传时按 `task_id + node_id` 做幂等归并

### 4.3.5 与自治的关系

由于 claim 真相源在云端：

- 云端在线时：任意实例可 claim、可分发，宕机/替换不影响正确性
- 云端失联时：降级为只读协调，仅继续已 claim / 已缓存任务的投递，不再 claim 新任务（详见第 10 节自治边界）

***

## 5. 内部模块拆分

建议拆为以下内部子模块：

```text
edge-access
identity-verifier
task-dispatcher
state-aggregator
cache-manager
autonomy-manager
sync-client
```

### 5.1 edge-access

负责：

```text
接收 Agent 请求
连接接入
路由到对应内部模块
```

### 5.2 identity-verifier

负责：

```text
mTLS 终止与证书链校验（连接级）
证书指纹提取
逐请求 EdgeIdentity 状态校验（命中本地缓存，短 TTL）
gateway 归属校验
吊销状态校验（消费 NotifyIdentityEvent 刷新本地 revoked 缓存）
```

注意：identity-verifier 只做**校验**，不签发证书；身份主表与签发能力都在 Control Plane。`Bootstrap` / `Renew` 由 `edge-access` 转发到 Control Plane。

### 5.3 task-dispatcher

负责：

```text
接收 Control Plane 下发任务
拆分成 NodeTask
按节点状态投递
回收任务结果
```

### 5.4 state-aggregator

负责：

```text
接收 heartbeat
接收 metrics
接收 runtime state
聚合后上送 Control Plane
```

### 5.5 cache-manager

负责：

```text
模型 / Agent 包回源下载（从云端对象存储）
区域缓存
版本清理（LRU）
缓存命中统计
对外提供制品文件端点（支持 HTTP Range，见 9.2）
```

### 5.6 autonomy-manager

负责：

```text
Cloud 失联检测
本地任务继续执行
状态暂存
恢复后增量同步
```

***

## 6. 关键交互

## 6.1 Edge 接入

```text
Edge Agent
  ↓
Gateway edge-access
  ↓
identity-verifier
  ↓
bootstrap / mTLS 处理
```

## 6.2 任务分发

```text
Control Plane
  ↓
Gateway
  ↓
按区域拆分任务
  ↓
投递到具体 Edge
```

## 6.3 状态上报

```text
Edge
  ↓
Gateway 聚合
  ↓
批量上送 Control Plane
```

## 6.4 模型缓存

```text
Cloud Registry
  ↓
Gateway Cache
  ↓
Edge Local Cache
```

***

## 7. 任务分发设计

Gateway 接收的不是最终用户动作，而是来自 Control Plane 的区域级任务。

例如：

```text
Deploy model qwen3-8b to gw-shanghai
```

Gateway 需要把它转换为：

```text
NodeTask(edge-001)
NodeTask(edge-002)
NodeTask(edge-003)
```

要求：

- 任务分发幂等
- 节点离线时支持等待或重试
- 支持滚动分发
- 支持失败结果上送

V1 先用轮询式任务拉取：

```text
AgentService/PullTasks
```

避免复杂长连接与推送通道。

***

## 8. 状态聚合设计

Gateway 接收三类主要状态：

```text
NodeState
Metrics
RuntimeState
```

处理方式：

- `NodeState` 保留最新值
- `Metrics` 聚合后送入时序系统
- `RuntimeState` 保留最新快照并提供查询

V1 原则：

```text
指标与状态分离
聚合优先于原样透传
```

这样可以减轻 Control Plane 压力。

***

## 9. 模型缓存设计

缓存链路：

```text
Cloud
  ↓
Gateway
  ↓
Edge
```

缓存目标：

- 避免大量节点重复回源下载
- 缩短模型部署时延
- 允许局部断网时继续运行

V1 策略：

```text
Gateway 默认保留最近 10 个版本
Edge 默认保留最近 3 个版本
淘汰策略使用 LRU
```

缓存元数据至少包含：

```text
model
version
checksum
size
last_access_at
ref_count
```

这里要区分两层内容：

- `Control Plane` 中心数据库保存缓存元数据与全局索引
- `Gateway` 本地磁盘保存实际缓存文件

V1 推荐 `Gateway` 本地目录：

```text
/var/lib/gateway/
  cache/
    models/
    agents/
  state/
  tasks/
  sync/
  revoked/

/var/log/gateway/
  gateway.log
```

其中：

- `cache/models` 保存模型缓存副本
- `cache/agents` 保存 Agent 升级包缓存副本
- `state` 保存自治运行期状态
- `tasks` 保存待分发与待回传任务快照
- `sync` 保存与云端同步游标和积压队列
- `revoked` 保存吊销身份快照

### 9.1 Gateway 本地状态存储

V1 不在 `Gateway` 上部署独立 PostgreSQL 这类中心数据库，本地只放可重建、可丢失的缓存与现场。

推荐：

```text
本地磁盘
+
可选嵌入式轻量状态库（SQLite / bbolt）
```

本地只承载缓存与自治现场，不承载全局真相源（包括不承载任务 claim 真相源，claim 在云端主库）：

```text
cache index（可重新扫描重建）
revoked / identity 状态缓存（云端推送刷新，短 TTL）
pending status uploads（失联期待补传结果）
sync cursor / watermark
```

### 9.2 制品文件端点

大文件（模型、Agent 升级包）不走 gRPC，由 `cache-manager` 暴露独立的鉴权文件端点：

```text
GET /v1/artifacts/models/{name}/{version}
GET /v1/artifacts/agents/{version}
```

要求：

- 使用与 gRPC 相同的 mTLS 客户端身份鉴权（同一张节点证书）
- 支持 HTTP `Range`，允许断点续传
- 未命中本地缓存时回源云端对象存储，校验通过后写入区域缓存再返回
- Edge 下载完成后自行校验 `sha256 + signature` 才入本地缓存

控制面（gRPC）只下发“下载哪个制品、期望校验值”，不搬运字节。

***

## 10. 区域自治

当 Gateway 与 Control Plane 失联时，仍应支持：

```text
任务继续执行
模型继续分发
状态本地缓存
恢复后增量同步
```

V1 自治边界：

- 不做复杂多节点一致性协议
- 不在 Gateway 本地重建完整控制面
- 只保障已有任务的持续执行与状态缓存

### 10.1 自治期间保留什么

`Gateway` 与云端失联后，仍需要依赖本地状态存储保留：

```text
已接收但未完成的区域任务
已拆分但未下发完成的 NodeTask
节点最近一次在线与运行摘要
本地缓存索引
待上送的 task result / runtime state / heartbeat 摘要
revoked identity 快照
```

### 10.2 自治期间允许什么

V1 允许（只读协调）：

```text
继续投递云端失联前已 claim 的 NodeTask
继续向 Edge 分发已缓存模型 / 制品
继续接收并暂存节点状态
继续基于本地 revoked 缓存拒绝失效身份
```

### 10.3 自治期间不允许什么

V1 不允许：

```text
claim 新的 NodeTask（claim 真相源在云端主库）
新节点 bootstrap / 证书签发 / 续签（依赖云端 Signer）
生成新的全局部署意图
修改全局资源模型
做多 Gateway 一致性选主
把本地状态提升为全局真相源
```

说明：claim 集中在云端是为换取一致性简单；代价是失联期不能开新任务，只能把失联前已 claim 的工作做完。这是 V1 有意识的取舍。

### 10.4 恢复后的增量同步

与 `Control Plane` 恢复连接后，`Gateway` 应按本地同步游标执行：

```text
补传 task results
补传节点状态摘要
补传 runtime state 快照
刷新 identity / revoked 列表
刷新区域任务视图
```

恢复策略建议：

- 先补结果，再补状态
- 同步过程幂等
- 以云端主库为最终真相源

### 10.5 Gateway 与 Control Plane 同步协议

V1 推荐把同步拆成三类 RPC：

```text
PushRegionalTask
SyncGatewayStatus
NotifyIdentityEvent
```

其中职责分别是：

- `PushRegionalTask`: 云端把区域级任务下发到 Gateway
- `SyncGatewayStatus`: Gateway 向云端回传区域摘要状态
- `NotifyIdentityEvent`: 云端把身份变化事件通知到 Gateway

### 10.6 推荐同步顺序

恢复连接后建议按以下顺序同步：

```text
1. 拉取 identity / revoked 增量
2. 补传 task result
3. 补传节点状态摘要
4. 补传 runtime state 快照
5. 刷新区域任务视图
6. 刷新缓存元数据
```

这样做的原因：

- 先同步身份，避免失效节点在恢复窗口继续被接受
- 先同步任务结果，避免控制面任务状态长时间滞后
- 状态与缓存视图放在后面补齐即可

### 10.7 SyncGatewayStatus 内容边界

`SyncGatewayStatus` 不应该传完整原始数据流，而应该传：

```text
gateway runtime summary
connected node count
task backlog
pending upload count
cache summary
node status delta
runtime state delta
```

V1 优先上传：

```text
摘要
增量
幂等结果
```

而不是全量镜像。

### 10.8 NotifyIdentityEvent 内容边界

身份事件至少需要支持：

```text
revoke
suspend
resume
rotate
```

Gateway 收到后应更新本地：

```text
revoked identities
identity status cache
```

并立即影响：

```text
mTLS 身份校验
任务投递可达性判断
```

### 10.9 PushRegionalTask 下发语义

区域任务下发后，Gateway 不应直接把“云端任务对象”原样透传给节点。

而是：

```text
regional task
  ↓
Gateway validate
  ↓
split to NodeTask
  ↓
claim / dispatch
```

要求：

- 同一个 `regional task` 可重复下发但不能重复执行
- `NodeTask` 创建要带稳定 `task_id + node_id`
- 分发结果回传时按幂等键归并

### 10.10 V1 同步失败处理

如果同步失败：

- 不回滚已在边缘完成的本地执行结果
- 把待回传数据保留在本地 `pending upload queue`
- 下次同步继续按游标重试

只有以下内容必须以云端为准重新刷新：

```text
identity status
regional task status
deployment view
```

***

## 11. 安全设计

Gateway 在安全链路中承担：

```text
mTLS 终止与证书链校验
Bootstrap / Renew 请求的接入转发（不读写 Token / Identity 主表）
逐请求 EdgeIdentity 状态检查（本地缓存）
吊销拦截（消费 NotifyIdentityEvent）
```

Gateway 不承担：

```text
Bootstrap Token 校验（在 Control Plane）
证书签发（在 Control Plane Signer）
持有任何 CA 私钥
```

安全原则：

- 任何 CA 私钥都不下发到 `gateway-runtime`
- 每个请求都做身份状态校验（不只在握手时）
- 不接受长期 Token 作为日常通信凭证

***

## 12. 可观测性

Gateway 需要暴露自身指标：

```text
connected_edges
task_dispatch_latency
cache_hit_ratio
sync_backlog
bootstrap_requests
rejected_requests
```

同时提供运行日志：

```text
接入日志
任务日志
缓存日志
同步日志
```

***

## 13. V1 非目标

V1 暂不实现：

- Gateway 间复杂一致性协议
- 多区域全局负载均衡
- 复杂 push 通道
- 自定义边缘网络
- 完整本地化控制面

***

## 14. 后续细化项

需要继续补充：

- Gateway API 列表
- runtime 实例共享状态存储方案
- 缓存目录结构
- 区域自治恢复算法
- Gateway 与 Control Plane 的同步协议
