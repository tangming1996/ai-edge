# Database Schema 设计

## 1. 设计目标

本文件定义平台 V1 的数据库表结构设计方向。

目标：

```text
明确核心表
明确唯一约束
明确对象与状态分离策略
支撑 API / Controller / Gateway / Audit 实现
```

V1 强调：

- 表结构稳定
- 查询路径清晰
- 不过度范式化
- 支持后续扩展

***

## 2. 存储分类

V1 建议至少区分三类存储：

```text
元数据数据库
时序指标存储
对象存储
```

其中本文件只描述：

```text
元数据数据库
```

推荐：

```text
PostgreSQL
```

这里指的是：

```text
云端 Control Plane 主元数据库
```

它保存全局真相源。

V1 不假设：

```text
每个 Gateway 都部署一套完整中心数据库
```

相反，边缘侧采用：

```text
本地文件缓存
+
轻量状态存储
```

***

## 2.1 云端与边缘侧存储边界

V1 明确拆分为三层：

### 云端主库

负责：

```text
全局资源对象
任务全局状态
EdgeIdentity
BootstrapToken
缓存元数据
审计记录
```

### Gateway 本地状态

负责：

```text
区域缓存索引
待下发任务快照
待回传结果
同步游标
吊销列表快照
```

### Edge 本地状态

负责：

```text
模型缓存
任务工作目录
待补传状态
本机 Runtime 恢复现场
```

这三层的关系是：

```text
云端主库 = 全局真相源
Gateway 本地状态 = 自治运行缓存
Edge 本地状态 = 本机执行恢复现场
```

***

## 3. 表清单

建议核心表：

```text
gateways
edge_nodes
edge_identities
bootstrap_tokens
models
runtime_profiles
model_deployments
tasks
task_runs
task_events
gateway_runtime_instances
gateway_cache_entries
edge_runtime_states
```

说明：

- 高频 metrics 不落本库
- 大文件不落本库
- 审计与执行记录拆表

***

## 4. gateways

用途：

```text
存储逻辑 Gateway 定义
```

建议字段：

```sql
id                 uuid pk
name               varchar(128) unique not null
region             varchar(128) not null
endpoint           text not null
labels_json        jsonb not null default '{}'
cache_policy_json  jsonb not null default '{}'
autonomy_json      jsonb not null default '{}'
status             varchar(32) not null
runtime_instances  integer not null default 0
connected_nodes    integer not null default 0
last_sync_at       timestamptz null
created_at         timestamptz not null
updated_at         timestamptz not null
```

索引建议：

```text
unique(name)
index(region)
index(status)
```

***

## 5. edge_nodes

用途：

```text
存储节点资产信息与摘要状态
```

建议字段：

```sql
id                    uuid pk
name                  varchar(128) unique not null
gateway_id            uuid not null references gateways(id)
hostname              varchar(255) not null
labels_json           jsonb not null default '{}'
hardware_json         jsonb not null
runtime_caps_json     jsonb not null default '[]'
status                varchar(32) not null
agent_version         varchar(64) null
last_seen_at          timestamptz null
runtime_summary_json  jsonb not null default '{}'
created_at            timestamptz not null
updated_at            timestamptz not null
```

索引建议：

```text
unique(name)
index(gateway_id)
index(status)
index(last_seen_at)
gin(labels_json)
```

***

## 6. edge_identities

用途：

```text
存储节点证书身份与生命周期
```

建议字段：

```sql
id                 uuid pk
node_id            varchar(128) unique not null
edge_node_id       uuid not null references edge_nodes(id)
gateway_id         uuid not null references gateways(id)
serial             varchar(255) not null
fingerprint        varchar(255) unique not null
subject            text null
issuer             text null
status             varchar(32) not null
issued_at          timestamptz not null
expire_at          timestamptz not null
revoked_at         timestamptz null
last_seen_at       timestamptz null
created_at         timestamptz not null
updated_at         timestamptz not null
```

关键约束：

```text
unique(node_id)
unique(fingerprint)
active identity 下 serial 唯一
```

实现建议：

