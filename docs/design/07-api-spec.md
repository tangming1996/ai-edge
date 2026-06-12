# API 规格设计

## 1. 设计目标

本文件定义平台 V1 的统一接口契约规范。

核心原则改为：

```text
所有 API 先定义 proto
所有内部通信统一使用 gRPC
HTTP/JSON 不是主契约，只能由同一套 proto 自动生成
```

也就是说：

- `proto` 是唯一契约源
- `gRPC` 是 Control Plane、Gateway、Edge Agent 之间的默认协议
- Dashboard / CLI 如需 HTTP/JSON，仅允许通过 `grpc-gateway` 或等价生成方案暴露

***

## 2. 接口分层

V1 接口按 service 拆分为三层：

```text
Control Plane Admin Services
Gateway Agent Services
Gateway Internal Services
```

### 2.1 Control Plane Admin Services

面向：

```text
Dashboard
CLI
管理端自动化系统
```

推荐方式：

```text
proto service
gRPC
可选生成 HTTP/JSON 绑定
```

### 2.2 Gateway Agent Services

面向：

```text
Edge Agent
```

要求：

```text
proto service
gRPC unary
mTLS
```

### 2.3 Gateway Internal Services

面向：

```text
Control Plane <-> Gateway
```

要求：

```text
proto service
gRPC
服务鉴权
```

***

## 3. 通用约定

## 3.1 契约源

所有接口以：

```text
Protobuf
```

定义。

生成目标可以包括：

```text
Go server/client
gRPC stubs
grpc-gateway bindings
OpenAPI artifact
```

但这些都应来自同一套 `.proto` 文件，而不是分别维护。

## 3.2 package 与版本

建议：

```proto
package edge.ai.api.v1;
```

版本通过 `proto package` 与生成产物体现，而不是手工维护多套路由。

## 3.3 请求追踪

链路追踪信息通过：

```text
gRPC metadata
```

透传。

建议字段：

```text
x-request-id
x-trace-id
```

## 3.4 时间格式

建议优先使用：

```text
google.protobuf.Timestamp
```

而不是字符串时间。

## 3.5 错误表达

统一采用两层表达：

```text
gRPC status code
+ 
业务 error code
```

例如：

```text
codes.PermissionDenied + IDENTITY_REVOKED
codes.NotFound + TOKEN_NOT_FOUND
```

***

## 4. proto service 划分

V1 建议最少拆为以下 services：

```text
GatewayService
NodeService
IdentityService
BootstrapTokenService
ModelService
DeploymentService
TaskService
NodeOnboardingService
AgentService
GatewaySyncService
```

说明：

- 管理面按资源型 service 组织
- Agent 面按节点行为型 service 组织
- Gateway 内部同步按区域协同型 service 组织

***

## 5. Control Plane Admin Services

## 5.1 BootstrapTokenService

建议 RPC：

```proto
service BootstrapTokenService {
  rpc CreateBootstrapToken(CreateBootstrapTokenRequest) returns (CreateBootstrapTokenResponse);
  rpc ListBootstrapTokens(ListBootstrapTokensRequest) returns (ListBootstrapTokensResponse);
  rpc FreezeBootstrapToken(FreezeBootstrapTokenRequest) returns (FreezeBootstrapTokenResponse);
  rpc RevokeBootstrapToken(RevokeBootstrapTokenRequest) returns (RevokeBootstrapTokenResponse);
}
```

关键消息字段：

```text
name
gateway
expires_in
max_uses
labels
```

## 5.2 GatewayService

建议 RPC：

```proto
service GatewayService {
  rpc CreateGateway(CreateGatewayRequest) returns (CreateGatewayResponse);
  rpc GetGateway(GetGatewayRequest) returns (GetGatewayResponse);
  rpc ListGateways(ListGatewaysRequest) returns (ListGatewaysResponse);
}
```

## 5.3 NodeService

建议 RPC：

```proto
service NodeService {
  rpc GetNode(GetNodeRequest) returns (GetNodeResponse);
  rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);
  rpc RevokeNode(RevokeNodeRequest) returns (RevokeNodeResponse);
}
```

