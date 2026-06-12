## 1. Controller Entrypoint

- [x] 1.1 为 `cmd/controller` 增加环境变量配置读取、数据库初始化和信号处理
- [x] 1.2 装配 `internal/deployment.Controller` 所需的 `TaskCreator` 依赖并启动 reconcile loop
- [x] 1.3 为 `controller` 补最小启动日志和失败即退出行为，确保不再是占位程序

## 2. Gateway Runtime Entrypoint

- [x] 2.1 盘点 `gateway-runtime` 启动所需配置项，并在 `cmd/gateway-runtime` 中实现环境变量加载与默认值
- [x] 2.2 装配数据库、identity cache、upstream control plane 连接、onboarding proxy、dispatcher 和 connectivity monitor
- [x] 2.3 启动 `gateway-runtime` 的 gRPC 服务监听，并注册本次入口必须暴露的服务
- [x] 2.4 启动 artifact HTTP 服务与健康检查端点，并接入统一优雅退出流程

## 3. Edge Agent Entrypoint

- [x] 3.1 在 `cmd/edge-agent` 中接入配置文件读取、数据目录准备和信号处理
- [x] 3.2 按“先身份后循环”的顺序完成 `LoadOrBootstrap`、mTLS 连接建立和 node ID 装配
- [x] 3.3 装配 runtime manager、任务执行器和 task runner，使 agent 能执行已拉取任务
- [x] 3.4 启动 heartbeat、证书续签等后台循环，并确保关闭时取消循环且保留本地状态

## 4. Deployment Artifacts And Validation

- [x] 4.1 同步更新 `install.sh`、systemd 单元和 Kubernetes 清单中的启动参数与入口假设
- [x] 4.2 补充最小运行说明或 smoke 路径，覆盖 `controller`、`gateway-runtime`、`edge-agent` 启动验证
- [x] 4.3 执行 `go build ./...` 和必要的聚焦验证，确认三个二进制都能成功启动且不再立即退出
