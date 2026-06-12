# AI Edge — manifests

本目录包含在 Kubernetes 环境中部署 AI Edge 平台所需的全部配置，包含容器镜像构建和 Helm Chart 两部分。

## 目录结构

```
manifests/
├── dockerfiles/               # Control plane 容器镜像多阶段构建
│   ├── Dockerfile.apiserver
│   ├── Dockerfile.controller
│   ├── Dockerfile.gateway-runtime
│   └── Makefile               # 镜像构建和推送（linux/amd64 + linux/arm64）
│
├── helm/ai-edge/              # 主 Helm Chart
│   ├── Chart.yaml
│   ├── values.yaml            # 完整配置参考
│   └── templates/
│       ├── _helpers.tpl       # 辅助函数（镜像 / DB host / PVC）
│       ├── NOTES.txt          # 安装后提示（含节点安装命令）
│       ├── apiserver.yaml     # API Server Deployment + Service
│       ├── controller.yaml    # Controller Deployment
│       ├── gateway-runtime.yaml  # Gateway Runtime DaemonSet + Service
│       ├── postgresql.yaml    # 可选：PostgreSQL 16-alpine 实例
│       ├── minio.yaml         # 可选：MinIO 实例
│       └── prometheus.yaml    # 可选：Prometheus 实例
│
└── scripts/                   # 节点端 / 客户端安装脚本
    ├── install-edge-agent.sh  # 节点级安装（systemd）
    └── install-edgectl.sh     # 管理CLI安装
```

## 组件定位说明

| 组件 | 部署方式 | 说明 |
|------|----------|------|
| `apiserver` | Helm Deployment | Control plane API 服务 |
| `controller` | Helm Deployment | Kubernetes Controller |
| `gateway-runtime` | Helm DaemonSet | 边缘网关代理，运行在标记了 `node.edgeai.io/role=gateway` 的节点 |
| `edge-agent` | **节点级二进制** | 运行在每台边缘服务器上，非 K8s Workload，通过 `install-edge-agent.sh` 安装 |
| `edgectl` | **客户端 CLI** | 云端操作员工具，通过 `install-edgectl.sh` 安装 |

## 构建容器镜像

所有镜像使用 Go 1.25 + alpine:3.20 多阶段构建，自动完成跨平台编译（linux/amd64 + linux/arm64）。

```bash
cd manifests/dockerfiles

# 构建并推送到 Docker Hub (tming3379)
make build

# 构建单个镜像
make build-apiserver
make build-controller
make build-gateway-runtime

# 推送到自定义 registry
REGISTRY=your-registry.io make build
```

## 部署到 Kubernetes

Helm Chart 支持三种部署场景，所有基础设施组件均可独立启用或关闭。

### 场景一：使用 Chart 内置组件（all-in-one）

适合快速验证或小规模部署：

```bash
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --create-namespace \
  --set postgresql.enabled=true \
  --set postgresql.persistence.storageClass=standard \
  --set minio.enabled=true \
  --set prometheus.enabled=true
```

### 场景二：连接外部数据库，使用 Chart 内置 MinIO + Prometheus

```bash
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --create-namespace \
  --set db.host=your-postgres.example.com \
  --set db.port=5432 \
  --set db.username=postgres \
  --set db.database=edgeai \
  --set db.existingSecret=your-db-secret \
  --set minio.enabled=true \
  --set prometheus.enabled=true
```

### 场景三：完全使用外部依赖

适合生产环境，所有基础设施使用已有实例：

```bash
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --create-namespace \
  --set db.host=your-postgres.example.com \
  --set db.port=5432 \
  --set db.username=postgres \
  --set db.database=edgeai \
  --set db.existingSecret=your-db-secret \
  --set minio.externalHost=your-minio.example.com \
  --set minio.externalPort=9000 \
  --set minio.auth.rootUser=minioadmin \
  --set minio.auth.rootPassword=xxx \
  --set minio.externalBucket=edgeai-models \
  --set prometheus.enabled=false
```

### 前置依赖

- Kubernetes 1.24+
- Helm 3.10+
- 数据库凭证 Secret（若使用外部数据库）

### 准备 Secret

```bash
# 外部数据库凭证（必需，若 db.host 非空）
kubectl create secret generic your-db-secret \
  --from-literal=password="your-db-password" \
  --namespace edgeai-system

# CA 证书（可选，生产环境推荐）
kubectl create secret generic edgeai-ca \
  --from-file=ca.crt=/path/to/ca.crt \
  --from-file=ca.key=/path/to/ca.key \
  --namespace edgeai-system

# Gateway mTLS 证书（可选）
kubectl create secret generic edgeai-gateway-tls \
  --from-file=tls.crt=/path/to/gateway-tls.crt \
  --from-file=tls.key=/path/to/gateway-tls.key \
  --namespace edgeai-system
```

