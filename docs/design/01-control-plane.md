# Control Plane 详细设计

## 1. 设计目标

Control Plane 是整个平台的中心控制层。

它的目标不是执行边缘 Runtime，而是负责：

```text
统一资源建模
统一策略下发
统一任务编排
统一可观测与审计入口
```

在本项目中，Kubernetes 只承担控制面的宿主环境与资源管理能力，不承担边缘节点调度语义。

***

## 2. 范围

V1 中 Control Plane 包含：

```text
API Server
Controller Manager
Task Engine
Model Registry
Dashboard
Node Onboarding Service
```

不包含：

```text
边缘节点 Runtime 执行
边缘网络 overlay
容器级调度
Kubernetes Node 生命周期管理
```

***

## 3. 核心职责

### 3.1 资源入口

统一接收和管理：

```text
Gateway
EdgeNode
Model
RuntimeProfile
ModelDeployment
BootstrapToken
EdgeIdentity
Task
```

### 3.2 控制逻辑

根据用户声明和系统状态生成：

```text
DeploymentTask
UpgradeTask
RollbackTask
RevokeTask
```

### 3.3 管理视图

向用户提供：

```text
配置入口
部署入口
状态视图
任务历史
审计入口
```

***

## 4. 逻辑组件

## 4.1 API Server

职责：

```text
资源 CRUD
查询聚合
管理鉴权
外部入口
```

典型服务：

```text
ModelService
DeploymentService
GatewayService
NodeService
TaskService
NodeOnboardingService
```

设计要求：

- API 语义保持资源化
- 长耗时操作不在请求内直接完成
- 所有执行型操作都转换为 Task
- `proto` 是唯一契约源
- 服务间通信统一使用 `gRPC`
- 如需 HTTP/JSON，仅允许由同一套 `proto` 自动生成绑定

## 4.2 Controller Manager

职责：

```text
监听资源变化
生成任务
维护期望状态
执行回收逻辑
```

核心控制循环：

```text
ModelDeployment 变化
  ↓
选择目标 Gateway / Edge
  ↓
生成 DeploymentTask
  ↓
交给 Task Engine 编排
```

V1 不做复杂调度器，只做基于：

```text
Gateway 归属
标签选择
RuntimeProfile 匹配
```

的轻量级目标选择。

## 4.3 Task Engine

职责：

```text
任务创建
任务状态流转
重试与超时
审计记录
Gateway 分发协调
```

`Task Engine` 是系统核心，但详细状态机和执行语义放在单独文档中说明。

## 4.4 Model Registry

职责：

```text
模型元数据管理
Artifact 存储
版本管理
校验信息管理
下载授权
```

V1 推荐基于：

```text
对象存储 + 元数据表
```

实现，不自建复杂制品仓库。

## 4.5 Dashboard

职责：

```text
资源可视化
任务操作
状态查看
异常定位
```

V1 以管理端为主，不做复杂多租户门户能力。

## 4.6 Node Onboarding Service

职责：

```text
Bootstrap Token 校验
首次注册
证书签发（内置 Signer）
身份续签
吊销管理
```

关键归属约定（V1 固定）：

- Bootstrap Token 的校验与 `used_count` 更新只发生在 Control Plane，针对云端主库
- 证书签发由 Control Plane 内置 `Signer` 完成
- 每区域一个 Intermediate CA，**所有 Intermediate CA 私钥只保存在 Control Plane**，不下发到 `gateway-runtime`
- `Gateway` 只是 Agent `Bootstrap` / `Renew` 请求的接入代理，本身不读写 Token / Identity 主表，也不持有 CA 私钥
- 因此新节点接入与证书签发依赖 Control Plane 在线；区域断网期间不可新接入（详见自治边界）

该模块详细设计单独见 `02-node-onboarding-security.md`。

***

## 5. 关键数据流

## 5.0 协议约束

V1 统一协议原则：

```text
所有 API 先定义 proto
内部服务间统一 gRPC
Agent <-> Gateway 统一 gRPC + mTLS
管理面如需 HTTP/JSON，使用 proto 生成网关层
```