- 用部分唯一索引保证 `serial` 在 `Active/Suspended` 范围内唯一

索引建议：

```text
index(edge_node_id)
index(gateway_id)
index(status)
index(expire_at)
index(last_seen_at)
```

***

## 7. bootstrap_tokens

用途：

```text
存储首次接入令牌
```

建议字段：

```sql
id             uuid pk
name           varchar(128) not null
token_hash     varchar(255) unique not null
gateway_id     uuid not null references gateways(id)
labels_json    jsonb not null default '{}'
max_uses       integer not null
used_count     integer not null default 0
expire_at      timestamptz not null
status         varchar(32) not null
created_by     varchar(128) null
created_at     timestamptz not null
updated_at     timestamptz not null
```

关键约束：

- 不存明文 token
- `used_count <= max_uses`

并发要求：

- `used_count` 更新必须与注册成功放入同一事务

索引建议：

```text
unique(token_hash)
index(gateway_id)
index(status)
index(expire_at)
```

***

## 8. models

用途：

```text
存储模型元数据
```

建议字段：

```sql
id               uuid pk
name             varchar(128) not null
version          varchar(128) not null
format           varchar(64) not null
checksum         varchar(255) not null
size_bytes       bigint not null
artifact_uri     text not null
signature_uri    text null
framework        varchar(128) null
precision        varchar(64) null
labels_json      jsonb not null default '{}'
status           varchar(32) not null
published_at     timestamptz null
created_at       timestamptz not null
updated_at       timestamptz not null
```

关键约束：

```text
unique(name, version)
checksum 不可变
```

索引建议：

```text
unique(name, version)
index(status)
gin(labels_json)
```

***

## 9. runtime_profiles

用途：

```text
存储 Runtime 选择规则
```

建议字段：

```sql
id                  uuid pk
name                varchar(128) unique not null
selector_json       jsonb not null
runtime             varchar(64) not null
priority            integer not null default 0
runtime_config_json jsonb not null default '{}'
status              varchar(32) not null
created_at          timestamptz not null
updated_at          timestamptz not null
```

索引建议：

```text
unique(name)
index(runtime)
index(priority desc)
index(status)
```

***

## 10. model_deployments

用途：

```text
存储用户声明的部署意图
```

建议字段：

```sql
id                 uuid pk
name               varchar(128) unique not null
model_id           uuid not null references models(id)
runtime            varchar(64) not null
target_json        jsonb not null
rollout_json       jsonb not null default '{}'
policy_json        jsonb not null default '{}'
status             varchar(32) not null
desired_nodes      integer not null default 0
ready_nodes        integer not null default 0
failed_nodes       integer not null default 0
task_id            uuid null
created_by         varchar(128) null
created_at         timestamptz not null
updated_at         timestamptz not null
```

索引建议：

```text
unique(name)
index(model_id)
index(status)
```

***

## 11. tasks

用途：

```text
存储任务当前状态
```

建议字段：

```sql
id                uuid pk
task_key          varchar(128) unique not null
parent_task_id    uuid null references tasks(id)   -- NodeTask 指向 DeploymentTask
type              varchar(64) not null
scope             varchar(32) not null              -- Region / Node
target_gateway_id uuid null references gateways(id)
target_node_id    uuid null references edge_nodes(id)
payload_json      jsonb not null
retry_policy_json jsonb not null default '{}'
timeout_seconds   integer not null
status            varchar(32) not null
retry_count       integer not null default 0
-- 以下 claim 字段仅 NodeTask 使用，是多 gateway-runtime 实例去重的真相源
owner_instance    varchar(128) null
claim_expire_at   timestamptz null
dispatch_status   varchar(32) null                  -- Unclaimed / Claimed / Dispatched / Done
created_by        varchar(128) null
started_at        timestamptz null
finished_at       timestamptz null
message           text null
created_at        timestamptz not null
updated_at        timestamptz not null
```

关键约束：

```text
unique(task_key)
parent_task_id 自引用（父任务为 NULL）
```

