# AI Edge — manifests

本目录包含在 Kubernetes 环境中部署 AI Edge 平台所需的全部配置，包含容器镜像构建和 Helm Chart 两部分。

## 目录结构

```
manifests/
├── dockerfiles/                  # Control plane 容器镜像多阶段构建
│   ├── Dockerfile.apiserver
│   ├── Dockerfile.controller
│   ├── Dockerfile.gateway-runtime
│   └── Makefile                  # 镜像构建和推送（linux/amd64 + linux/arm64）
│
├── helm/ai-edge/                 # 主 Helm Chart
│   ├── Chart.yaml
│   ├── values.yaml               # 完整配置参考
│   ├── migrations/               # 打包到 Chart 的数据库迁移 SQL
│   │   ├── 000001_create_gateways.up.sql
│   │   ├── ...
│   │   └── 000013_create_gateway_runtime_instances.up.sql
│   └── templates/
│       ├── _helpers.tpl          # 辅助函数（fullname / 镜像 / Secret 名解析 / DB host / PVC）
│       ├── NOTES.txt             # 安装后提示（含 Secret 清单和节点安装命令）
│       ├── apiserver.yaml        # API Server Deployment + Service
│       ├── controller.yaml       # Controller Deployment
│       ├── gateway-runtime.yaml  # Gateway Runtime DaemonSet + Service
│       ├── migration-job.yaml    # Helm pre-install/upgrade hook Job：自动跑 golang-migrate
│       ├── postgresql.yaml       # 可选：Chart 内置 PostgreSQL 16-alpine
│       ├── minio.yaml            # 可选：Chart 内置 MinIO
│       ├── prometheus.yaml       # 可选：Chart 内置 Prometheus
│       ├── secrets.yaml          # 自动生成的 Secret（DB / PG / MinIO / CA / TLS）
│       └── serviceaccount.yaml
│
├── scripts/                      # 节点端 / 客户端安装脚本
│   ├── install-edge-agent.sh     # 节点级安装（systemd）
│   └── install-edgectl.sh        # 管理 CLI 安装
│
└── README.md                     # 本文件
```

> Secret 管理的完整说明、优先级规则、故障排查请阅读 [`docs/helm-secrets.md`](../docs/helm-secrets.md)。

## 组件定位说明

| 组件 | 部署方式 | 说明 |
|------|----------|------|
| `apiserver` | Helm Deployment | Control plane API 服务 |
| `controller` | Helm Deployment | Kubernetes Controller |
| `gateway-runtime` | Helm DaemonSet | 边缘网关代理，运行在标记了 `node.edgeai.io/role=gateway` 的节点 |
| `edge-agent` | **节点级二进制** | 运行在每台边缘服务器上，非 K8s Workload，通过 `install-edge-agent.sh` 安装 |
| `edgectl` | **客户端 CLI** | 云端操作员工具，通过 `install-edgectl.sh` 安装 |

## 安装特性总览

Helm Chart 在 V1 之后经过了若干轮收紧，下面三件事是 **默认行为**——大多数用户不再需要手动介入。

### 组件名称不再重复

`helm install ai-edge ./helm/ai-edge` 现在只会得到 `ai-edge-apiserver`、`ai-edge-controller`、`ai-edge-gateway-runtime`，**不再**出现 `ai-edge-ai-edge-apiserver` 这种二级前缀。`_helpers.tpl` 的 `ai-edge.fullname` 会在 `Release.Name == Chart.Name` 时去掉冗余前缀；只有用其他名字 release（如 `helm install prod ./ai-edge`）时才会拼成 `prod-ai-edge-apiserver`，便于多环境并存。配套地，新增的 `ai-edge.apiserverAddr` 辅助函数把 apiserver Service 的 FQDN 注入到 gateway-runtime 的 `CONTROL_PLANE_ADDR`，rename release 不会再把 gateway 跟 apiserver 之间的链路打断。

### 数据库自动迁移