## 5.4 IdentityService

建议 RPC：

```proto
service IdentityService {
  rpc GetIdentity(GetIdentityRequest) returns (GetIdentityResponse);
  rpc ListIdentities(ListIdentitiesRequest) returns (ListIdentitiesResponse);
}
```

## 5.5 ModelService

建议 RPC：

```proto
service ModelService {
  rpc CreateModel(CreateModelRequest) returns (CreateModelResponse);
  rpc GetModel(GetModelRequest) returns (GetModelResponse);
  rpc ListModels(ListModelsRequest) returns (ListModelsResponse);
}
```

## 5.6 DeploymentService

建议 RPC：

```proto
service DeploymentService {
  rpc CreateDeployment(CreateDeploymentRequest) returns (CreateDeploymentResponse);
  rpc GetDeployment(GetDeploymentRequest) returns (GetDeploymentResponse);
  rpc ListDeployments(ListDeploymentsRequest) returns (ListDeploymentsResponse);
}
```

## 5.7 TaskService

建议 RPC：

```proto
service TaskService {
  rpc GetTask(GetTaskRequest) returns (GetTaskResponse);
  rpc ListTasks(ListTasksRequest) returns (ListTasksResponse);
  rpc CancelTask(CancelTaskRequest) returns (CancelTaskResponse);
}
```

***

## 6. Gateway Agent Services

这部分由 Edge Agent 调用，统一要求：

```text
gRPC
mTLS（复用长连接，逐请求做身份状态校验）
proto-generated client
```

接入拓扑约定：

- Agent 只连接逻辑 Gateway 的统一接入地址
- `gateway-runtime` 终止 mTLS 并做身份状态校验
- `NodeOnboardingService` 的**实现在 Control Plane**，`gateway-runtime` 仅做接入转发，不读写 Token / Identity 主表、不签发证书
- `AgentService` 由 `gateway-runtime` 直接处理（聚合/投递/转发）

## 6.1 NodeOnboardingService（Control Plane 实现，Gateway 代理）

`Bootstrap` 是唯一允许 Bootstrap Token、且为服务端 TLS（非 mTLS）的接口；`Renew` 走现有 mTLS 身份。两者都由 Control Plane 校验与签发。

建议 RPC：

```proto
service NodeOnboardingService {
  rpc Bootstrap(BootstrapRequest) returns (BootstrapResponse);
  rpc Renew(RenewRequest) returns (RenewResponse);
}
```

`BootstrapRequest` 关键字段：

```text
token
csr_pem
hostname
serial
hardware
```

`BootstrapResponse` 关键字段：

```text
node_id
certificate_pem
ca_pem
expire_at
```

典型业务错误码：

```text
TOKEN_NOT_FOUND
TOKEN_EXPIRED
TOKEN_EXHAUSTED
GATEWAY_MISMATCH
IDENTITY_CONFLICT
CSR_INVALID
```

## 6.2 AgentService

建议 RPC：

```proto
service AgentService {
  rpc ReportHeartbeat(ReportHeartbeatRequest) returns (ReportHeartbeatResponse);
  rpc ReportMetrics(ReportMetricsRequest) returns (ReportMetricsResponse);
  rpc ReportRuntimeState(ReportRuntimeStateRequest) returns (ReportRuntimeStateResponse);
  rpc PullTasks(PullTasksRequest) returns (PullTasksResponse);
  rpc ReportTaskResult(ReportTaskResultRequest) returns (ReportTaskResultResponse);
}
```

设计要求：

- V1 以 unary RPC 为主
- 复用 mTLS 长连接承载这些 unary 请求，不每请求重新握手；但每请求都校验 EdgeIdentity 状态
- `PullTasks` 使用轮询式请求，不做 server streaming
- `ReportTaskResult` 必须天然支持幂等（按 `task_id + node_id`）

注意：模型 / Agent 包等大文件**不在 `AgentService` 内传输**，单独走制品文件通道（见 6.3）。

