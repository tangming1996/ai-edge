# Task Engine 详细设计

## 1. 设计目标

`Task Engine` 是系统核心执行编排层。

平台中的所有执行类操作都应任务化，而不是通过同步 API 直接操作边缘节点。

任务化的目标是：

```text
统一执行语义
统一状态跟踪
统一审计
统一失败处理
```

***

## 2. 设计范围

Task Engine 负责：

```text
任务创建
任务状态机
重试与超时
幂等控制
任务历史
Gateway 分发协同
```

不负责：

```text
模型真实下载
本机 Runtime 执行
系统镜像构建
复杂工作流 DSL
```

***

## 3. 任务模型

V1 至少包含两层任务（父子关系）：

```text
DeploymentTask (parent, scope=Region)
  └── NodeTask (child, scope=Node)
```

其中：

- `DeploymentTask` 是控制面视角的区域级任务（父任务）
- `NodeTask` 是面向单节点的执行任务（子任务），通过 `parent_task_id` 关联到 `DeploymentTask`

这样做的原因：

- Control Plane 只关心全局意图
- Gateway 负责区域内拆分与投递
- Agent 只关心自身执行任务

落库说明：父子任务都存在云端 `tasks` 表，靠 `parent_task_id` 关联；父任务的汇总状态（成功/失败/部分成功）由子任务结果聚合得到（见 `11-database-schema.md`）。

### 3.1 目标节点展开与计数归属

- `Control Plane` 依据 `target`（gateway + labelSelector）从已知 `EdgeNode` 计算 `desiredNodes`，写入 `ModelDeployment.status` 与父 `DeploymentTask`
- `Gateway` 把父任务展开为各 `NodeTask`（仅针对本区域可投递节点），并回传实际展开数与结果计数
- 两者以云端主库为准对账：`desiredNodes` 由控制面定义，`readyNodes / failedNodes` 由子任务结果聚合

### 3.2 NodeTask claim（多实例去重）

`gateway-runtime` 是无状态实例，多实例不重复分发靠云端主库原子 claim：

```text
gateway-runtime 在云端 tasks 表对 NodeTask 原子 claim
claim 字段：owner_instance / claim_expire_at / dispatch_status
claim 成功才向节点投递；claim 超时其他实例可接管
结果回传按 task_id + node_id 幂等归并
```

### 3.3 Runtime 解析归属

`runtime: auto` 的解析（依据 `RuntimeProfile`）在生成 `NodeTask` 时完成，把解析后的具体 runtime 与参数写入 `NodeTask.payload`，Edge 不反查 `RuntimeProfile`。

***

## 4. 任务类型

V1 建议支持：

```text
InstallModel
DeleteModel
StartRuntime
StopRuntime
RestartRuntime
UpgradeRuntime
UpgradeAgent
CollectLogs
RevokeNode
```

后续可扩展：

```text
RollbackRuntime
WarmupModel
DrainNode
RefreshIdentity
```

***

## 5. 任务对象结构

建议结构：

```yaml
taskID: xxx
type: InstallModel
scope: Node
target:
  gateway: gw-shanghai
  node: edge-001
spec:
  model: qwen3-8b
  version: v1
status: Pending
retryPolicy:
  maxRetries: 3
timeoutSeconds: 1800
createdAt: ...
```

关键字段说明：

- `type`: 任务类型
- `scope`: 区域级或节点级
- `target`: 目标 Gateway / Edge
- `spec`: 任务具体参数
- `status`: 当前状态
- `retryPolicy`: 重试策略
- `timeoutSeconds`: 超时时间

***

## 6. 状态机设计

基本状态机：

```text
Pending
  ↓
Dispatching
  ↓
Running
  ↓
Success
```

失败路径：

```text
Running
  ↓
Failed
  ↓
Retrying
  ↓
Running
```

补充终态建议：

```text
Cancelled
Timeout
PartiallySucceeded
```

状态定义：

