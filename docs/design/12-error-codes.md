# Error Codes 设计

## 1. 设计目标

本文件定义平台 V1 统一错误表达规范。

在协议层统一改为：

```text
gRPC status code
+
业务 error code
```

目标：

```text
统一 gRPC 错误表达
统一 Agent / Gateway / Control Plane 的失败语义
便于排障、审计和前端展示
```

***

## 2. 错误结构

V1 推荐两层结构：

### 2.1 transport 层

由：

```text
gRPC status
```

表达基础类别，例如：

```text
InvalidArgument
NotFound
AlreadyExists
PermissionDenied
Unauthenticated
FailedPrecondition
Unavailable
Internal
```

### 2.2 业务层

由稳定业务码表达领域语义，例如：

```text
TOKEN_EXPIRED
IDENTITY_REVOKED
TASK_TIMEOUT
MODEL_SIGNATURE_INVALID
```

推荐放入：

```text
google.rpc.ErrorInfo
```

或等价错误详情结构中。

***

## 3. 命名规则

建议：

```text
大写蛇形
按领域分组
保持长期稳定
```

例如：

```text
TOKEN_EXPIRED
IDENTITY_REVOKED
TASK_TIMEOUT
MODEL_SIGNATURE_INVALID
```

***

## 4. 分类

建议分为：

```text
COMMON
AUTH
BOOTSTRAP
IDENTITY
TASK
MODEL
RUNTIME
GATEWAY
SYSTEM
UPGRADE
```

***

## 5. gRPC 状态码选型原则

建议映射原则：

- 参数错误使用 `InvalidArgument`
- 缺失资源使用 `NotFound`
- 冲突使用 `AlreadyExists` 或 `Aborted`
- 认证失败使用 `Unauthenticated`
- 权限或身份不允许使用 `PermissionDenied`
- 前置条件不满足使用 `FailedPrecondition`
- 依赖不可用使用 `Unavailable`
- 内部未分类错误使用 `Internal`

不要把所有失败都塞给：

```text
Internal
```

***

## 6. Common 类

### INVALID_ARGUMENT

建议 gRPC status：

```text
InvalidArgument
```

可重试：

```text
false
```

### NOT_FOUND

建议 gRPC status：

```text
NotFound
```

### CONFLICT

建议 gRPC status：

```text
AlreadyExists / Aborted
```

### RATE_LIMITED

建议 gRPC status：

```text
ResourceExhausted
```

可重试：

```text
true
```

### INTERNAL_ERROR

建议 gRPC status：

```text
Internal
```

***

## 7. Auth 类

### UNAUTHORIZED

建议 gRPC status：

```text
Unauthenticated
```

### FORBIDDEN

建议 gRPC status：

```text
PermissionDenied
```

### MTLS_REQUIRED

建议 gRPC status：

```text
Unauthenticated
```

### CERTIFICATE_INVALID

建议 gRPC status：

```text
Unauthenticated
```

### CERTIFICATE_EXPIRED

建议 gRPC status：

```text
Unauthenticated
```

***

## 8. Bootstrap 类

### TOKEN_NOT_FOUND

建议 gRPC status：

```text
NotFound
```

### TOKEN_EXPIRED

建议 gRPC status：

```text
FailedPrecondition
```

### TOKEN_EXHAUSTED

建议 gRPC status：

```text
ResourceExhausted
```

### TOKEN_FROZEN

建议 gRPC status：

```text
PermissionDenied
```

### GATEWAY_MISMATCH

建议 gRPC status：

```text
PermissionDenied
```

### CSR_INVALID

建议 gRPC status：

```text
InvalidArgument
```

***

## 9. Identity 类

### IDENTITY_CONFLICT

建议 gRPC status：

```text
AlreadyExists
```

### IDENTITY_REVOKED

建议 gRPC status：

```text
PermissionDenied
```

### IDENTITY_EXPIRED

建议 gRPC status：

```text
Unauthenticated
```

### IDENTITY_SUSPENDED

建议 gRPC status：

```text
PermissionDenied
```

### IDENTITY_NOT_ACTIVE

建议 gRPC status：

```text
FailedPrecondition
```

### RENEW_NOT_ALLOWED

建议 gRPC status：

```text
FailedPrecondition
```

***

## 10. Task 类

### TASK_NOT_FOUND

建议 gRPC status：

```text
NotFound
```

### TASK_CONFLICT

建议 gRPC status：

```text
Aborted
```

### TASK_TIMEOUT

建议 gRPC status：

```text
DeadlineExceeded
```

### TASK_CANCELLED

建议 gRPC status：

```text
Cancelled
```

### TASK_RESULT_DUPLICATED

