# 设计文档目录

这个目录用于承载 `EdgeAI Runtime Platform` 的详细设计文档。

根目录 `README.md` 负责说明总体架构与产品方向；
`docs/design` 负责把各个模块拆成可落地的设计稿，便于后续继续补充：

- API 设计
- 数据模型
- 时序图
- 状态机
- 部署拓扑
- V1 边界

***

## 文档列表

### 1. [01-control-plane.md](./01-control-plane.md)

Control Plane 总体详细设计。

覆盖：

```text
API Server
Controller Manager
Dashboard
模块边界
控制面数据流
部署形态
```

### 2. [02-node-onboarding-security.md](./02-node-onboarding-security.md)

节点接入与安全体系设计。

覆盖：

```text
Bootstrap Token
Node Registration
Certificate Issue
EdgeIdentity
Renew / Revoke
Upgrade Security
```

### 3. [03-gateway.md](./03-gateway.md)

Gateway 与 `gateway-runtime` 模块设计。

覆盖：

```text
区域接入
任务分发
状态聚合
缓存管理
断网自治
DaemonSet 部署模型
```

### 4. [04-edge-agent.md](./04-edge-agent.md)

Edge Agent 详细设计。

覆盖：

```text
bootstrap
identity
heartbeat
task-runner
runtime-manager
updater
```

### 5. [05-task-engine.md](./05-task-engine.md)

任务系统详细设计。

覆盖：

```text
Task 对象
状态机
重试
幂等
审计
Gateway 协同
```

### 6. [06-model-registry-runtime.md](./06-model-registry-runtime.md)

模型仓库与 Runtime 抽象设计。

覆盖：

```text
Artifact 管理
版本策略
签名与校验
RuntimeProfile
Runtime Mapping
缓存层级
```

### 7. [07-api-spec.md](./07-api-spec.md)

系统 API 规格设计。

覆盖：

```text
proto services
gRPC services
生成式 API 绑定
错误码约定
鉴权约定
幂等约定
```

### 8. [08-resource-model.md](./08-resource-model.md)

资源对象模型设计。

覆盖：

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

### 9. [09-sequence-flows.md](./09-sequence-flows.md)

关键时序流程设计。

覆盖：

```text
Node Onboarding
Model Deployment
Certificate Renew
Node Revoke
Agent Upgrade
Gateway 自治
```

### 10. [10-crd-schema.md](./10-crd-schema.md)

CRD Schema 草案设计。

覆盖：

```text
Gateway CRD
EdgeNode CRD
EdgeIdentity CRD
BootstrapToken CRD
ModelDeployment CRD
Task CRD
字段约束
```

### 11. [11-database-schema.md](./11-database-schema.md)

数据库表结构设计。

覆盖：

```text
核心表设计
唯一约束
索引建议
事务边界
状态与时序分离
```

### 12. [12-error-codes.md](./12-error-codes.md)

统一错误码设计。

覆盖：

```text
错误响应结构
领域错误分类
Agent 重试建议
升级错误
前端展示建议
```

### 13. [13-proto-grpc-guidelines.md](./13-proto-grpc-guidelines.md)

proto / gRPC 约束设计。

覆盖：

```text
proto package 规划
service 拆分
消息命名规范
生成代码策略
grpc-gateway 边界
版本演进策略
```

***

## 阅读顺序建议

推荐顺序：

```text
总体架构
↓
Node Onboarding & Security
↓
Gateway
↓
Edge Agent
↓
Task Engine
↓
Model Registry / Runtime
↓
API Spec
↓
Resource Model
↓
Sequence Flows
↓
CRD Schema
↓
Database Schema
↓
Error Codes
↓
Proto / gRPC Guidelines
```

这样更容易理解：

```text
节点如何安全接入
Gateway 如何承载区域控制
Agent 如何执行
任务如何流转
模型如何分发与运行
接口如何定义
资源如何建模
关键流程如何串联
对象如何落到字段
数据如何落库存储
错误如何统一表达
```

***

## 设计原则

所有详细设计继续遵守以下原则：

```text
轻量级优先
边缘自治优先
Task Driven
安全默认开启
兼容后续演进
```

V1 设计默认：

```text
不把 Edge 设备做成 Kubernetes Node
不引入过重的 Service Mesh / PKI / 调度系统
先保证核心链路成立
```

***

## 后续补充项

后续会继续补充：

- API 详细字段定义
- CRD 草案
- 关键时序图
- 本地目录结构
- 数据库表结构
- 失败处理与异常流程
- Mermaid 图稿
- proto schema 草案
- SQL migration 草案
- Admission 校验规则

## 开工判断

到当前这版为止，文档已经满足第一批代码开工基线：

```text
主数据库边界明确
Gateway / Edge 本地状态边界明确
proto + gRPC 契约明确
任务流转明确
身份模型与唯一约束明确
第一批 migration 顺序明确
```

后续补文档的原则是：

```text
只补实现时真正遇到的空白
不再以前置文档为理由阻塞编码
```
