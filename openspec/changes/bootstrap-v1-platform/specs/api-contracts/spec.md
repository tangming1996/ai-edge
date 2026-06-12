## ADDED Requirements

### Requirement: Proto 单一契约源
平台所有服务间 API SHALL 以 `protobuf` 定义为唯一契约源，package 固定为 `edge.ai.api.v1`，并按领域拆分为独立 `.proto` 文件（gateway / node / identity / bootstrap / model / deployment / task / agent / gateway_sync / common）。任何手写第二套 REST/gRPC 契约 MUST NOT 存在。

#### Scenario: 新增接口必须先定义 proto
- **WHEN** 开发者需要新增一个服务间接口
- **THEN** 必须先在 `api/proto/edge/ai/api/v1/` 下定义 proto service/message
- **AND** 服务端与客户端代码必须由该 proto 生成，而非手写

#### Scenario: 拒绝分叉的 HTTP 契约
- **WHEN** Dashboard / CLI 需要 HTTP/JSON 接口
- **THEN** 该接口只能由同一套 proto 经 `grpc-gateway` 自动生成
- **AND** 不允许手写独立的 REST DTO 或路由语义

### Requirement: gRPC 服务分层与命名规范
系统 SHALL 按领域拆分为以下 service：`GatewayService`、`NodeService`、`IdentityService`、`BootstrapTokenService`、`ModelService`、`DeploymentService`、`TaskService`、`NodeOnboardingService`、`AgentService`、`GatewaySyncService`。message 命名 SHALL 遵循 `VerbNounRequest`/`VerbNounResponse`，字段用 `snake_case`，枚举用 `UPPER_SNAKE_CASE`，时间用 `google.protobuf.Timestamp`，列表查询统一带 `page_size`/`page_token`。

#### Scenario: 列表接口统一分页
- **WHEN** 定义任意 `ListXxxRequest`
- **THEN** 该消息必须包含 `page_size` 与 `page_token` 字段
- **AND** 响应必须返回 `next_page_token`

#### Scenario: service 按领域而非页面拆分
- **WHEN** 评审新增 service
- **THEN** 禁止出现 `DashboardService`/`AllInOneService` 这类按页面聚合的 service

### Requirement: 代码生成工具链
系统 SHALL 使用 `buf` 管理 proto 的 lint、breaking 检查与代码生成，生成目标包括 Go message、gRPC server/client、`grpc-gateway` 绑定与 OpenAPI 产物，统一输出到 `api/gen/`。

#### Scenario: 生成产物来自同一套 proto
- **WHEN** 执行 `buf generate`
- **THEN** Go stub、grpc-gateway 绑定、OpenAPI 均由 `api/proto/` 下同一套 proto 产出
- **AND** `api/gen/` 不包含任何手工编辑的契约代码

#### Scenario: 破坏性变更被拦截
- **WHEN** 提交修改复用了已有字段号或删除字段未保留 reserved
- **THEN** `buf breaking` 检查 MUST 失败并阻止合并

### Requirement: 统一错误模型
gRPC 接口 SHALL 采用"gRPC status code + 业务 error code"两层错误表达，业务码放入 `google.rpc.ErrorInfo.reason`，并在 metadata 携带 `domain`/`service`/`request_id`。响应体内 MUST NOT 再复制一层手写错误结构。

#### Scenario: 身份被吊销的错误返回
- **WHEN** 一个被吊销身份发起 mTLS 请求
- **THEN** 服务返回 `codes.PermissionDenied` 且 `ErrorInfo.reason = IDENTITY_REVOKED`

### Requirement: 制品文件通道为唯一契约例外
大文件（模型 / Agent 包）SHALL NOT 经由 gRPC 传输，而是由 `gateway-runtime` 暴露独立 HTTP Range 文件端点。此例外 MUST 在 proto/gRPC 约束文档中登记，且控制面元数据（制品定位、checksum、signature_uri）仍由 proto 定义。

#### Scenario: 大文件不走 gRPC
- **WHEN** Edge 需要下载模型制品
- **THEN** 通过 HTTP 文件端点（支持 Range）获取字节
- **AND** 通过 gRPC 仅获取"下载哪个制品与其校验值"的元数据
