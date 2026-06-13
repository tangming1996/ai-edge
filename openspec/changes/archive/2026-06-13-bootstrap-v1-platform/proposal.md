## Why

`EdgeAI Runtime Platform` 目前只有设计文档（`README.md` + `docs/design/`），尚无任何可运行代码。设计文档已明确判断"第一批代码开工基线"满足：主库边界、`proto + gRPC` 契约、任务状态机、身份模型与唯一约束、首批 migration 顺序都已锁定。

本变更把这套 V1 设计转化为一份**可执行的系统开发方案**：定义平台各能力的需求契约（specs）、落地架构（design）与分阶段实施步骤（tasks），先打通"节点安全接入 → 身份签发 → 任务流转 → 模型分发 → 运行可观测"的最小闭环，再逐步补齐部署编排、运行时抽象与升级安全。

## What Changes

- 建立 `proto + gRPC` 单一契约与代码生成工具链（buf），作为所有服务间通信的唯一来源；`grpc-gateway` 仅作可选 HTTP/JSON 适配层。
- 建立云端 `PostgreSQL` 主元数据库的表结构与首批 migration（gateways / edge_nodes / edge_identities / bootstrap_tokens / tasks / task_runs / task_events，随后补 models / deployments 等）。
- 实现 Node Onboarding 与安全体系：Bootstrap Token、CSR 签发（Control Plane 内置 Signer + 区域 Intermediate CA）、`EdgeIdentity`、mTLS 长连接 + 逐请求身份校验、证书续签、节点吊销。
- 实现 Task Engine：父子任务模型（DeploymentTask → NodeTask）、状态机、重试/超时、幂等、云端原子 claim、审计（tasks / task_runs / task_events）。
- 实现 `gateway-runtime`：无状态区域接入代理（mTLS 终止）、任务展开与 claim 分发、状态聚合、制品 HTTP Range 文件通道、断网只读自治。
- 实现 `edge-agent`：bootstrap / identity / heartbeat / downloader / runtime-manager / metrics / task-runner / updater，及本机恢复现场。
- 实现 Model Registry 与 ModelDeployment：模型元数据 + 对象存储制品 + 签名校验；部署意图 + Controller 目标选择 + rollout 计数对账。
- 实现 Runtime 抽象：`RuntimeProfile` 选择规则 + `runtime: auto` 解析 + 运行时适配器（llama.cpp / vLLM / TensorRT / ONNX）。
- 实现可观测能力：heartbeat / metrics / runtime-state 上报，指标进入 `Prometheus`，状态摘要进主库。
- 提供 Control Plane 管理面（API Server admin services、Controller Manager）与最小 Dashboard/CLI 入口。

## Capabilities

### New Capabilities
- `api-contracts`: `proto` 单一契约源、service 拆分、message/enum/错误模型约定与 buf 代码生成工具链（含制品文件通道这一登记例外）。
- `metadata-datastore`: 云端 `PostgreSQL` 主库表结构、唯一/部分唯一约束、关键索引、事务边界与首批 SQL migration。
- `node-onboarding-security`: Bootstrap Token 生命周期、CSR 证书签发与 CA 层级、`EdgeIdentity`、mTLS + 逐请求身份校验、证书续签与节点吊销。
- `task-engine`: 父子任务模型、任务状态机、重试/超时/取消、幂等、云端原子 claim、任务审计与历史。
- `gateway-runtime`: 无状态区域接入代理、任务展开与 claim 分发、状态聚合、制品 Range 文件通道、断网只读自治。
- `edge-agent`: 边缘代理各子模块（bootstrap/identity/heartbeat/downloader/runtime-manager/metrics/task-runner/updater）与本机恢复现场。
- `model-management`: Model Registry 元数据与制品/签名管理、`ModelDeployment` 部署意图、Controller 目标选择与 rollout 计数对账。
- `runtime-abstraction`: `RuntimeProfile` 选择规则、`runtime: auto` 解析、异构运行时适配器统一执行接口。
- `observability`: 节点状态/指标/运行时状态上报与存储分层（状态摘要进主库、时序指标进 `Prometheus`）。

### Modified Capabilities
<!-- 无既有 spec，全部为新增能力 -->

## Impact

- 新增代码骨架（建议 Go monorepo）：`api/proto/`、`api/gen/`、`cmd/`（apiserver / controller / gateway-runtime / edge-agent / edgectl）、`internal/`（各能力实现）、`migrations/`、`deploy/`（K8s manifests）。
- 新增依赖：`buf` 工具链、`protoc-gen-go` / `protoc-gen-go-grpc` / `grpc-gateway`、`PostgreSQL` 驱动与迁移工具、`MinIO` 客户端、`Prometheus` 客户端、`cosign`/`ed25519` 签名校验。
- 外部系统：`Kubernetes`（仅作 Control Plane 宿主）、`PostgreSQL`（主库）、`MinIO`（对象存储）、`Prometheus`（指标）。
- 安全面：引入平台 Root CA + 区域 Intermediate CA（私钥仅在 Control Plane Signer）、mTLS 证书生命周期管理。
- V1 非目标：Edge 不接入 K8s、不做复杂调度器/工作流引擎、不做双向 streaming、不做分库分表与细粒度 RBAC。