claim 真相源说明：`gateway-runtime` 是无状态实例，NodeTask 的 claim 通过对**本表**原子更新完成（不在 Gateway 本地库），保证同一节点任务不会被多个实例重复分发。

索引建议：

```text
index(parent_task_id)
index(type)
index(scope)
index(status)
index(target_gateway_id)
index(target_node_id)
index(created_at)
partial index(target_gateway_id, dispatch_status) where scope = 'Node'
```

***

## 12. task_runs

用途：

```text
存储任务每次实际执行记录
```

建议字段：

```sql
id              uuid pk
task_id         uuid not null references tasks(id)
executor_type   varchar(32) not null
executor_ref    varchar(128) not null
attempt         integer not null
status          varchar(32) not null
started_at      timestamptz null
finished_at     timestamptz null
exit_code       integer null
message         text null
detail_json     jsonb not null default '{}'
created_at      timestamptz not null
updated_at      timestamptz not null
```

索引建议：

```text
index(task_id)
index(executor_type, executor_ref)
index(status)
index(started_at)
```

***

## 13. task_events

用途：

```text
存储任务状态变化与审计事件
```

建议字段：

```sql
id              uuid pk
task_id         uuid not null references tasks(id)
event_type      varchar(64) not null
from_status     varchar(32) null
to_status       varchar(32) null
message         text null
event_json      jsonb not null default '{}'
created_by      varchar(128) null
created_at      timestamptz not null
```

索引建议：

```text
index(task_id)
index(event_type)
index(created_at)
```

***

## 14. gateway_runtime_instances

用途：

```text
存储 gateway-runtime 实例状态
```

建议字段：

```sql
id                 uuid pk
gateway_id         uuid not null references gateways(id)
instance_name      varchar(128) unique not null
node_name          varchar(128) null
pod_name           varchar(128) null
status             varchar(32) not null
last_heartbeat_at  timestamptz null
created_at         timestamptz not null
updated_at         timestamptz not null
```

索引建议：

```text
index(gateway_id)
index(status)
index(last_heartbeat_at)
```

***

## 14.1 Gateway 本地状态存储建议

虽然 V1 不建议在 Gateway 上部署独立大型数据库服务，但自治能力仍然需要本地状态落点。

推荐：

```text
SQLite
```

或者：

```text
bbolt
```

用于承载**可重建、可丢失**的缓存与现场（不承载 claim 真相源）：

```text
pending upload buffer（失联期待补传结果）
revoked / identity 状态缓存（云端推送刷新，短 TTL）
sync watermark
cache index（可重新扫描重建）
```

说明：

- 这是 `Gateway` 的本地缓存与自治现场，不是全局主库
- 任务 claim 真相源在云端 `tasks` 表，**不放本地**
- 本地状态整体丢失也不影响区域正确性（最多重拉/重扫）
- 数据恢复后仍以云端主库为最终准

***

## 15. gateway_cache_entries

用途：

```text
存储区域缓存元数据
```

建议字段：

```sql
id               uuid pk
gateway_id       uuid not null references gateways(id)
model_id         uuid not null references models(id)
version          varchar(128) not null
checksum         varchar(255) not null
size_bytes       bigint not null
cache_path       text not null
status           varchar(32) not null
last_access_at   timestamptz null
ref_count        integer not null default 0
created_at       timestamptz not null
updated_at       timestamptz not null
```

关键约束：

```text
unique(gateway_id, model_id, version)
```

索引建议：

```text
index(gateway_id)
index(last_access_at)
index(status)
```

***

## 16. edge_runtime_states

用途：

```text
存储节点当前 Runtime 摘要状态
```

建议字段：

```sql
id                 uuid pk
edge_node_id       uuid not null references edge_nodes(id)
runtime            varchar(64) not null
loaded_models_json jsonb not null default '[]'
state_json         jsonb not null default '{}'
updated_at         timestamptz not null
```

关键约束：

```text
unique(edge_node_id, runtime)
```

说明：

- 这里只保留当前快照
- 历史时序不在此表

***

## 16.1 Edge 本地状态存储建议

`Edge` 侧默认不运行数据库服务。

V1 推荐：

