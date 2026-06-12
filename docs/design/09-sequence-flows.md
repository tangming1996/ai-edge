# 关键时序流程设计

## 1. 设计目标

本文件用于固定系统 V1 的关键交互时序。

作用：

```text
统一模块交互理解
明确请求方向
明确状态变化点
帮助后续实现 API 与状态机
```

V1 优先定义三个最关键流程：

```text
Node Onboarding
Model Deployment
Certificate Renew / Revoke
```

***

## 2. Node Onboarding

### 2.1 流程目标

完成：

```text
首次安装
首次注册
证书签发
身份建立
切换到 mTLS
```

### 2.2 时序

```text
Admin
  ↓
Control Plane
  ↓ create BootstrapToken
Gateway
  ↓ expose install endpoint
Edge Agent
  ↓ install（含内置 caBundle）
Edge Agent
  ↓ generate key pair / csr
Edge Agent
  ↓ Bootstrap over server-TLS（用 caBundle 校验 Gateway）
Gateway
  ↓ forward Bootstrap to Control Plane
Control Plane
  ↓ verify token / serial / region binding（云端事务）
Control Plane (Signer)
  ↓ issue certificate（区域 Intermediate CA）
Control Plane
  ↓ create EdgeIdentity + used_count+1（同事务）
Edge Agent
  ↓ persist cert files
Edge Agent
  ↓ switch to mTLS
```

### 2.3 详细步骤

1. 管理员创建 `BootstrapToken`
2. 安装脚本把 `gateway`、`token` 与内置 `caBundle` 写入本地配置
3. Agent 首次启动，生成 `node.key` 与 `node.csr`
4. Agent 以服务端 TLS（用 `caBundle` 校验 Gateway）调用 `NodeOnboardingService/Bootstrap`
5. Gateway 接入并转发到 Control Plane
6. Control Plane 在云端事务内校验 `token_hash`、过期时间、使用次数、区域归属、`serial` 绑定
7. Control Plane Signer 生成 `nodeID` 并用区域 Intermediate CA 签发客户端证书
8. Control Plane 在同事务创建 `EdgeIdentity` 并 `used_count+1`
9. Agent 落盘 `node.key`、`node.crt`、`ca.crt`
10. Bootstrap Token 作废，后续通信切换为 `mTLS`

### 2.4 关键状态点

```text
BootstrapToken: Active -> Exhausted / Revoked
EdgeIdentity: Pending -> Active
EdgeNode: Registered -> Online
```

### 2.5 失败分支

- `token` 失效：注册失败，等待人工处理
- `serial` 冲突：拒绝注册，避免身份漂移
- `csr` 非法：返回错误，不进入签发

***

## 3. Model Deployment

### 3.1 流程目标

完成：

```text
部署意图创建
任务生成
区域拆分
节点安装
Runtime 启动
结果回收
```

### 3.2 时序

```text
User
  ↓
API Server
  ↓ create ModelDeployment
Controller Manager
  ↓ generate DeploymentTask
Task Engine
  ↓ bind target Gateway
Gateway
  ↓ split to NodeTask
Edge Agent
  ↓ pull task
Edge Agent
  ↓ download model from Gateway
Edge Agent
  ↓ verify checksum / signature
Edge Agent
  ↓ load model / start runtime
Edge Agent
  ↓ report task result
Gateway
  ↓ aggregate node results
Task Engine
  ↓ update task status
Dashboard
  ↓ show rollout result
```

### 3.3 详细步骤

1. 用户创建 `ModelDeployment`
2. `Controller Manager` 按目标区域 + 标签从已知 `EdgeNode` 计算 `desiredNodes`，并生成区域 `DeploymentTask`（解析 `runtime: auto`）
3. `Task Engine` 把区域父任务绑定到对应 `Gateway`
4. `gateway-runtime` 把父任务展开为多个 `NodeTask`（带 `parentTaskRef` 与解析后的 runtime），对**云端主库**原子 claim 后投递
5. `Edge Agent` 通过 `AgentService/PullTasks` 拉取任务
6. Agent 通过 Gateway 制品文件端点（HTTP Range）下载模型
7. Agent 校验 `sha256 + signature` 后入缓存
8. `runtime-manager` 按 `NodeTask.payload` 中已解析的 runtime 启动
9. Agent 通过 `ReportTaskResult` / `ReportRuntimeState` 回传结果和运行状态
10. 子任务结果聚合到父任务，`Task Engine` 更新整体状态与 `readyNodes / failedNodes`

### 3.4 关键状态点

```text
ModelDeployment: Pending -> Progressing -> Available / Failed
Task: Pending -> Dispatching -> Running -> Success / Failed
RuntimeState: Starting -> Running
```

### 3.5 失败分支

- 下载失败：任务可重试
- 签名校验失败：任务直接失败，不重试
- Runtime 不兼容：任务失败并上送原因
- 节点离线：任务保持待投递或进入延迟分发

