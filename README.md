# AI Edge

> EdgeAI Runtime Platform for secure onboarding, model distribution, runtime abstraction, and autonomous operation across large-scale edge nodes.

`AI Edge` 是一个面向边缘 AI 场景的轻量级运行平台，用来统一管理分布式边缘设备、模型生命周期、异构推理 Runtime，以及弱网/断网环境下的持续运行能力。

## 当前状态

当前仓库已经具备以下基础能力：

- `go build ./...` 可通过
- `go test ./...` 可通过
- `apiserver`、`controller`、`gateway-runtime`、`edge-agent` 已有可运行入口
- 本地联调依赖已提供：`Postgres`、`MinIO`、`Prometheus`

当前更适合：

- Alpha 验证
- PoC
- 内部联调
- 小范围试点

当前还不建议直接用于生产环境，原因包括：

- 核心链路测试覆盖仍然偏少
- 生产部署清单还不完整
- 配置样例与安全基线说明还需要继续补齐

## 这个项目是干什么的

很多企业在边缘 AI 场景里，依然通过 `ssh`、手工拷贝模型、逐台拉起容器的方式管理现场设备。一旦规模从几台机器增长到几十、几百甚至上万台，这种方式会迅速失控。

`AI Edge` 的目标，是把下面这些能力统一起来：

- 边缘设备安全接入
- 模型注册、版本管理和分发
- 异构推理 Runtime 的统一抽象
- 任务驱动的部署、升级、回滚与审计
- 区域 Gateway 缓存、聚合和离线自治

它不是一个把边缘设备强行做成 Kubernetes Node 的方案，而是一个更贴近边缘 AI 运维现实的 `Edge AI Runtime Platform`。

## 项目目标

- 让企业能够统一管理数百到数万台边缘设备
- 让模型部署、升级、回滚变成标准化任务
- 让不同硬件和 Runtime 的差异被平台屏蔽
- 让区域现场在弱网或断网时仍可继续运行
- 让控制面、区域网关和边缘代理形成清晰职责分层

## 核心能力

- 安全接入：支持 Bootstrap Token、证书身份、续签与吊销
- 区域 Gateway：负责接入、任务分发、状态聚合、制品缓存与区域自治
- Edge Agent：负责任务执行、模型下载、运行时管理、状态上报和自升级
- 模型生命周期：支持模型注册、版本管理、签名校验和制品分发
- Runtime 抽象：面向异构硬件统一封装推理运行时能力
- Task Driven：将安装、启动、升级、停止、删除等动作全部任务化
- Proto First API：gRPC 为主，HTTP/JSON 由同一套 proto 自动生成<mccoremem id="01KTQQSEAFDPEYJ2S5Q38N4TEQ" />

## 适合什么行业

这个项目适合需要在分布式现场运行 AI 推理、并长期管理边缘设备与模型的行业，例如：

- 工业制造与智慧工厂
- 能源、电力、矿山等远程站点
- 零售门店与连锁场景
- 仓储物流与园区
- 交通、安防、城市治理
- 医疗设备、专用终端、边缘服务器

## 架构概览

系统主要由三层组成：

- `Control Plane`：负责 API、控制器、资源管理、签名与核心状态
- `Gateway Runtime`：负责区域接入、任务分发、缓存和离线自治
- `Edge Agent`：负责节点接入、任务执行、模型下载和本地状态维护

设计文档见：

- [README-ARCH.md](./README-ARCH.md)
- [docs/design/README.md](./docs/design/README.md)

## Quick Start

下面是一条最小可跑通的本地联调路径。

### 1. 准备环境

需要安装：

- Go `1.25+`
- Docker 和 Docker Compose
- `migrate` 命令行工具

### 2. 启动本地依赖

```bash
docker compose up -d
```

这会启动：

- PostgreSQL
- MinIO
- Prometheus

### 3. 初始化数据库

```bash
make migrate-up
```

### 4. 构建项目

```bash
go build ./...
```

或者只构建可执行文件：

```bash
make build
```

### 5. 启动 Control Plane

启动 `apiserver`：

```bash
DB_HOST=localhost DB_PORT=5433 DB_USER=postgres DB_PASSWORD=postgres DB_NAME=edgeai \
  go run ./cmd/apiserver
```

启动 `controller`：

```bash
DB_HOST=localhost DB_PORT=5433 DB_USER=postgres DB_PASSWORD=postgres DB_NAME=edgeai \
  go run ./cmd/controller
```

默认情况下：

- gRPC 监听 `:9091`
- HTTP/JSON 监听 `:8081`

### 6. 启动 Gateway Runtime

