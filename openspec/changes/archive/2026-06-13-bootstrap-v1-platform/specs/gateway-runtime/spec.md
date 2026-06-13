## ADDED Requirements

### Requirement: 无状态区域接入代理
`gateway-runtime` SHALL 作为逻辑 Gateway 的无状态执行实例，以 DaemonSet 部署在专用 gateway node pool，承担 Edge 接入的 mTLS 终止与请求级身份状态校验。实例之间 MUST NOT 共享进程状态，任意实例宕机或替换 MUST NOT 影响区域正确性。

#### Scenario: 实例替换不影响正确性
- **WHEN** 一个 gateway-runtime 实例被替换
- **THEN** Edge 通过统一接入地址连接到其他实例，未完成的 claim 超时后被接管

#### Scenario: 转发 Bootstrap 而不处理身份主表
- **WHEN** Agent 发起 `Bootstrap`/`Renew` 请求
- **THEN** gateway-runtime 仅作接入转发到 Control Plane，不读写 Token/Identity 主表，不持有 CA 私钥

### Requirement: 任务展开与 claim 分发
`gateway-runtime` SHALL 接收 Control Plane 经 `PushRegionalTask` 下发的区域父任务，将其展开为本区域可投递节点的 `NodeTask`，对云端主库做原子 claim 后再向节点投递，并回传实际展开数与成功/失败结果计数。

#### Scenario: 父任务展开为节点任务
- **WHEN** gateway-runtime 收到一个 DeploymentTask
- **THEN** 仅针对本区域可投递节点生成 NodeTask 并 claim 后投递
- **AND** 向 Control Plane 回传展开数与 readyNodes/failedNodes 计数

### Requirement: 状态聚合
`gateway-runtime` SHALL 收集区域内节点的 NodeState、RuntimeState 与节点结果，聚合后经 `SyncGatewayStatus` 上传 Control Plane。

#### Scenario: 聚合后上报
- **WHEN** 区域内多个节点上报心跳与运行时状态
- **THEN** gateway-runtime 聚合后批量上报，而非每节点逐条透传到云端主库

### Requirement: 制品 Range 文件通道
`gateway-runtime` SHALL 暴露支持 HTTP `Range` 的鉴权文件端点（如 `/v1/artifacts/models/{name}/{version}`、`/v1/artifacts/agents/{version}`），鉴权复用节点 mTLS 客户端身份，支持断点续传；未命中区域缓存时由 `gateway-runtime` 回源云端对象存储。

#### Scenario: 缓存未命中回源
- **WHEN** Edge 请求一个本区域未缓存的模型制品
- **THEN** gateway-runtime 回源云端对象存储拉取并缓存，再向 Edge 提供带 checksum 的下载

### Requirement: 区域缓存层级与 LRU
`gateway-runtime` SHALL 维护区域模型缓存（默认保留最近 10 个版本，LRU 回收），缓存元数据以云端 `gateway_cache_entries` 为准，本地 cache index 可重建。

#### Scenario: 避免重复下载
- **WHEN** 区域内 100 台设备需要同一模型版本
- **THEN** 该模型只回源下载一次，其余设备从 Gateway 区域缓存就近拉取

### Requirement: 断网只读自治
当 `gateway-runtime` 与 Cloud 失联时，系统 SHALL 降级为只读协调，仅继续推进失联前已 claim/已缓存的任务投递与执行、已缓存模型分发、节点状态本地缓冲、按本地 revoked 快照拒绝失效身份。失联期 MUST NOT claim 新任务、MUST NOT 进行新节点 bootstrap/签发、MUST NOT 修改全局资源意图。恢复后 SHALL 增量同步（补传结果、刷新身份、重新开放 claim）。

#### Scenario: 失联期拒绝新接入
- **WHEN** Gateway 与 Cloud 失联期间有新设备尝试 Bootstrap
- **THEN** 拒绝新接入（依赖云端 Signer），但已接入节点的存量任务与缓存模型继续可用

#### Scenario: 恢复后增量同步
- **WHEN** Gateway 与 Cloud 恢复连接
- **THEN** 补传失联期结果、刷新身份缓存并重新开放 claim