```text
文件目录
+
状态文件
```

必要时可以增加：

```text
SQLite
```

用于增强以下能力：

```text
任务恢复
待补传队列
本机运行态快照
升级中断恢复
```

但这仍然属于本机恢复现场，而不是中心数据库延伸。

***

## 17. 事务边界建议

V1 至少保证以下事务一致性：

### 17.1 Bootstrap 注册

同一事务内完成：

```text
校验 token
used_count + 1
创建 / 更新 EdgeIdentity
创建 / 更新 EdgeNode
```

### 17.2 吊销

同一事务内完成：

```text
更新 EdgeIdentity.status
写入 task / audit event
```

### 17.3 Deployment 创建

同一事务内完成：

```text
写入 model_deployments
写入 tasks
写入 task_events
```

***

## 18. 状态与时序分离建议

不要把高频数据写入主对象表：

- `metrics` 进入时序系统
- `task_events` 单独记录
- `runtime` 历史轨迹单独考虑，不放 `edge_nodes`

这样可以避免：

```text
主表膨胀
更新热点
查询变慢
```

***

## 19. V1 非目标

V1 暂不设计：

- 分库分表
- 多租户隔离库
- 冷热数据自动迁移
- 审计日志外部归档平台

***

## 20. 后续细化项

需要继续补充：

- SQL migration 草案
- 关键索引 DDL
- 部分唯一索引示例
- 审计与保留周期策略

***

## 21. 关键索引 DDL 示例

以下示例用于说明 V1 中最关键的唯一约束与查询索引应该如何落地。

### 21.1 EdgeIdentity 指纹唯一

```sql
create unique index ux_edge_identities_fingerprint
on edge_identities (fingerprint);
```

目的：

```text
保证同一证书指纹不能重复注册
```

### 21.2 EdgeIdentity node_id 唯一

```sql
create unique index ux_edge_identities_node_id
on edge_identities (node_id);
```

目的：

```text
保证 nodeID 是全局唯一身份
```

### 21.3 Active / Suspended 身份下 serial 部分唯一

```sql
create unique index ux_edge_identities_serial_active
on edge_identities (serial)
where status in ('Active', 'Suspended');
```

目的：

```text
防止同一设备主标识被多个有效身份复用
```

这也是 V1 防止：

```text
镜像克隆
重复抢注
身份漂移
```

的关键约束。

### 21.4 BootstrapToken 哈希唯一

```sql
create unique index ux_bootstrap_tokens_token_hash
on bootstrap_tokens (token_hash);
```

目的：

```text
避免接入令牌重复
```

### 21.5 tasks 当前待执行查询索引

```sql
create index idx_tasks_gateway_status_created
on tasks (target_gateway_id, status, created_at desc);
```

目的：

```text
加速 Gateway 侧待分发任务查询
```

### 21.6 tasks 节点维度查询索引

```sql
create index idx_tasks_node_status_created
on tasks (target_node_id, status, created_at desc);
```

目的：

```text
加速节点执行状态与排障查询
```

### 21.7 task_events 时间索引

```sql
create index idx_task_events_task_created
on task_events (task_id, created_at desc);
```

目的：

```text
加速任务历史回放
```

### 21.8 gateway_cache_entries 最近访问索引

```sql
create index idx_gateway_cache_entries_gateway_access
on gateway_cache_entries (gateway_id, last_access_at asc);
```

目的：

```text
支持 LRU 回收
```

***

## 22. 事务与并发控制示例

### 22.1 Bootstrap 注册事务

建议流程：

```sql
begin;

select id, gateway_id, max_uses, used_count, status, expire_at
from bootstrap_tokens
where token_hash = $1
for update;

-- 校验 status / expire_at / used_count

update bootstrap_tokens
set used_count = used_count + 1,
    updated_at = now()
where id = $2;

-- upsert edge_nodes
-- insert edge_identities

commit;
```

关键点：

- token 行必须 `for update`
- `used_count + 1` 与身份创建必须在同一事务中完成

### 22.2 任务 claim 并发控制