- `Pending`: 已创建，未分发
- `Dispatching`: 正在向 Gateway 或节点投递
- `Running`: 节点已接收并执行
- `Success`: 成功完成
- `Failed`: 最终失败
- `Retrying`: 进入重试窗口
- `Timeout`: 超时未完成

***

## 7. 幂等设计

任务系统必须支持幂等。

原因：

- 网络抖动会导致重复投递
- Gateway 与 Agent 都可能重试
- 任务状态更新存在乱序风险

V1 约束：

- 每个任务必须有稳定 `taskID`
- Agent 本地以 `taskID` 做去重
- 重复投递同一任务时，返回已有执行状态

典型幂等示例：

```text
InstallModel(qwen3-8b:v1)
```

如果模型已经完成安装：

```text
返回 Success
而不是重新下载
```

***

## 8. 重试设计

重试必须是显式策略，而不是无限循环。

建议策略：

```text
maxRetries = 3
退避 = 指数退避
仅对可恢复错误重试
```

可恢复错误：

```text
网络失败
下载临时失败
网关短暂不可达
```

不可恢复错误：

```text
参数错误
签名校验失败
目标 Runtime 不支持
身份被吊销
```

***

## 9. 超时与取消

每类任务都应该有默认超时。

建议示例：

```text
InstallModel     30m
StartRuntime     10m
UpgradeAgent     20m
CollectLogs      5m
```

超时后：

```text
标记 Timeout
记录最后执行位置
决定是否进入重试
```

V1 允许管理端主动取消任务，但取消语义只保证：

```text
停止后续投递
尽力中断尚未开始的执行
```

不保证一定能中止已执行到系统级操作中的本机流程。

***

## 10. 任务流转

## 10.1 创建

来源包括：

```text
用户发起部署
控制器检测变更
管理员手动操作
系统内部修复动作
```

## 10.2 下发

```text
Controller Manager
  ↓
Task Engine 创建 DeploymentTask
  ↓
绑定 Gateway
  ↓
Gateway 拆分为 NodeTask
```

## 10.3 执行

```text
Edge Agent 调用 AgentService/PullTasks
  ↓
接收 NodeTask
  ↓
task-runner 执行
  ↓
回写执行结果
```

## 10.4 回收

```text
Gateway 汇总节点结果
  ↓
Task Engine 聚合成区域任务状态
  ↓
Dashboard / API 提供查看
```

***

## 11. 审计设计

任务系统必须具备审计能力。

至少记录：

```text
谁创建了任务
何时创建
目标是谁
做了什么
执行结果
失败原因
重试次数
```

建议拆分：

```text
task_current_state
task_history
task_event_log
```

这样方便同时支持：

```text
当前状态查询
历史回放
问题排查
```

***

## 12. Gateway 协同

Task Engine 不直接面向每个 Edge 下发任务，而是通过 Gateway 协同。

原则：

- Control Plane 负责全局意图
- Gateway 负责区域拆分
- Agent 负责本机执行

Gateway 需要反馈：

```text
已分发
节点执行中
节点成功数
节点失败数
失败明细
```

这样 Task Engine 才能生成：

```text
Success
Failed
PartiallySucceeded
```

***

## 13. 数据存储设计

推荐存储内容：

```sql
tasks
task_runs
task_events
```

`tasks` 存放（父子任务同表，靠 `parent_task_id` 关联）：

```text
任务定义
当前状态
目标
策略
parent_task_id（NodeTask 指向 DeploymentTask）
claim 字段：owner_instance / claim_expire_at / dispatch_status
```

`task_runs` 存放：

```text
每次实际执行记录
开始时间
结束时间
退出结果
```

`task_events` 存放：

```text
状态变化事件
错误信息
重试事件
人工操作事件
```

***

## 14. V1 非目标

V1 暂不实现：

- 图形化工作流引擎
- 跨任务依赖图编排
- 分布式事务
- 复杂优先级抢占调度
- 大规模实时 push 执行链路

***

## 15. 后续细化项

需要继续补充：

- 每类 Task 的 `spec` 结构
- 错误码分类
- Agent 侧幂等落盘格式
- Task 与 Deployment 的映射关系
- Gateway 汇总算法
