# Proto / gRPC 约束设计

## 1. 设计目标

本文件用于明确平台 V1 的接口契约与代码生成规范。

固定原则：

```text
proto 是唯一 API 契约源
内部通信统一使用 gRPC
对外 HTTP/JSON 只能由 proto 自动生成
```

这样做的目标是：

```text
避免双份接口定义
避免 REST 与 gRPC 语义漂移
降低服务间集成成本
统一代码生成流程
```

***

## 2. 适用范围

以下接口全部适用：

```text
Control Plane Admin API
Gateway Agent API
Gateway Internal API
```

也就是说：

- 管理面接口先定义成 proto service
- Agent 与 Gateway 之间直接走 gRPC
- Control Plane 与 Gateway 之间直接走 gRPC

***

## 3. package 规划

建议基础 package：

```proto
package edge.ai.api.v1;
```

后续可按领域拆分文件，例如：

```text
gateway.proto
node.proto
identity.proto
bootstrap.proto
model.proto
deployment.proto
task.proto
agent.proto
gateway_sync.proto
common.proto
```

推荐 `go_package`：

```proto
option go_package = "github.com/your-org/ai-edge/api/gen/edge/ai/api/v1;edgeapiv1";
```

***

## 4. service 拆分原则

按领域而不是按页面拆分 service。

建议：

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

不要做：

```text
DashboardService
AdminPageService
AllInOneService
```

因为这些拆分方式不稳定。

***

## 5. message 命名规范

建议：

- 请求消息：`VerbNounRequest`
- 响应消息：`VerbNounResponse`
- 资源消息：`Gateway`、`EdgeNode`、`Task`
- 列表消息：`ListXxxRequest` / `ListXxxResponse`

例如：

```proto
message CreateBootstrapTokenRequest {}
message CreateBootstrapTokenResponse {}
message ListNodesRequest {}
message ListNodesResponse {}
```

字段命名：

```text
snake_case
```

枚举命名：

```text
UPPER_SNAKE_CASE
```

***

## 6. common 消息约定

建议统一复用：

```text
google.protobuf.Timestamp
google.protobuf.Duration
google.protobuf.Empty
```

分页建议：

```proto
message PageRequest {
  int32 page_size = 1;
  string page_token = 2;
}

message PageResponse {
  string next_page_token = 1;
}
```

标签与选择器建议：

```proto
map<string, string> labels = 1;
map<string, string> label_selector = 2;
```

***

## 7. 错误模型约定

gRPC 返回：

```text
status code + ErrorInfo
```

业务码放入：

```text
google.rpc.ErrorInfo.reason
```

建议 metadata：

```text
domain = edge.ai
service = task.v1
request_id = xxx
```

不要在 proto 响应体里再复制一层手写错误结构。

***

## 8. grpc-gateway 边界

如果需要对外暴露 HTTP/JSON：

```text
允许
```

但必须满足：

```text
基于同一套 proto
由生成工具自动产出
HTTP 仅作为对外适配层
不成为系统主契约
```

推荐：

```text
grpc-gateway
buf generate
OpenAPI from proto
```

不建议：

```text
手写第二套 REST DTO
手写第二套路由语义
```

### 8.1 制品文件通道（唯一例外）

大文件（模型 / Agent 包）不走 gRPC，由 `gateway-runtime` 暴露独立 HTTP 文件端点（支持 `Range`，见 `07-api-spec.md` 6.3）。

登记说明：

- 这是“proto 单一契约”的**唯一有意例外**，且只用于字节搬运
- 控制面元数据（下载哪个制品、`checksum`、`signatureURI`）仍由 proto 定义
- 文件端点不引入第二套业务语义，鉴权复用同一 mTLS 身份

***

## 9. 版本演进策略

proto 版本演进原则：

- 不复用字段号
- 删除字段时保留 `reserved`
- 枚举新增只能追加
- 避免频繁改 service 名称

示例：

```proto
message EdgeIdentity {
  reserved 8, 9;
  reserved "legacy_field";
}
```

建议引入：

```text
buf breaking
```

做兼容性检查。

***

## 10. 生成代码策略

建议生成目标：

```text
Go protobuf messages
Go gRPC server/client
grpc-gateway bindings
OpenAPI artifact
```

目录建议：

```text
api/proto/
api/gen/
```

例如：

```text
api/proto/edge/ai/api/v1/*.proto
api/gen/go/...
```

***

## 11. 传输模式建议

V1 默认：

```text
unary RPC
```

原因：

- 更简单
- 更容易做鉴权与重试

连接模型澄清：V1 用 unary RPC，但**复用 mTLS 长连接**承载（不每请求重新握手），并在每个请求上做 EdgeIdentity 状态校验。这与“逐请求身份校验”的安全要求并不矛盾：连接级校验证书链，请求级校验身份状态。

V1 暂不优先：

```text
双向 streaming
长连接任务推送
复杂多路复用
```

***

## 12. 与现有文档的关系

本文件是协议层总约束。

其他文档应遵守：

- `07-api-spec.md` 定义 RPC 与 service
- `09-sequence-flows.md` 使用 RPC 名称描述流程
- `12-error-codes.md` 使用 `gRPC status + business code`

***

## 13. V1 非目标

V1 暂不设计：

- 多语言 SDK 全家桶
- 多协议并行手工维护
- 每个服务各自定义生成脚本

***

## 14. 后续细化项

需要继续补充：

- `buf.yaml` / `buf.gen.yaml` 草案
- proto 文件拆分清单
- 公共 message 复用规则
- grpc metadata 规范