V1 默认 claim 真相源在**云端主库**，`gateway-runtime` 为无状态 worker，靠原子 claim 去重：

```text
先 claim
再 dispatch
claim 过期可被其他实例接管
```

参考 claim 语句（用独立 claim 字段，不复用 message）：

```sql
update tasks
set owner_instance = $owner_instance,
    claim_expire_at = now() + interval '5 minutes',
    dispatch_status = 'Claimed',
    updated_at = now()
where id = $task_id
  and scope = 'Node'
  and (dispatch_status is null
       or dispatch_status = 'Unclaimed'
       or claim_expire_at < now());   -- 允许接管过期 claim
```

只有 `update` 影响行数为 1 的实例才获得投递权；结果回传按 `task_id + node_id` 幂等归并。

***

## 23. 索引设计注意事项

V1 应避免两类问题：

### 23.1 索引不够导致慢查询

重点关注：

```text
按 gateway + status 查任务
按 node 查 identity
按 expire_at 查即将过期身份
按 last_access_at 做缓存清理
```

### 23.2 索引过多导致写放大

不要为每个 JSON 字段都加索引。

V1 优先保证：

```text
主查询路径
唯一约束
恢复路径
```

而不是一次性把所有可能查询都优化完。

***

## 24. 初始 SQL Migration 草案

如果开始落地实现，V1 第一批 migration 建议只覆盖最核心链路。

推荐顺序：

```text
001_gateways
002_edge_nodes
003_edge_identities
004_bootstrap_tokens
005_models
006_runtime_profiles
007_model_deployments
008_tasks
009_task_runs
010_task_events
011_gateway_runtime_instances
012_gateway_cache_entries
013_edge_runtime_states
```

### 24.1 第一阶段必须先建的表

如果目标是先打通：

```text
节点接入
身份签发
任务流转
```

那第一阶段最少需要：

```text
gateways
edge_nodes
edge_identities
bootstrap_tokens
tasks
task_runs
task_events
```

也就是说，**现在已经可以开始写第一批代码了**。

最小可落地闭环是：

```text
BootstrapToken
→ EdgeIdentity
→ EdgeNode
→ Task
→ TaskResult
```

### 24.2 第一版 migration 示例顺序

建议：

```sql
-- 001_create_gateways.sql
-- 002_create_edge_nodes.sql
-- 003_create_edge_identities.sql
-- 004_create_bootstrap_tokens.sql
-- 005_create_tasks.sql
-- 006_create_task_runs.sql
-- 007_create_task_events.sql
```

然后第二阶段再补：

```sql
-- 008_create_models.sql
-- 009_create_runtime_profiles.sql
-- 010_create_model_deployments.sql
-- 011_create_gateway_runtime_instances.sql
-- 012_create_gateway_cache_entries.sql
-- 013_create_edge_runtime_states.sql
```

### 24.3 为什么现在就可以开始写代码

因为以下关键问题已经有明确答案：

- 主数据库在云端 `PostgreSQL`
- `Gateway` / `Edge` 的本地缓存与状态边界明确
- API 契约已经统一到 `proto + gRPC`
- 任务状态机已经定义
- 身份模型与唯一约束已经定义

这意味着后续补文档只应该服务于实现细节，而不是继续阻塞开工。

### 24.4 建议第一批实现顺序

如果现在开始写代码，建议按下面的顺序：

```text
1. proto: bootstrap / agent / task
2. migrations: gateways / edge_nodes / edge_identities / bootstrap_tokens / tasks
3. NodeOnboardingService
4. AgentService: PullTasks / ReportTaskResult / ReportHeartbeat
5. Gateway 本地状态存储
6. 最小 task dispatcher
```

这样可以先打通最小闭环：

```text
节点注册
证书签发
任务拉取
任务回报
状态上报
```

### 24.5 还没写完但不阻塞开工的内容

这些内容仍然值得继续补，但已经不阻塞第一批编码：

```text
完整 proto message 细节
grpc-gateway 暴露边界
完整 SQL DDL
所有错误码映射
完整缓存清理策略
```

原则上：

```text
先打通最小闭环
再补全扩展细节
```