建议 gRPC status：

```text
AlreadyExists
```

### TASK_ALREADY_CLAIMED

含义：NodeTask 已被其他 `gateway-runtime` 实例 claim 且未过期。

建议 gRPC status：

```text
Aborted
```

可重试：

```text
true（退避后由 claim 持有者推进，或 claim 过期后接管）
```

***

## 11. Model 类

### MODEL_NOT_FOUND

建议 gRPC status：

```text
NotFound
```

### MODEL_VERSION_CONFLICT

建议 gRPC status：

```text
AlreadyExists
```

### MODEL_CHECKSUM_INVALID

建议 gRPC status：

```text
DataLoss
```

### MODEL_SIGNATURE_INVALID

建议 gRPC status：

```text
PermissionDenied
```

### MODEL_DOWNLOAD_FAILED

建议 gRPC status：

```text
Unavailable
```

### ARTIFACT_NOT_FOUND

含义：制品文件端点上找不到对应模型/Agent 包。

建议 gRPC status / HTTP：

```text
NotFound / 404
```

### ARTIFACT_RANGE_NOT_SATISFIABLE

含义：制品文件端点 `Range` 请求区间非法。

建议 HTTP：

```text
416 Range Not Satisfiable
```

说明：制品通道是 HTTP 文件通道，错误以 HTTP 状态码为主；若需要在 gRPC 任务结果中表达，归一到 `MODEL_DOWNLOAD_FAILED`。

***

## 12. Runtime 类

### RUNTIME_NOT_SUPPORTED

建议 gRPC status：

```text
FailedPrecondition
```

### RUNTIME_START_FAILED

建议 gRPC status：

```text
Internal
```

### RUNTIME_STOP_FAILED

建议 gRPC status：

```text
Internal
```

### RUNTIME_STATUS_UNKNOWN

建议 gRPC status：

```text
Unknown
```

### MODEL_NOT_LOADED

建议 gRPC status：

```text
FailedPrecondition
```

***

## 13. Gateway 类

### GATEWAY_NOT_FOUND

建议 gRPC status：

```text
NotFound
```

### GATEWAY_OFFLINE

建议 gRPC status：

```text
Unavailable
```

### GATEWAY_SYNC_DELAYED

建议 gRPC status：

```text
Unavailable
```

### GATEWAY_CAPACITY_EXCEEDED

建议 gRPC status：

```text
ResourceExhausted
```

***

## 14. System 类

### DATABASE_UNAVAILABLE

建议 gRPC status：

```text
Unavailable
```

### OBJECT_STORE_UNAVAILABLE

建议 gRPC status：

```text
Unavailable
```

### DEPENDENCY_UNAVAILABLE

建议 gRPC status：

```text
Unavailable
```

### SERIALIZATION_ERROR

建议 gRPC status：

```text
Internal
```

***

## 15. Upgrade 类

### VERSION_POLICY_VIOLATION

建议 gRPC status：

```text
FailedPrecondition
```

### RELEASE_MANIFEST_INVALID

建议 gRPC status：

```text
InvalidArgument
```

### AGENT_UPGRADE_FAILED

建议 gRPC status：

```text
Internal
```

***

## 16. Agent 行为建议

### 16.1 不重试

```text
TOKEN_EXPIRED
TOKEN_EXHAUSTED
IDENTITY_REVOKED
MODEL_SIGNATURE_INVALID
RUNTIME_NOT_SUPPORTED
VERSION_POLICY_VIOLATION
```

### 16.2 可延迟重试

```text
RATE_LIMITED
MODEL_DOWNLOAD_FAILED
GATEWAY_OFFLINE
DATABASE_UNAVAILABLE
DEPENDENCY_UNAVAILABLE
```

### 16.3 需要人工介入

```text
IDENTITY_CONFLICT
CERTIFICATE_INVALID
MODEL_SIGNATURE_INVALID
RELEASE_MANIFEST_INVALID
```

***

## 17. 前端与网关派生

如果管理面通过 `grpc-gateway` 暴露 HTTP/JSON：

- HTTP 状态码只是从 `gRPC status` 派生
- 业务码仍保持不变
- 前端不要依赖某个手写 REST 错误语义

推荐展示三层信息：

- `grpc_status`
- `business_code`
- `message`

***

## 18. V1 非目标

V1 暂不设计：

- 单独维护一套 REST 错误码体系
- 多语言错误模板引擎
- 每个子系统各自独立前缀规范

***

## 19. 后续细化项

需要继续补充：

- `google.rpc.ErrorInfo` 载荷约定
- grpc-gateway HTTP 状态映射表
- Dashboard 错误提示文案规范
- 可重试错误的退避策略建议