```bash
GATEWAY_ID=local-gw \
CONTROL_PLANE_ADDR=localhost:9091 \
DB_HOST=localhost DB_PORT=5433 DB_USER=postgres DB_PASSWORD=postgres DB_NAME=edgeai \
HTTP_ADDR=:8082 \
go run ./cmd/gateway-runtime
```

默认情况下：

- gateway gRPC 监听 `:9443`
- artifact HTTP 和 health/metrics 监听 `:8082`

如果未配置 TLS 证书路径，`gateway-runtime` 会以非 mTLS 模式启动，便于本地开发联调；生产环境不应使用这种方式。

### 7. 可选：启动 Edge Agent

先准备一个本地配置文件，例如 `./edge-agent.json`：

```json
{
  "gateway_addr": "localhost:9443",
  "gateway_id": "replace-with-gateway-id",
  "gateway_http_addr": "http://localhost:8082",
  "token": "replace-with-bootstrap-token",
  "data_dir": "./var/lib/edge-agent",
  "heartbeat_interval": "10s",
  "agent_version": "dev"
}
```

然后启动：

```bash
go run ./cmd/edge-agent --config ./edge-agent.json
```

### 8. 验证基础能力

运行测试：

```bash
go test ./...
```

运行 smoke test：

```bash
./scripts/smoke-test.sh
```

## 仓库结构

```text
cmd/             服务入口
internal/        核心领域实现
api/proto/       proto 契约定义
api/gen/         生成代码与 OpenAPI
deploy/          K8s、systemd、Prometheus 配置
docs/design/     详细设计文档
migrations/      数据库迁移
scripts/         安装与联调脚本
```

## 后续方向

如果你想把它推进到生产可用，通常还需要继续补齐：

- 更完整的部署编排与发布流程
- 更高的单测与集成测试覆盖
- 更严格的证书、密钥和配置管理
- 更完整的监控、告警与运维手册
- 标准化安装包、镜像和升级路径

## 发布版本

当前仓库已经支持通过 GitHub Release 自动发布 Docker Hub 镜像、Linux 二进制，以及 `edgectl` 的 macOS 二进制。<mccoremem id="03gatqgctkqpwepjbh5e4zy29" />

发布前需要在 GitHub 仓库配置以下 Secrets：

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

可选配置：

- `Repository Variable: DOCKERHUB_NAMESPACE`

如果未设置 `DOCKERHUB_NAMESPACE`，发布流程默认使用 `DOCKERHUB_USERNAME` 作为 Docker Hub 命名空间。

发布正式版本的最小步骤如下：

```bash
git add .
git commit -m "Prepare release v0.1.0"
git push origin main

git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

推送 `v*` 标签后会自动执行：

- 运行 `make check`、`make test`、`make verify-generate`、`make verify-license`
- 推送多架构镜像到 `docker.io/<DOCKERHUB_USERNAME>/edgeai-apiserver`
- 推送多架构镜像到 `docker.io/<DOCKERHUB_USERNAME>/edgeai-controller`
- 推送多架构镜像到 `docker.io/<DOCKERHUB_USERNAME>/edgeai-gateway-runtime`
- 发布 `linux/amd64` 和 `linux/arm64` 的二进制到 GitHub Release
- 发布 `edgectl` 的 `darwin/amd64` 和 `darwin/arm64` 二进制到 GitHub Release
- 上传 `checksums.txt`

常用产物示例：

- Docker 镜像：`docker.io/<namespace>/edgeai-apiserver:v0.1.0`
- Docker 镜像：`docker.io/<namespace>/edgeai-controller:v0.1.0`
- Docker 镜像：`docker.io/<namespace>/edgeai-gateway-runtime:v0.1.0`
- Release 资产：`edgectl-linux-amd64`
- Release 资产：`edgectl-darwin-arm64`
- Release 资产：`edge-agent-linux-arm64`

标签策略如下：

- 正式版本，如 `v0.1.0`：发布版本标签并更新 Docker `latest`
- 预发布版本，如 `v0.1.0-rc.1`：只发布对应版本标签，不更新 Docker `latest`

## 许可证

本项目采用 `MIT` 许可证发布。许可证全文见 [LICENSE](./LICENSE)。<mccoremem id="03gat0mt4s8wivxjvvx1k0z9d" />

## 文档入口

- 总体架构：[README-ARCH.md](./README-ARCH.md)
- CLI 与服务命令参考：[docs/cli-usage.md](./docs/cli-usage.md)
- 详细设计：[docs/design/README.md](./docs/design/README.md)
- API 契约：`api/proto/edge/ai/api/v1`
- 部署样例：`deploy/`