Chart 把仓库根目录 `migrations/*.sql` 一并打包，安装 / 升级时自动渲染一个 `pre-install,pre-upgrade` Hook Job 运行 `migrate/migrate` CLI：

- 数据载体：`<release>-ai-edge-migrations` ConfigMap，键名与 `migrations/` 下文件名一致。
- 触发时机：每次 `helm install` / `helm upgrade` 都跑；Job 报告 `Complete` 之前 Helm 不会进入下一阶段。
- 凭据：复用 `db.secretName`（或 `db.existingSecret`）的 `password` 字段，host 走 `db.host` 或内置 PG Service FQDN。
- 关掉它：`migration.enabled=false`（适用于「我在外部维护 Postgres、不希望 Helm 触碰 schema」的场景）。
- 详见 [§ 7.6](#76-数据库自动迁移) 与 [`docs/helm-secrets.md`](../docs/helm-secrets.md) 的相关章节。

### Gateway 自注册（不再手插表 / 不再读节点 annotation）

`gateway-runtime` 在启动时通过 `GATEWAY_AUTO_REGISTER=true`（Chart 默认 `true`）主动调用 `GatewayService.CreateGateway`，按 gateway **NAME** 幂等创建 / 查找记录。**操作员不再需要手工 `INSERT INTO gateways`**，也**不再**给节点打 `edgeai.io/gateway-*` annotation——NAME 直接来自 K8s 节点名（`spec.nodeName`）。

```bash
# 默认行为：helm install 完成后，gateways 表里应该已经有每节点一行
kubectl exec -n edgeai-system deploy/ai-edge-apiserver -- \
    edgectl gateway list
```

如果 Helm 安装时把 `gatewayRuntime.autoRegister` 关掉了（例如你希望手工控制 gateway 生命周期），或者想创建一个**额外**的独立 gateway region，可以从外部用 `edgectl`：

```bash
edgectl --server apiserver:9090 \
    gateway register --name gateway-shanghai \
    --region cn-east-1 \
    --endpoint gateway-shanghai.example.com:9443
# 末行固定输出 gateway_id: <uuid>，便于 shell 捕获
GATEWAY_ID=$(edgectl ... gateway register ... | tail -n1 | awk '{print $2}')
```

> 注意：`gateway-runtime` 内部用的「gateway id」是 K8s 节点名（`spec.nodeName`），apiserver 自动分配的 UUID 仅记录在日志和 `CACHE_DIR/apiserver_gateway_id` 里，**不**改写运行时变量——下游 dispatcher / task store 都用节点名做归属判断，调换会破坏一致性。详见 [docs/design/03-gateway.md § 4.3.6](../docs/design/03-gateway.md)。

> **为什么不在 chart values 里提供 `gatewayRuntime.name/region/endpoint`？**
> `gatewayRuntime` 是 DaemonSet，每个匹配的节点都会跑一个 Pod；把这三个值放在 `values.yaml` 等于强制所有 Pod 注册到同一个 gateway 实体——多 region 集群下是错的，单 region 也容易误填。曾经尝试通过 K8s Node Annotation + Downward API 把每节点属性注入到 Pod，但 `metadata.annotations[...]` Downward API 只能读到 **Pod 自己的** annotation（Pod 不会继承 Node 的 annotation），那个方案其实是「沉默地失败」——变量被设为空字符串，看上去工作但 region / endpoint 全被吞掉。
>
> 现在的方案是「先自注册，必要时再补登记」：自注册只把节点名写进 `gateways` 表的 NAME 字段，需要给某个 region 配 endpoint / region 元数据时，再用 `edgectl gateway update` 改（见 [docs/cli-usage.md § 1.4](../docs/cli-usage.md)）。不再依赖 K8s 1.27+ 的 `metadata.annotations` 能力。

## 1. 构建容器镜像

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

## 2. 部署前准备

### 2.1 前置依赖

| 工具 | 版本要求 | 说明 |
|------|----------|------|
| Kubernetes | 1.24+ | 任意发行版（EKS / GKE / AKE / 自建） |
| Helm | 3.10+ | 用于部署主 Chart |
| kubectl | 1.24+ | 与集群 API Server 通信 |
| 镜像仓库 | — | 推送 apiserver / controller / gateway-runtime 镜像 |

### 2.2 节点要求

- `gateway-runtime` 通过 `nodeSelector: node.edgeai.io/role=gateway` 调度到打了该标签的节点；该节点需要 9443 端口可达（mTLS），并允许 `hostNetwork: true`（可选）。
- 边缘节点（`edge-agent`）仅需 Linux + systemd；可通过 `install-edge-agent.sh` 一键安装。

### 2.3 镜像可达性

Chart 默认从 Docker Hub `tming3379` 拉取镜像。如果使用私有仓库，请先构建并推送（见 §1），再按需覆盖：

```bash
--set global.imageRegistry=your-registry.io
# 或直接为单个组件覆盖：
--set apiserver.image.repository=registry.internal/edgeai-apiserver
--set apiserver.image.tag=v0.1.0
```

## 3. 快速部署（all-in-one / 开发环境）

让 Chart 自己生成 Postgres、MinIO 和所有 TLS 材料，一行命令即可在本地集群或 CI 中跑通：

```bash
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --create-namespace \
  --set postgresql.enabled=true \
  --set minio.enabled=true \
  --set prometheus.enabled=true \
  --set apiserver.ca.generate=true \
  --set gatewayRuntime.tls.generate=true \
  --set global.storageClass=default
```

安装完成后 `helm install` 的输出会打印一个 `== Secrets ==` 块，列出本次 release 自动创建的所有 Secret 名称。**请立即把里面的密码保存到安全位置**：

```bash
# 1) 查看 release 中所有 Secret
kubectl get secrets -n edgeai-system -l app.kubernetes.io/instance=ai-edge

# 2) 取一个自动生成的密码（控制面 / DB）
kubectl get secret -n edgeai-system edgeai-db \
  -o jsonpath='{.data.password}' | base64 -d
```

> 自动生成的密码使用 `randAlphaNum 24`，每次 `helm install` 都会重新生成。
> `helm upgrade` 默认不会更新已有 Secret 的 data，因此升级不会轮换密码；如需轮换请先删除 Secret 再 `helm upgrade`。
> 启用内置 PostgreSQL 时，Postgres Pod 与 apiserver/controller/gateway 共享同一份 `edgeai-db` 凭证（key: `password`），不再生成单独的 `*-postgresql-secret`。

## 4. 自定义 Secret 部署（生产环境推荐）

所有 Secret 都遵循统一的优先级：

```
existingSecret  >  generate / createSecret  >  组件默认  >  空
```

即：一旦设置 `*.existingSecret`，Chart 既不会读、也不会创建该 Secret；只有当 `existingSecret` 为空且 `generate=true`（或该组件默认会自建）时，Chart 才生成 Secret。

### 4.1 场景 A：使用外部托管 PostgreSQL（推荐生产）

把数据库密码放在你自己管理的 Secret 里，Chart 只会引用、不会改写：

```bash
# 1) 创建数据库 Secret
kubectl create namespace edgeai-system
kubectl create secret generic corp-db \
  --namespace edgeai-system \
  --from-literal=password='YOUR_DB_PASSWORD' \
  --from-literal=username=edgeai \
  --from-literal=database=edgeai

# 2) 部署（DB 用外部，MinIO 仍由 Chart 自带，TLS 自签）
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --set db.host=edgeai-prod.cluster-abc123.us-east-1.rds.amazonaws.com \
  --set db.existingSecret=corp-db \
  --set minio.enabled=true \
  --set apiserver.ca.generate=true \
  --set gatewayRuntime.tls.generate=true
```

Chart 仍会在 release 命名空间里创建 MinIO、TLS 等其他 Secret；如果你希望全部用外部对象，请同时设置 `apiserver.ca.existingSecret`、`gatewayRuntime.tls.existingSecret`、`minio.auth.existingSecret`（见 4.3）。

### 4.2 场景 B：完全使用公司 PKI（cert-manager / Vault / 内部 CA）

最接近「严格生产」的形态：所有证书与密码都由贵司的 PKI / Secret 管理平台签发，Chart 零 Secret 生成：

```bash
# 1) 准备 apiserver CA。apiserver 进程直接读取 Secret 中
#    名为 ca.crt / ca.key 的键(CA_CERT_PATH/CA_KEY_PATH)。
#    同一份 PEM 也以 tls.crt / tls.key 别名写入,便于把
#    Secret 当作 kubernetes.io/tls 给其他组件消费。
kubectl create secret generic corp-apiserver-ca \
  --namespace edgeai-system \
  --from-file=tls.crt=./pki/apiserver-ca.crt \
  --from-file=tls.key=./pki/apiserver-ca.key \
  --from-file=ca.crt=./pki/apiserver-ca.crt \
  --from-file=ca.key=./pki/apiserver-ca.key

# 2) 准备 gateway mTLS 证书(包含 tls.crt / tls.key,可选 ca.crt)
kubectl create secret generic corp-gateway-tls \
  --namespace edgeai-system \
  --from-file=tls.crt=./pki/gateway.crt \
  --from-file=tls.key=./pki/gateway.key

# 3) 准备数据库 Secret
kubectl create secret generic corp-db \
  --namespace edgeai-system \
  --from-literal=password='YOUR_DB_PASSWORD' \
  --from-literal=username=edgeai \
  --from-literal=database=edgeai

# 4) 部署
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --set db.host=postgres.prod.internal \
  --set db.existingSecret=corp-db \
  --set postgresql.enabled=false \
  --set apiserver.ca.existingSecret=corp-apiserver-ca \
  --set gatewayRuntime.tls.existingSecret=corp-gateway-tls
```

### 4.3 场景 C：组件级粒度

Chart 支持按组件关闭，关闭后该组件的 Secret 也不会生成：

```bash
# 关闭 controller 和 gateway-runtime，只保留控制面
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --set controller.enabled=false \
  --set gatewayRuntime.enabled=false \
  --set postgresql.enabled=true \
  --set apiserver.ca.generate=true
```

| 组件 | 关闭后行为 |
|------|-----------|
| `apiserver.enabled=false` | 不创建 Deployment / Service；其 CA Secret 也不再生成 |
| `controller.enabled=false` | 不创建 Controller Deployment |
| `gatewayRuntime.enabled=false` | 不创建 DaemonSet；其 TLS / CA Secret 不再生成 |
| `postgresql.enabled=false` | 不创建内置 Postgres；DB 凭证必须由 `db.existingSecret` 提供 |
| `minio.enabled=false` | 不创建内置 MinIO；上游需通过 `minio.externalHost` 提供 |
| `prometheus.enabled=false` | 不创建内置 Prometheus |

### 4.4 通用：所有可被外部覆盖的 Secret

| Secret | 自动生成条件 | 外部覆盖 key | 默认 Secret 名 |
|--------|-------------|-------------|---------------|
| 控制面 DB 凭证（同时供内置 Postgres 使用） | `postgresql.enabled=true` 或 `db.createSecret=true` | `db.existingSecret` | `edgeai-db` |
| 内置 MinIO 凭证 | `minio.enabled=true` 且无 `existingSecret` | `minio.auth.existingSecret` | `<release>-minio-secret` |
| apiserver CA | `apiserver.ca.generate=true` | `apiserver.ca.existingSecret` | `<release>-ai-edge-apiserver-ca` |
| gateway mTLS 证书 | `gatewayRuntime.tls.generate=true` | `gatewayRuntime.tls.existingSecret` | `<release>-ai-edge-gateway-tls` |
| gateway CA bundle | `gatewayRuntime.ca.generate=true` | `gatewayRuntime.ca.existingSecret` | `<release>-ai-edge-gateway-ca` |

> 当 `apiserver.ca.generate` 与 `gatewayRuntime.tls.generate` 同时为 true 时，gateway 的 mTLS 证书会**由 apiserver CA 签发**，这样 `gateway-tls` Secret 自带的 `ca.crt` 既是签名 CA 也是校验 CA，避免循环信任。

### 4.5 安装后查看

```bash
helm status ai-edge -n edgeai-system
helm get manifest ai-edge -n edgeai-system | head -200
kubectl get all,secret,configmap -n edgeai-system -l app.kubernetes.io/instance=ai-edge
```

## 5. 节点接入（edge-agent）

`edge-agent` 不通过 Helm 部署，而是在每台边缘服务器上独立安装。完整流程分三步：

### 5.1 取得 gateway_id

默认情况下，gateway-runtime Pod 在 `GATEWAY_AUTO_REGISTER=true` 时**自动**向 apiserver 调用 `CreateGateway`，因此 `helm install` 完成后 `gateways` 表里应该已经有每节点一行（NAME = K8s 节点名）。可以直接查：

```bash
kubectl exec -n edgeai-system deploy/ai-edge-apiserver -- \
    edgectl gateway list
```

如果 Helm 安装时把 `gatewayRuntime.autoRegister` 关掉了（例如你希望手工控制 gateway 生命周期），或者想给同一个 region 增补一条记录（多 region 共享一个 control plane），可以从外部用 `edgectl`：

```bash
# 在 apiserver 可达的管理机上执行
edgectl --server <apiserver>:9090 \
    gateway register \
    --name gateway-shanghai \
    --region cn-east-1 \
    --endpoint gateway-shanghai.example.com:9443

# 末行固定输出 gateway_id: <uuid>
GATEWAY_ID=$(edgectl ... gateway register ... | tail -n1 | awk '{print $2}')
```

> 自注册的 gateway **NAME** 就是 K8s 节点名（来自 `fieldRef: spec.nodeName`），
> 不是 UUID；同名 `register` 会复用同一行，**不会**重复插入。

#### 5.1.1 补登记 region / endpoint（post-register）

自注册只把节点名写进 `gateways.name`，region / endpoint / labels 这些业务属性**不在自注册路径里**——它们是操作员控制的元数据，Pod 每次重启都不应该重写。需要在安装完成后给某条 gateway 补 region / endpoint，用 `edgectl gateway update`：

```bash
# 把 gateway name 解析成 UUID 后再 update；update 支持 endpoint / labels
edgectl --server <apiserver>:9090 \
    gateway update gateway-shanghai \
    --endpoint gateway-shanghai.example.com:9443 \
    --label env=prod --label site=shanghai
```

完整字段说明见 [docs/cli-usage.md § 1.4](../docs/cli-usage.md)。

> **节点级 gateway 业务属性的来源 — 不再走 Node Annotation**。
> 旧版本曾尝试用 `kubectl annotate node ... edgeai.io/gateway-name=...`
> 加 K8s 1.27+ 的 `fieldRef: metadata.annotations['...']` Downward API 把每节点
> 属性注入到 Pod，但 Downward API 只能读到 **Pod 自己的** annotation
> （Pod 不会继承 Node 的 annotation），那段配置实际上是「沉默地失败」
> ——变量被设为空字符串，看上去工作但 region / endpoint 全被吞掉。
> 当前的「自注册 + edgectl gateway update」两段式既不依赖 K8s 1.27+ 特性，
> 也避免了在 chart values 里硬塞节点级配置导致多 Pod 共用同一身份的问题。

### 5.2 申请 bootstrap token

```bash
kubectl exec -n edgeai-system deploy/ai-edge-apiserver -- \
    edgectl token create \
    --gateway "$GATEWAY_ID" \
    --expires-in 24h \
    --max-uses 50 \
    --description "shanghai-batch-1"
```

输出的 `Plaintext` 字段是节点首次注册的凭证，**只显示一次**。

### 5.3 标记 gateway node + 安装 edge-agent

```bash
# 1) 标记希望运行 gateway-runtime / 接收 edge-agent 的节点
kubectl label node <node-name> node.edgeai.io/role=gateway

# 2) （可选）给该 gateway 补 region / endpoint 元数据。
#    NAME 已经由 §5.1 自注册按节点名创建，这里只改元数据。
edgectl --server <apiserver>:9090 \
    gateway update <node-name> \
    --region cn-east-1 \
    --endpoint <node-name>.example.com:9443

# 3) 在每台边缘节点上执行
curl -sL https://raw.githubusercontent.com/tangming1996/ai-edge/main/manifests/scripts/install-edge-agent.sh | \
    GATEWAY_ID="$GATEWAY_ID" \
    GATEWAY_ADDR=ai-edge-gateway-runtime.edgeai-system.svc.cluster.local:9443 \
    TOKEN=<bootstrap-token-plaintext> \
    bash
```

> 注:`GATEWAY_ADDR` 指向 **gateway-runtime** 的 gRPC 端口 (默认 9443, mTLS),不是
> apiserver。edge-agent 启动后只与 gateway-runtime 通信,经由它再访问 apiserver
> 和 controller。旧名 `CONTROL_PLANE_ADDR` 仍可作为 alias 使用,会打印 deprecation
> 警告。
>
> 安装后检查状态：

```bash
systemctl status edge-agent
journalctl -u edge-agent -f
```

首次启动时 `edge-agent` 用 bootstrap token 向 Control Plane 注册，换取 mTLS 证书；后续重启直接走 mTLS，不再需要 token。

## 6. 安装 edgectl（管理 CLI）

edgectl 是云端操作员工具，安装在管理员工作机：

```bash
# 一键安装（最新版本）
curl -sL https://raw.githubusercontent.com/tangming1996/ai-edge/main/manifests/scripts/install-edgectl.sh | bash

# 指定版本
VERSION=v0.1.0 curl -sL https://raw.githubusercontent.com/tangming1996/ai-edge/main/manifests/scripts/install-edgectl.sh | bash

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

## 7. values.yaml 关键配置

> 表中仅列出最常用的 key。完整列表参见 `helm/ai-edge/values.yaml`，每个 key 都带有详细注释。

### 7.1 全局 / 镜像

| Key | 说明 | 默认值 |
|-----|------|--------|
| `global.imageRegistry` | 全局镜像仓库前缀 | `tming3379` |
| `global.imagePullPolicy` | 镜像拉取策略 | `IfNotPresent` |
| `global.storageClass` | 集群级默认 StorageClass（可被各组件覆盖） | `""` |
| `namespace` | 部署命名空间 | `edgeai-system` |
| `imagePullSecrets` | 全局 imagePullSecrets 列表 | `[]` |

### 7.2 Control Plane 组件

| Key | 说明 | 默认值 |
|-----|------|--------|
| `apiserver.enabled` | 启用 apiserver | `true` |
| `apiserver.replicaCount` | apiserver 副本数 | `2` |
| `apiserver.image.repository` | 镜像名 | `edgeai-apiserver` |
| `apiserver.image.tag` | 镜像 tag | `latest` |
| `apiserver.ca.existingSecret` | 使用已有的 apiserver CA Secret | `""` |
| `apiserver.ca.generate` | 自动生成自签 CA（10 年） | `false` |
| `apiserver.ca.commonName` | 自动签发时的 CN | `edgeai-platform-ca` |
| `controller.enabled` | 启用 controller | `true` |
| `controller.replicaCount` | controller 副本数 | `1` |

### 7.3 Gateway 组件

| Key | 说明 | 默认值 |
|-----|------|--------|
| `gatewayRuntime.enabled` | 启用 gateway-runtime | `true` |
| `gatewayRuntime.service.type` | Service 类型 | `LoadBalancer` |
| `gatewayRuntime.service.grpcPort` | mTLS gRPC 端口 | `9443` |
| `gatewayRuntime.hostNetwork` | 使用 hostNetwork | `false` |
| `gatewayRuntime.nodeSelector` | 节点选择器 | `{node.edgeai.io/role: gateway}` |
| `gatewayRuntime.tls.existingSecret` | 使用已有的 gateway mTLS Secret | `""` |
| `gatewayRuntime.tls.generate` | 自动生成 mTLS 证书 | `false` |
| `gatewayRuntime.tls.commonName` | 自动签发时的 CN | `edgeai-gateway` |
| `gatewayRuntime.ca.existingSecret` | 使用已有的 gateway CA bundle | `""` |
| `gatewayRuntime.ca.generate` | 自动生成 gateway CA | `false` |
| `gatewayRuntime.autoRegister` | 启动时向 apiserver 调用 `CreateGateway`（按 gateway NAME 幂等） | `true` |
| `gatewayRuntime.env.*` | 进程级运行时配置（HTTP_ADDR / GRPC_ADDR / CACHE_DIR / 各 TTL / …） | 详见 `values.yaml` |

> 自注册的 gateway **NAME** = K8s 节点名；region / endpoint 等业务属性**不在 chart values 暴露**，统一在 helm install 完成后用 `edgectl gateway update` 补登记——详见 [§ 5.1](#51-取得-gateway_id) 与 [docs/design/03-gateway.md § 4.3.6](../docs/design/03-gateway.md)。`gatewayRuntime.env.GATEWAY_NAME` / `GATEWAY_REGION` / `GATEWAY_ENDPOINT` 这三个 env 是内部 debug / 单元测试接口，**生产不应**在 chart 里覆盖。

### 7.4 内置中间件

| Key | 说明 | 默认值 |
|-----|------|--------|
| `postgresql.enabled` | 使用 Chart 内置 PostgreSQL | `false` |
| `postgresql.persistence.enabled` | 启用持久化 | `true` |
| `postgresql.persistence.size` | PVC 大小 | `10Gi` |
| `postgresql.auth.existingSecret` | 内置 PG 改读这个 Secret 中的 `password` 字段（其它场景保持默认 `edgeai-db`） | `""` |
| `minio.enabled` | 使用 Chart 内置 MinIO | `false` |
| `minio.externalHost` | 外部 MinIO 地址 | `""` |
| `minio.defaultBuckets` | 默认创建的 bucket | `edgeai-models` |
| `minio.auth.existingSecret` | 使用已有的 MinIO 凭证 Secret | `""` |
| `prometheus.enabled` | 使用 Chart 内置 Prometheus | `false` |
| `prometheus.scrapeInterval` | 抓取间隔 | `15s` |

### 7.5 数据库连接（所有组件共用）

| Key | 说明 | 默认值 |
|-----|------|--------|
| `db.host` | 数据库地址 | `""`（外部）或内置 Postgres FQDN |
| `db.port` | 数据库端口 | `5432` |
| `db.username` | 数据库用户名 | `postgres` |
| `db.database` | 数据库名 | `edgeai` |
| `db.existingSecret` | 引用已有的 DB 凭证 Secret（最高优先级） | `""` |
| `db.secretName` | 自动生成 Secret 时使用的名字 | `edgeai-db` |
| `db.createSecret` | 强制自动生成 DB Secret（即使未启用内置 PG） | `false` |
| `db.sslmode` | Postgres SSL 模式 | `disable` |

> 优先级：设置了 `db.existingSecret` → 使用你的 Secret；否则若 `postgresql.enabled=true` → 自动创建并发布 `db.secretName`；否则不创建任何 DB Secret，部署会因 `DB_HOST` 为空而 `Pending`/`Running` 但连不上库。
>
> 启用内置 PostgreSQL 时，Postgres Pod 直接从 `db.secretName`（默认 `edgeai-db`）读取 `POSTGRES_PASSWORD`——它**不**再独立生成 `*-postgresql-secret`，从根上避免 Postgres 与应用组件的密码漂移。如果是从早于本次重构的版本升级，旧 release 中的 `*-postgresql-secret` 仍是孤儿 Secret（chart 不会再渲染它），需要 `helm uninstall` 后重新 `helm install` 才能彻底清理（或手动 `kubectl delete secret`）。

### 7.6 数据库自动迁移

> 完整背景与 troubleshooting 见 [`docs/helm-secrets.md`](../docs/helm-secrets.md#迁移job)。

| Key | 说明 | 默认值 |
|-----|------|--------|
| `migration.enabled` | 总开关；设为 `false` 时 chart 不会渲染迁移 Job / ConfigMap | `true` |
| `migration.image.repository` | 迁移使用的镜像 | `migrate/migrate` |
| `migration.image.tag` | 镜像 tag | `v4.17.1` |
| `migration.image.pullPolicy` | 镜像拉取策略（为空时继承 `global.imagePullPolicy`） | `""` |
| `migration.activeDeadlineSeconds` | 单次 Job 运行的硬超时 | `600` |
| `migration.backoffLimit` | Job 重试次数上限 | `5` |
| `migration.resources` | 迁移容器的资源 requests / limits | `cpu: 50m/200m, mem: 64Mi/128Mi` |

工作流程：

1. `helm install` / `helm upgrade` 进入 `pre-install,pre-upgrade` 阶段，Helm 渲染 `<release>-ai-edge-migrations` ConfigMap 和 `<release>-ai-edge-migrate` Job。
2. ConfigMap 装载 `migrations/*.up.sql` / `*.down.sql` 到容器内的 `/migrations/`。
3. Job 容器执行 `migrate -path /migrations -database $DATABASE_URL up`；`golang-migrate` 在幂等模式下：未应用过则全部应用，已最新则 no-op。
4. Job 进入 `Complete` 状态后 Helm 才继续 `pre-install,pre-upgrade` 的后续钩子及主资源渲染。
5. 旧 ConfigMap / Job 在下一次 hook 触发前由 `before-hook-creation,hook-succeeded` 删除策略清理。

如果安装时想看一眼迁移日志：

```bash
kubectl logs -n edgeai-system -l app.kubernetes.io/component=migration \
    --tail=200
```

常见故障：

| 现象 | 原因 / 处置 |
|------|------------|
| Job 卡在 `ContainerCreating` 等 `migrations` ConfigMap | 检查是否有人在外部改了 ConfigMap 的 ownerReference；正常情况下 hook 阶段会自行创建。 |
| Job 退 `Error`，`no migration found` | 仓库内 `migrations/` 缺失或被打包过滤掉；`helm install --dry-run --debug ./helm/ai-edge` 看 ConfigMap data 列表。 |
| 报告 `connection refused` | 走到内置 PG 时常见：PG Pod 还没 `Ready` 时迁移 Job 已经启动。Job 的 `backoffLimit=5` + `activeDeadlineSeconds=600` 通常能自愈，若持续失败可 `kubectl get pods -n edgeai-system -l app.kubernetes.io/name=postgresql` 排查 PG 自身。 |
| 想跳过迁移 | 临时 `helm install ... --set migration.enabled=false`，等生产维护窗口再 `helm upgrade` 重新开启。 |

## 8. Helm 校验 / 排错

```bash
# 渲染检查（不安装）
helm template ./helm/ai-edge

# 带参数渲染
helm template ./helm/ai-edge \
  --set postgresql.enabled=true \
  --set minio.enabled=true \
  --set apiserver.ca.generate=true \
  --set gatewayRuntime.tls.generate=true

# Lint 检查
helm lint ./helm/ai-edge

# Dry-run 实际集群安装
helm install ai-edge ./helm/ai-edge \
  --namespace edgeai-system \
  --create-namespace \
  --dry-run \
  --set postgresql.enabled=true
```

安装后如果 Pod 卡在 `CreateContainerConfigError` 且提示 `secret "X" not found`，说明某个 Secret 名解析出来但实际不存在。对照 [`docs/helm-secrets.md`](../docs/helm-secrets.md) 的「Secret inventory」一节，确认要 `existingSecret` 指向已有 Secret，还是把 `generate=true` 让 Chart 自动生成。

## 9. 清理 helm 部署

```bash
helm uninstall ai-edge -n edgeai-system

```