***

## 6.3 制品文件通道（非 gRPC）

大文件不适合 gRPC unary/streaming，V1 单独定义一个由 `gateway-runtime` 暴露的 HTTP 文件通道：

```text
GET /v1/artifacts/models/{name}/{version}
GET /v1/artifacts/agents/{version}
```

约定：

- 鉴权复用同一张节点 mTLS 客户端证书（与 gRPC 同一身份体系）
- 支持 `Range` 请求与断点续传
- 响应头携带 `checksum`，Edge 落盘前校验 `sha256 + signature`
- 未命中区域缓存时由 `gateway-runtime` 回源云端对象存储

这是 `proto + gRPC` 单一契约原则的**有意例外**：控制面元数据仍由 proto 定义，字节搬运走文件通道。该例外应在 `13-proto-grpc-guidelines.md` 中登记。

***

## 7. Gateway Internal Services

这部分用于 Control Plane 与 Gateway 之间的区域协同。

## 7.1 GatewaySyncService

建议 RPC：

```proto
service GatewaySyncService {
  rpc PushRegionalTask(PushRegionalTaskRequest) returns (PushRegionalTaskResponse);
  rpc SyncGatewayStatus(SyncGatewayStatusRequest) returns (SyncGatewayStatusResponse);
  rpc NotifyIdentityEvent(NotifyIdentityEventRequest) returns (NotifyIdentityEventResponse);
}
```

用途：

```text
区域任务下发
节点汇总同步
缓存状态同步
吊销 / suspend / resume 事件通知
```

claim 约定：`PushRegionalTask` 只把区域父任务交给区域；`gateway-runtime` 把父任务展开为 `NodeTask` 后，对**云端主库**做原子 claim 再投递（claim 真相源不在 Gateway 本地）。`NotifyIdentityEvent` 用于把吊销/挂起事件推送到实例，刷新本地身份缓存。

***

## 8. 消息设计原则

消息字段建议：

- 使用 `snake_case`
- 避免把内部数据库字段直接暴露到消息层
- 时间优先使用 `Timestamp`
- 枚举优先使用 `enum`
- 列表查询统一支持 `page_size` / `page_token`

例如：

```proto
message ListNodesRequest {
  string gateway = 1;
  string status = 2;
  map<string, string> label_selector = 3;
  int32 page_size = 4;
  string page_token = 5;
}
```

***

## 9. 鉴权设计

## 9.1 管理面

管理面 service 使用：

```text
用户身份鉴权
```

可结合：

```text
OIDC
JWT
Session
```

如果对外暴露 HTTP/JSON，也必须复用同一套鉴权上下文。

## 9.2 Agent 日常通信

统一使用：

```text
mTLS
```

通过 gRPC transport 完成身份建立。

## 9.3 首次注册

仅：

```text
NodeOnboardingService/Bootstrap
```

允许：

```text
Bootstrap Token
```

成功后即废弃。

***

## 10. 幂等与重试

关键 RPC 需要具备幂等语义：

- `Bootstrap`
- `ReportTaskResult`
- `PushRegionalTask`

建议依赖：

```text
稳定业务 ID
request_id
task_id
node_id
```

而不是额外再造一套 HTTP 风格幂等头。

***

## 11. 对外 HTTP/JSON 边界

如果 Dashboard / CLI 需要 HTTP/JSON：

```text
允许
```

但前提是：

```text
由同一套 proto 自动生成
不手写第二套 REST 契约
不允许 HTTP 版本与 gRPC 版本分叉
```

推荐：

```text
grpc-gateway
buf generate
OpenAPI 由 proto 衍生
```

***

## 12. V1 非目标

V1 暂不定义：

- 双向 streaming 任务通道
- 手写 REST 契约体系
- 多套协议各自演进
- WebSocket 推送协议

***

## 13. 后续细化项

需要继续补充：

- `.proto` 文件拆分方案
- message 字段级 schema
- enum 清单
- grpc-gateway 暴露范围
- protobuf breaking change 规则
