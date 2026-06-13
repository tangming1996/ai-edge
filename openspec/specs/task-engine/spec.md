# task-engine Specification

## Purpose
TBD - created by archiving change bootstrap-v1-platform. Update Purpose after archive.
## Requirements
### Requirement: 一切执行操作任务化
平台所有执行类操作 SHALL 任务化，而非通过同步 API 直接操作边缘节点。V1 任务类型至少包括 `InstallModel`、`DeleteModel`、`StartRuntime`、`StopRuntime`、`RestartRuntime`、`UpgradeRuntime`、`UpgradeAgent`、`CollectLogs`、`RevokeNode`。

#### Scenario: 长耗时操作转为任务
- **WHEN** 用户请求安装模型这类长耗时操作
- **THEN** API 立即创建对应 Task 并返回 task 引用，而不在请求内同步完成执行

### Requirement: 父子任务模型
Task Engine SHALL 支持两层父子任务：`DeploymentTask`（scope=Region，父）与 `NodeTask`（scope=Node，子，通过 `parent_task_id` 关联），父子同存于 `tasks` 表。父任务汇总状态 SHALL 由子任务结果聚合得到。

#### Scenario: 父任务聚合子任务结果
- **WHEN** 一个 DeploymentTask 的全部 NodeTask 完成，部分成功部分失败
- **THEN** 父任务状态聚合为 `PartiallySucceeded`，并可查询成功数/失败数/失败明细

### Requirement: 任务状态机
任务 SHALL 遵循状态机：`Pending → Dispatching → Running → Success`，失败路径 `Running → Failed → Retrying → Running`，并支持终态 `Cancelled`、`Timeout`、`PartiallySucceeded`。状态流转 SHALL 记录到 `task_events`。

#### Scenario: 失败进入重试窗口
- **WHEN** 一个 Running 的任务因可恢复错误失败且未超过最大重试
- **THEN** 任务进入 `Retrying` 并按退避策略重新执行

### Requirement: 幂等
任务系统 SHALL 支持幂等：每个任务有稳定 `taskID`，Agent 本地按 `taskID` 去重，重复投递返回已有执行状态。结果回传 SHALL 按 `task_id + node_id` 幂等归并。

#### Scenario: 重复投递已完成任务
- **WHEN** `InstallModel(qwen3-8b:v1)` 已完成，再次收到同一 taskID 投递
- **THEN** 返回 `Success`，不重新下载或重复执行

### Requirement: 重试与超时
重试 SHALL 是显式策略（建议 `maxRetries=3` + 指数退避），且仅对可恢复错误（网络失败、下载临时失败、网关短暂不可达）重试；不可恢复错误（参数错误、签名校验失败、Runtime 不支持、身份被吊销）MUST NOT 重试。每类任务 SHALL 有默认超时，超时标记 `Timeout` 并记录最后执行位置。

#### Scenario: 签名校验失败不重试
- **WHEN** 任务因制品签名校验失败而失败
- **THEN** 任务直接进入 `Failed` 终态，不进入 `Retrying`

### Requirement: NodeTask 云端原子 claim
`gateway-runtime` 为无状态多实例，NodeTask 的去重分发 SHALL 通过对云端 `tasks` 表的原子 claim 实现（字段 `owner_instance`/`claim_expire_at`/`dispatch_status`）。只有原子更新影响行数为 1 的实例获得投递权；claim 超时后其他实例 MAY 接管。claim 真相源 MUST NOT 放在 Gateway 本地。

#### Scenario: 多实例不重复分发
- **WHEN** 两个 gateway-runtime 实例同时尝试 claim 同一 NodeTask
- **THEN** 仅一个实例的原子更新成功并投递，另一个放弃

#### Scenario: 过期 claim 被接管
- **WHEN** 持有 claim 的实例宕机且 `claim_expire_at` 已过
- **THEN** 另一实例可重新 claim 并接管投递，结果按 `task_id + node_id` 幂等归并

### Requirement: 任务审计与历史
Task Engine SHALL 通过 `tasks`（当前状态）、`task_runs`（每次执行记录）、`task_events`（状态变化与人工操作事件）记录审计信息，至少包含创建者、创建时间、目标、操作、结果、失败原因与重试次数。

#### Scenario: 任务历史可回放
- **WHEN** 排障需要查看某任务的完整经过
- **THEN** 可通过 task_events 按时间顺序回放状态变化与错误信息

### Requirement: 取消语义
V1 SHALL 允许管理端主动取消任务，取消只保证停止后续投递与尽力中断尚未开始的执行，不保证中止已进入系统级操作的本机流程。

#### Scenario: 取消尚未投递的任务
- **WHEN** 管理员取消一个仍为 `Pending` 的任务
- **THEN** 该任务不再被投递，状态转为 `Cancelled`