***

## 4. Certificate Renew

### 4.1 流程目标

完成：

```text
证书续签
身份延续
无 Bootstrap Token 参与
```

### 4.2 时序

```text
Edge Agent
  ↓ check expireAt
Edge Agent
  ↓ generate renew csr（用现有 mTLS 身份）
Gateway
  ↓ forward Renew to Control Plane
Control Plane
  ↓ verify EdgeIdentity status (Active)
Control Plane (Signer)
  ↓ issue new certificate
Edge Agent
  ↓ replace local cert
Edge Agent
  ↓ continue mTLS communication
```

### 4.3 详细步骤

1. Agent 定期检查本地证书过期时间
2. 在到期前 30 天发起续签
3. Agent 调用 `NodeOnboardingService/Renew`（经 Gateway 转发到 Control Plane）
4. Gateway 用当前 `mTLS` 身份完成接入校验
5. Control Plane 检查 `EdgeIdentity.status == Active`
6. Control Plane Signer 签发新证书并更新身份记录（新 `fingerprint`）
7. Agent 原子替换本地证书
8. 后续连接自动使用新证书

说明：续签依赖 Control Plane Signer 在线；证书 90 天有效、提前 30 天续签，区域短暂断网不影响存量证书。

### 4.4 失败分支

- 续签失败但旧证书仍有效：继续使用旧证书并后台重试
- 身份已吊销：拒绝续签
- Gateway 不可达：延迟重试

***

## 5. Node Revoke

### 5.1 流程目标

完成：

```text
节点身份撤销
后续请求拒绝
```

### 5.2 时序

```text
Admin
  ↓ revoke node
Control Plane
  ↓ update EdgeIdentity status = Revoked
Gateway
  ↓ refresh revoked identities
Edge Agent
  ↓ next request via mTLS
Gateway
  ↓ reject request
Task Engine / Dashboard
  ↓ show node revoked
```

### 5.3 详细步骤

1. 管理员发起节点吊销
2. Control Plane 更新 `EdgeIdentity.phase = Revoked`
3. Control Plane 通过 `NotifyIdentityEvent` 推送吊销事件，`gateway-runtime` 刷新本地 revoked / identity 缓存
4. 节点下一次调用 `ReportHeartbeat`、`ReportMetrics`、`PullTasks` 时被拒绝
5. Dashboard 展示节点已吊销

### 5.4 生效语义

V1 中定义为：

```text
事件到达或缓存 TTL 到期后，下一次请求即失效
```

这与“复用长连接 + 逐请求身份状态校验”的通信模型一致。

***

## 6. Agent Upgrade

### 6.1 流程目标

完成：

```text
安全升级
防回滚
结果可追踪
```

### 6.2 时序

```text
Admin
  ↓ trigger upgrade
Control Plane
  ↓ create UpgradeAgent task
Gateway
  ↓ dispatch NodeTask
Edge Agent
  ↓ pull task
Edge Agent
  ↓ download artifact / sha / sig / manifest
Edge Agent
  ↓ verify checksum / signature / minAllowedVersion
Edge Agent
  ↓ replace binary
systemd
  ↓ restart edge-agent
Edge Agent
  ↓ resume heartbeat
```

### 6.3 失败分支

- `sha256` 不匹配：失败
- `signature` 校验失败：失败
- 版本低于安全基线：拒绝执行
- 重启失败：记录错误并等待人工处理

***

## 7. Gateway 断网自治

### 7.1 流程目标

在 `Gateway` 与 `Control Plane` 失联时，区域内仍维持基本执行能力。

### 7.2 时序

```text
Control Plane unavailable
  ↓
Gateway detect sync failure
  ↓
Gateway switch to autonomy mode
  ↓
Edge Agent continue pull tasks already available
  ↓
Gateway continue local cache / local aggregation
  ↓
Gateway buffer state updates
  ↓
Control Plane recover
  ↓
Gateway incremental sync
```

### 7.3 自治边界

允许：

```text
继续执行失联前已 claim 的任务
继续模型 / 制品分发（已缓存）
继续状态缓存
恢复后增量同步并重新开放 claim
```

不允许：

```text
claim 新的 NodeTask（claim 真相源在云端主库）
新节点 bootstrap / 证书签发 / 续签（依赖云端 Signer）
在本地重建完整控制面
执行复杂全局调度
修改全局资源意图
```

***

## 8. V1 时序设计原则

所有关键流程继续遵守：

- 控制面负责意图
- Gateway 负责区域协调
- Edge Agent 负责本机执行
- 安全接入优先于功能扩展
- 长耗时操作通过 Task 驱动
- 接口契约统一来自 proto
- 内部交互统一走 gRPC

***

## 9. 后续细化项

需要继续补充：

- Mermaid 时序图版本
- 错误分支细化图
- Rollout / Rollback 时序
- Gateway 多实例共享状态时序
