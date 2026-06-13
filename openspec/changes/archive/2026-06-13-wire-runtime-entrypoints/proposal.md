## Why

`gateway-runtime`、`edge-agent`、`controller` 的内部能力已经在 `internal/` 下基本具备，但对应的 `cmd` 入口仍是占位实现，导致仓库只能单独跑起 `apiserver`，无法验证区域网关、节点代理和部署控制器的最小工作闭环。现在补上启动入口接线，可以把现有实现真正暴露为可运行进程，并为后续联调和端到端测试提供稳定入口。

## What Changes

- 为 `gateway-runtime` 增加真实进程入口：加载配置、连接数据库和上游 control plane、注册 gRPC/HTTP 组件、管理后台协程与优雅退出。
- 为 `edge-agent` 增加真实进程入口：读取本地配置、加载或 bootstrap 身份、建立 mTLS 连接并启动 heartbeat、任务执行、续签等后台循环。
- 为 `controller` 增加真实进程入口：加载数据库配置、创建 deployment controller、启动 reconcile loop 并支持优雅退出。
- 统一三个二进制的最小配置约定、日志输出和生命周期管理方式，确保本地开发、systemd 和 Kubernetes 清单都能有明确的启动参数来源。
- 补充最小运行说明和验证路径，确保这些入口不再只是“能编译”，而是“能启动并维持服务循环”。

## Capabilities

### New Capabilities
- `service-entrypoints`: 定义 `gateway-runtime`、`edge-agent`、`controller` 三个二进制必须提供的可运行入口、配置加载、后台循环启动和优雅退出行为

### Modified Capabilities
- `gateway-runtime`: 补充 gateway-runtime 作为独立进程启动时必须装配的服务、依赖和生命周期要求
- `edge-agent`: 补充 edge-agent 作为长期运行代理进程时必须执行的启动、身份装载与后台任务要求

## Impact

- 影响代码主要位于 `cmd/gateway-runtime`、`cmd/edge-agent`、`cmd/controller`，并会触及 `internal/gateway`、`internal/agent`、`internal/deployment` 中缺失的装配层。
- 影响部署与运行方式，包括 `deploy/systemd`、`deploy/k8s`、安装脚本和本地联调命令。
- 不引入新的外部系统依赖，但会把已有的数据库、control plane gRPC、对象存储、节点本地数据目录等依赖显式化。