### 生产级完整部署命令

```bash
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --create-namespace \
  --set db.host=postgres-svc.edgeai-system.svc.cluster.local \
  --set db.existingSecret=edgeai-db \
  --set apiserver.caSecretName=edgeai-ca \
  --set gatewayRuntime.gatewayTlsSecretName=edgeai-gateway-tls \
  --set gatewayRuntime.gatewayCaSecretName=edgeai-ca
```

## 节点接入流程（edge-agent）

edge-agent 不通过 Helm 部署，而是在每台边缘服务器上独立安装：

```bash
# 在每台边缘节点上执行
curl -sL https://raw.githubusercontent.com/edgeai-platform/ai-edge/main/manifests/scripts/install-edge-agent.sh | \
    GATEWAY_ID=<gateway-id> \
    CONTROL_PLANE_ADDR=ai-edge-apiserver.edgeai-system.svc.cluster.local:9090 \
    TOKEN=<bootstrap-token> \
    bash
```

安装后检查状态：
```bash
systemctl status edge-agent
journalctl -u edge-agent -f
```

首次启动时 edge-agent 使用 bootstrap token 向 Control Plane 注册，换取 mTLS 证书。后续重启使用 mTLS 认证，不再需要 token。

## 安装 edgectl（管理CLI）

edgectl 是云端操作员工具，安装在管理员工作机上：

```bash
# 一键安装（最新版本）
curl -sL https://raw.githubusercontent.com/edgeai-platform/ai-edge/main/manifests/scripts/install-edgectl.sh | bash

# 指定版本
VERSION=v0.1.0 curl -sL https://raw.githubusercontent.com/edgeai-platform/ai-edge/main/manifests/scripts/install-edgectl.sh | bash

# 启用 Shell 补全
ENABLE_SHELL_COMPLETION=yes bash install-edgectl.sh
```

安装完成后：
```bash
edgectl --help

# 创建 bootstrap token（在一台有 k8s 访问权限的节点上执行）
kubectl exec -n edgeai-system deploy/ai-edge-apiserver -- \
    edgectl token create --gateway <gateway-id> --expires-in 24h
```

## values.yaml 关键配置

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `global.imageRegistry` | 全局镜像仓库前缀 | `tming3379` |
| `apiserver.enabled` | 启用 apiserver | `true` |
| `apiserver.replicaCount` | apiserver 副本数 | `2` |
| `apiserver.caSecretName` | CA 证书 Secret | 空 |
| `controller.enabled` | 启用 controller | `true` |
| `gatewayRuntime.enabled` | 启用 gateway-runtime | `true` |
| `gatewayRuntime.nodeSelector` | Gateway 节点选择器 | `node.edgeai.io/role=gateway` |
| `gatewayRuntime.hostNetwork` | 使用 hostNetwork | `false` |
| `postgresql.enabled` | 使用 Chart 内置 PostgreSQL | `false` |
| `postgresql.persistence.enabled` | 启用持久化 | `true` |
| `postgresql.persistence.size` | PVC 大小 | `10Gi` |
| `minio.enabled` | 使用 Chart 内置 MinIO | `false` |
| `minio.externalHost` | 外部 MinIO 地址 | 空 |
| `minio.externalPort` | 外部 MinIO 端口 | `9000` |
| `minio.defaultBuckets` | 默认创建的 bucket | `edgeai-models` |
| `prometheus.enabled` | 使用 Chart 内置 Prometheus | `false` |
| `prometheus.scrapeInterval` | 抓取间隔 | `15s` |
| `db.host` | 外部数据库地址（优先级最高） | 空 |
| `db.port` | 数据库端口 | `5432` |
| `db.existingSecret` | 数据库凭证 Secret | `edgeai-db` |
| `db.database` | 数据库名称 | `edgeai` |

## Helm 校验

```bash
# 渲染检查
helm template ./helm/ai-edge

# 带参数渲染
helm template ./helm/ai-edge \
  --set postgresql.enabled=true \
  --set minio.enabled=true

# Lint 检查
helm lint ./helm/ai-edge

# 实际集群安装（dry-run）
helm upgrade --install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --create-namespace \
  --dry-run \
  --set postgresql.enabled=true
```