### 5.1 部署数据流

```text
User
  ↓
API Server
  ↓
Controller Manager
  ↓
Task Engine
  ↓
Gateway
  ↓
Edge Agent
```

### 5.2 节点接入数据流

```text
Admin 创建 BootstrapToken
  ↓
Agent 启动 bootstrap
  ↓
Gateway 接入并转发 Bootstrap 请求
  ↓
Control Plane 校验 Token 并由 Signer 签发证书
  ↓
EdgeIdentity 创建（与 used_count+1 同事务）
  ↓
后续走 mTLS（Gateway 终止 + 逐请求身份校验）
```

### 5.3 状态回流

```text
Edge
  ↓
Gateway 聚合
  ↓
Control Plane
  ↓
Dashboard / API 查询
```

***

## 6. 部署设计

推荐部署形态：

```text
API Server            Deployment
Controller Manager    Deployment
Task Engine           Deployment
Model Registry        StatefulSet / 外部服务
Dashboard             Deployment
```

说明：

- `Task Engine` 可先与 `API Server` 共享数据库
- `Model Registry` 可直接复用外部 `MinIO`
- `Node Onboarding Service` 在 V1 可作为 `API Server` 的内部模块，不必独立部署

这样可以减少服务数量，保持轻量级。

***

## 7. 存储设计

V1 建议最少拆分为三类存储：

### 7.1 元数据存储

用于存放：

```text
资源对象
任务记录
身份信息
Token 信息
缓存元数据
```

推荐：

```text
PostgreSQL
```

这部分是系统的：

```text
云端主元数据数据库
```

用于承载全局真相源。

也就是说，V1 默认不是每个 `Gateway` 都各自维护一套完整中心数据库，而是：

- `Control Plane` 维护全局资源、任务、身份、令牌与缓存元数据
- `Gateway` 维护本地运行缓存与自治所需的轻量状态
- `Edge` 维护本机运行缓存与恢复现场

### 7.2 时序指标存储

用于存放：

```text
CPU
Memory
GPU
QPS
Latency
```

推荐：

```text
Prometheus
```

### 7.3 Artifact 存储

用于存放：

```text
模型文件
Agent 包
校验文件
签名文件
```

推荐：

```text
MinIO
```

说明：

- 云端对象存储保存模型主仓、Agent 包、签名与校验文件
- `Gateway` 与 `Edge` 本地只保存下载后的缓存副本
- 本地缓存文件不作为全局真相源

### 7.4 边缘侧本地存储边界

V1 需要明确区分：

```text
中心主库存储
Gateway 本地状态存储
Edge 本地状态存储
```

边缘侧本地存储的目标不是替代云端数据库，而是支持：

```text
缓存命中
断网自治
重启恢复
增量补传
```

推荐原则：

- `Gateway` 不部署独立大型数据库服务
- `Gateway` 使用本地磁盘 + 嵌入式轻量状态库
- `Edge Agent` 使用本地磁盘 + 本地状态文件或轻量状态库

这样可以在保持轻量级的前提下，支撑自治与恢复。

***

## 8. V1 边界

V1 Control Plane 明确不做：

- 复杂调度器
- 多活跨地域控制面
- 完整工作流编排引擎
- 细粒度 RBAC 体系
- 自建可插拔 PKI 平台

V1 优先保证：

```text
安全接入
任务可流转
模型可分发
状态可观测
升级可控
```

***

## 9. 模块边界原则

为了避免职责膨胀，模块边界固定如下：

- `API Server` 负责入口，不负责执行
- `Controller Manager` 负责生成任务，不直接下发到节点
- `Task Engine` 负责任务生命周期，不负责模型下载
- `Gateway` 负责区域执行协调，不负责全局资源建模
- `Edge Agent` 负责本机执行，不负责跨节点调度

***

## 10. 后续细化项

需要继续补充：

- API 字段设计
- 鉴权模型
- 资源对象定义
- 数据库表结构
- Dashboard 信息架构
