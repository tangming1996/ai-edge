# CLI & 服务命令参考

本文档说明 `edgectl` CLI 的使用方法、各服务的启动命令与关键环境变量，以及常见工作流。

---

## 一、edgectl CLI

`edgectl` 是平台管理员在 **云端 Control Plane** 侧使用的命令行工具，用于管理 gateway、节点、token、部署任务等资源。所有命令均直连 `apiserver` gRPC 端口（默认 `localhost:9091`），不做节点身份认证。

### 1.1 全局参数

```bash
edgectl [--server <addr>] [--token <bearer-token>]
```

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--server` | `localhost:9091` | apiserver gRPC 地址，可通过 `EDGECTL_SERVER` 环境变量设置 |
| `--token` | 无 | admin 身份 bearer token，可通过 `EDGECTL_TOKEN` 环境变量设置 |

### 1.2 token — Bootstrap Token 管理

所有节点首次注册都需要一个有效的 bootstrap token。token 必须绑定到某个 Gateway。

#### 创建 token

```bash
edgectl token create \
  --gateway <gateway-id> \
  --expires-in 24h \
  --max-uses 10 \
  --description "dev-token"
```

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--gateway` | 必填 | 目标 Gateway ID |
| `--expires-in` | `24h` | token 有效期，支持 `h`、`m`、`s` 单位 |
| `--max-uses` | `10` | 最大使用次数，`0` 表示不限制 |
| `--description` | 空 | 描述信息，便于管理 |

**输出示例：**

```
Token ID:    a1b2c3d4-...
Plaintext:   <PASSWORD>
Gateway:     e7638a3f-e237-4629-b874-3f8c675b40a6
Max Uses:    10
Expires At:  2026-06-11T17:00:00+08:00
```

> **重要**：`Plaintext` 字段只会在创建响应中返回一次，请妥善保存，后续节点注册需要用到。

#### 列出 token

```bash
edgectl token list --gateway <gateway-id>
```

### 1.3 node — 边缘节点管理

#### 查看节点列表

```bash
edgectl node list --gateway <gateway-id>
```

**输出示例：**

```
ID                                    GATEWAY                               STATUS  ONLINE  VERSION  LAST SEEN
3c2a0ab7-e393-4774-b5ee-ebb3083ed5bb  e7638a3f-e237-4629-b874-3f8c675b40a6  Active  true    dev      2026-06-10T10:08:02Z
```

| 字段 | 说明 |
|------|------|
| `ID` | 节点唯一身份 ID，由 Control Plane 在 bootstrap 时分配 |
| `GATEWAY` | 该节点注册的 Gateway ID |
| `STATUS` | 节点身份状态：`Active` / `Suspended` / `Revoked` |
| `ONLINE` | 是否在线（最近是否有心跳） |
| `VERSION` | 节点上 edge-agent 的版本号 |
| `LAST SEEN` | 最后一次心跳时间 |

#### 吊销节点身份

```bash
edgectl node revoke <node-id>
```

吊销后节点证书立即失效，节点再次连接时会收到 `IdentityRevoked` 错误。

### 1.4 gateway — Gateway 管理

Gateway 是区域级逻辑实体。`gateways` 表的主键（UUID）由 apiserver 生成；操作员**不应**直接 `INSERT INTO gateways`——`edgectl gateway` 是唯一的官方入口。

#### 1.4.1 注册 gateway（推荐）

```bash
edgectl gateway register \
  --name gateway-shanghai \
  --region cn-east-1 \
  --endpoint gateway-shanghai.example.com:9443 \
  --label env=prod --label site=shanghai
```

| 参数 | 必填 | 说明 |
|------|------|------|
| `--name` | 是 | 唯一 name（DNS-1123-ish，≤ 63 字符）；同名重复调用是幂等的 |
| `--region` | 否 | 区域标识，便于多 region 过滤 |
| `--endpoint` | 否 | 边缘节点访问该 gateway 使用的公网 mTLS 地址（host:port） |
| `--label` | 否 | `key=value` 形式，可重复 |

输出（多行人读 + 末行机读）：

```
Gateway registered.
ID:       6f0a3b51-...
Name:     gateway-shanghai
Region:   cn-east-1
Endpoint: gateway-shanghai.example.com:9443
Labels:   env=prod,site=shanghai
gateway_id: 6f0a3b51-...
```

末行的 `gateway_id: <id>` 固定在最后，方便 shell 直接捕获：

```bash
GATEWAY_ID=$(edgectl gateway register --name gateway-shanghai --region cn-east-1 \
    | tail -n1 | awk '{print $2}')
```

#### 1.4.2 列出 / 查询 / 删除

```bash
# 全部 gateway
edgectl gateway list
# 按 region 过滤
edgectl gateway list --region cn-east-1
# 按 id 或 name 查询
edgectl gateway get <gateway-id>
edgectl gateway get gateway-shanghai --by-name
# 软删除（status 置为 Deleted，关联 bootstrap token 同步失效）
edgectl gateway delete <gateway-id>
```

> 如果走 Helm Chart 部署，gateway-runtime Pod 默认在 `GATEWAY_AUTO_REGISTER=true` 时
> 自动调用本节 register，所以大多数安装不需要单独运行这条命令；
> 显式 `register` 主要用于：把多个 region 合并到同一个控制面、或在未启用 auto-register
> 的环境里手工补建 gateway。

#### 1.4.3 更新 gateway 业务属性

自注册（`register`）只把 NAME 写进 `gateways` 表，region / endpoint / labels 是
**操作员控制**的元数据——它们不属于 Pod 启动的输入，Pod 重启不应该反复覆盖。
自注册完成后用 `edgectl gateway update` 补登记：

```bash
# update 接受 id 或 name（name 模式自动 --by-name）
edgectl gateway update gateway-shanghai \
    --region cn-east-1 \
    --endpoint gateway-shanghai.example.com:9443 \
    --label env=prod --label site=shanghai
```

| 参数 | 必填 | 说明 |
|------|------|------|
| 位置参数 | 是 | gateway id 或 name（name 模式通过 `--by-name` 切换） |
| `--region` | 否 | 区域标识，覆盖 / 写入 `gateways.region` |
| `--endpoint` | 否 | 公网 mTLS endpoint（host:port），覆盖 `gateways.endpoint` |
| `--label` | 否 | `key=value` 形式，可重复；`--label` 后面带 `-`（`--label -`）可清空单个 key |

> `gateway update` 不会动 NAME（NAME 是 K8s 节点名，承担运行期身份），也不会
> 重置已经发放的 bootstrap token。如果需要彻底下架某 gateway，用
> `edgectl gateway delete <id>`（软删除，`status=Deleted`）。

### 1.5 deployment — 模型部署管理

#### 创建部署任务

```bash
edgectl deployment create \
  --model <model-name>:<version> \
  --gateway <gateway-id> \
  --runtime <runtime-type>
```

| 参数 | 说明 |
|------|------|
| `--model` | 模型名称和版本，格式 `name:version` |
| `--gateway` | 目标 Gateway ID |
| `--runtime` | 推理运行时类型，如 `tensorrt`、`vllm`、`llamacpp`、`onnx`，`auto` 表示由平台自动选择 |

### 1.6 task — 任务查看

#### 列出任务

```bash
edgectl task list --gateway <gateway-id>
```

#### 查看任务详情

```bash
edgectl task get <task-id>
```

---

## 二、服务启动命令

所有服务均通过 `go run` 或编译后的二进制文件启动。以下环境变量是各服务共用的数据库连接配置。

### 2.1 共用环境变量

```bash
DB_HOST=localhost        # 数据库地址
DB_PORT=5432             # 数据库端口
DB_USER=postgres         # 数据库用户名
DB_PASSWORD=postgres      # 数据库密码
DB_NAME=edgeai           # 数据库名
DB_SSLMODE=disable        # SSL 模式，默认 disable
```

### 2.2 apiserver（Control Plane API 服务）

```bash
DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=postgres DB_NAME=edgeai \
  go run ./cmd/apiserver
```

| 默认端口 | 说明 |
|---------|------|
| `:9090` | gRPC 监听端口 |
| `:8080` | HTTP/JSON（grpc-gateway）监听端口 |

**生产环境额外参数：**

```bash
CA_CERT_PATH=/path/to/ca.crt \
CA_KEY_PATH=/path/to/ca.key \
  go run ./cmd/apiserver
```

> 如果不提供 `CA_CERT_PATH` 和 `CA_KEY_PATH`，apiserver 会自动生成自签名 CA，仅适合开发环境，不适合生产。

### 2.3 controller（Deployment Reconcile 控制器）

```bash
DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=postgres DB_NAME=edgeai \
  go run ./cmd/controller
```

controller 不暴露端口，作为后台 reconcile loop 进程运行，持续监听 `ModelDeployment` CRD/数据库状态变化，生成并下发 Task。

### 2.4 gateway-runtime（区域网关）

```bash
GATEWAY_ID=local-gw \
CONTROL_PLANE_ADDR=localhost:9090 \
DB_HOST=localhost DB_PORT=5432 DB_USER=postgres DB_PASSWORD=postgres DB_NAME=edgeai \
HTTP_ADDR=:8081 \
  go run ./cmd/gateway-runtime
```

| 环境变量 | 必填 | 默认值 | 说明 |
|---------|------|--------|------|
| `GATEWAY_ID` | 是 | 无 | gateway-runtime 实例的运行时标识；通常等于 K8s 节点名（downward API 注入），必须与 bootstrap token 绑定的 gateway 对应 |
| `CONTROL_PLANE_ADDR` | 是 | `localhost:9090` | Control Plane apiserver gRPC 地址（Helm Chart 由 `ai-edge.apiserverAddr` 辅助函数自动渲染成 Service FQDN） |
| `GATEWAY_AUTO_REGISTER` | 否 | `false` | 设为 `true` / `1` / `yes` / `on` 时，启动后向 apiserver 调用 `CreateGateway`，按 gateway NAME 幂等。Helm Chart 默认 `true` |
| `GATEWAY_NAME` | 否 | `GATEWAY_ID` | **仅 debug / 单测使用**。自注册时使用的 gateway NAME；未设置时退回到 `GATEWAY_ID`。Helm Chart **不在生产** 覆盖此 env（多 region 集群会让所有 DaemonSet Pod 用同一 NAME 注册）。 |
| `GATEWAY_REGION` | 否 | `""` | **仅 debug / 单测使用**。自注册时附带的 region 字段。生产路径下 region 由 `edgectl gateway update` 在自注册后补登记，避免 Pod 重启反复覆盖。 |
| `GATEWAY_ENDPOINT` | 否 | `""` | **仅 debug / 单测使用**。自注册时附带的公网 mTLS endpoint（host:port）。生产路径下 endpoint 由 `edgectl gateway update` 在自注册后补登记。 |
| `HTTP_ADDR` | 否 | `:8081` | artifact HTTP、healthz、metrics 监听端口 |
| `GRPC_ADDR` | 否 | `:9443` | gRPC 监听端口 |
| `GATEWAY_TLS_CERT_PATH` | 否 | 无 | mTLS 服务器证书路径 |
| `GATEWAY_TLS_KEY_PATH` | 否 | 无 | mTLS 私钥路径 |
| `GATEWAY_CA_CERT_PATH` | 否 | 无 | mTLS 客户端 CA 证书路径 |
| `CACHE_DIR` | 否 | `./var/lib/gateway-runtime/cache` | 模型制品本地缓存目录；apiserver 分配的 gateway UUID 也会落到该目录下的 `apiserver_gateway_id` |
| `UPSTREAM_BASE_URL` | 否 | `http://localhost:9000` | MinIO/S3 对象存储地址 |
| `IDENTITY_CACHE_TTL` | 否 | `30s` | 节点身份缓存 TTL |
| `TASK_CLAIM_DURATION` | 否 | `5m` | 任务 claim 锁定时长 |
| `CONNECTIVITY_CHECK_INTERVAL` | 否 | `10s` | 云端连通性检测间隔 |
| `CONNECTIVITY_TIMEOUT` | 否 | `5s` | 连通性检测超时 |
| `CLOUD_HEALTH_URL` | 否 | 无 | 云端健康检查 URL，为空则跳过连通性检测 |

> **节点级配置（`GATEWAY_NAME` / `GATEWAY_REGION` / `GATEWAY_ENDPOINT`）的来源 — 不再走 Node Annotation**。
> `gateway-runtime` 是 DaemonSet，每个节点一个 Pod；放在环境变量 / chart values
> 里强制所有 Pod 共用同一组身份，多 region 集群下会出错。旧版本曾用
> `fieldRef: metadata.annotations['edgeai.io/gateway-*']`（K8s 1.27+ Downward
> API）注入，但那只是把 **Pod 自己的** annotation 拉出来——Pod 不会继承
> Node 的 annotation，结果是「沉默地失败」，region / endpoint 全部被吞掉。
>
> 现在的契约是：
>
> - **NAME** 走 `fieldRef: spec.nodeName`（Pod 调度完成后回写到 spec，是合法的 Downward API 路径）。
> - **REGION / ENDPOINT / labels** 在 helm install 完成后用 `edgectl gateway update` 补登记（见 [§ 1.4.3](#143-更新-gateway-业务属性)），属于**操作员控制**的元数据，不会被 Pod 重启反复覆盖。
> - `GATEWAY_NAME` / `GATEWAY_REGION` / `GATEWAY_ENDPOINT` 这三个 env **仅供 debug / 单测**，生产 chart 不应覆盖。
>
> 自注册成功时，gateway-runtime 会在启动日志打印 `apiserver_gateway_id=<uuid> name="<name>"`。
> **运行时仍以 `GATEWAY_ID`（节点名）作为 gateway 标识**——apiserver UUID 仅用于
> 审计 / 关联，下游 dispatcher / task store 不允许用它做归属判断，详见
> [`03-gateway.md` § 4.3.6](./design/03-gateway.md)。

> 如果 `GATEWAY_TLS_CERT_PATH` / `GATEWAY_TLS_KEY_PATH` / `GATEWAY_CA_CERT_PATH` 未完整配置，gateway-runtime 会以**非 mTLS 模式**启动，便于本地开发联调。**生产环境必须配置完整 mTLS。**

### 2.5 edge-agent（边端节点守护进程）

```bash
go run ./cmd/edge-agent --config ./edge-agent.json
```

配置文件示例（`edge-agent.json`）：

```json
{
  "gateway_addr": "localhost:9443",
  "gateway_id": "<gateway-uuid>",
  "gateway_http_addr": "http://localhost:8082",
  "token": "<bootstrap-token-plaintext>",
  "data_dir": "./var/lib/edge-agent",
  "heartbeat_interval": "10s",
  "agent_version": "dev"
}
```

| JSON 字段 | 对应环境变量 | 说明 |
|-----------|------------|------|
| `gateway_addr` | `EDGE_GATEWAY_ADDR` | gateway-runtime gRPC 地址（网络地址） |
| `gateway_id` | `EDGE_GATEWAY_ID` | 逻辑 Gateway ID（bootstrap token 绑定目标） |
| `gateway_http_addr` | `EDGE_GATEWAY_HTTP_ADDR` | gateway-runtime HTTP 地址（拉取制品用） |
| `token` | `EDGE_TOKEN` | Bootstrap Token 明文 |
| `data_dir` | `EDGE_DATA_DIR` | 证书、密钥本地存储目录 |
| `heartbeat_interval` | `EDGE_HEARTBEAT_INTERVAL` | 心跳间隔，支持 `10s`、`1m` 等格式 |

**首次启动流程：**
1. 检查本地是否存在有效证书（`data_dir/node.crt`）
2. 无证书时，使用 bootstrap token 向 gateway 发起 `Bootstrap` RPC，换取正式 mTLS 证书
3. 证书写入 `data_dir/`，后续重启直接加载，不再需要 token

---

## 三、Makefile 常用命令

```bash
# 构建所有服务二进制文件
make build

# 构建单个服务
make build-apiserver
make build-controller
make build-gateway-runtime
make build-edge-agent
make build-edgectl

# 代码质量检查
make vet          # go vet
make lint         # golangci-lint
make test         # go test -race -cover

# Proto 代码生成
make proto        # buf generate

# 数据库迁移（开发环境，Helm Chart 部署时已自动跑过）
make migrate-up   # 执行所有 pending migrations
make migrate-down # 回滚最后一个 migration

# 本地开发依赖（启动 Postgres / MinIO / Prometheus）
make docker-up
make docker-down
```

---

## 四、常见工作流

### 4.1 新节点接入（完整流程）

**第一步：标记节点（DaemonSet 才会调度上去）+ 等待自注册**

```bash
kubectl label node <node-name> node.edgeai.io/role=gateway
```

gateway-runtime Pod 第一次启动时会自动调用 `CreateGateway`，按 NAME（= K8s
节点名）幂等写入一行 `gateways` 记录。**不再**需要给节点打 `edgeai.io/gateway-*`
annotation——旧版本曾用 Node Annotation + K8s 1.27+ 的 Downward API 注入，
但 Downward API 只能读到 Pod 自己的 annotation（Pod 不会继承 Node 的），
那段配置实际上是「沉默地失败」，region / endpoint 全部被吞掉。

> 多 region 集群下 NAME 仍然是节点名（每个节点一行 `gateways`），region
> / endpoint / labels 在第二步统一补登记。

**第二步：（可选）补登记 region / endpoint / labels**

```bash
# NAME 来自第一步的节点名（= spec.nodeName）
edgectl gateway update <node-name> \
    --region cn-east-1 \
    --endpoint gateway-shanghai-01.example.com:9443 \
    --label env=prod --label site=shanghai
```

> 如果 `autoRegister` 被关掉、或要给同一个 control plane 额外挂一个
> region（多 region 共享一个 control plane），先用 `edgectl gateway register`
> 显式创建，再用 `update` 补 region / endpoint。详细字段见 [§ 1.4.1](#141-注册-gateway推荐)
> 和 [§ 1.4.3](#143-更新-gateway-业务属性)。

**第三步：创建 Bootstrap Token**

```bash
GATEWAY_ID=<gateway_id>（从 §1.4.1 / §1.4.2 的 edgectl 输出或 gateway list 中取得）
edgectl token create --gateway $GATEWAY_ID --expires-in 24h --max-uses 10 --description "node-provisioning"
```

复制输出的 `Plaintext` 字段。

**第四步：在节点上准备 edge-agent 配置**

```json
{
  "gateway_addr": "gateway-runtime-grpc-address:9443",
  "gateway_id": "<gateway-id>",
  "gateway_http_addr": "http://gateway-runtime-http-address:8082",
  "token": "<token-plaintext>",
  "data_dir": "/etc/edge-agent",
  "heartbeat_interval": "10s",
  "agent_version": "v1.0.0"
}
```

**第五步：启动 edge-agent**

```bash
go run ./cmd/edge-agent --config /etc/edge-agent/edge-agent.json
```

或使用一键脚本：

```bash
curl -sL https://raw.githubusercontent.com/tangming1996/ai-edge/main/manifests/scripts/install-edge-agent.sh | \
    GATEWAY_ID="$GATEWAY_ID" \
    GATEWAY_ADDR=ai-edge-gateway-runtime.edgeai-system.svc.cluster.local:9443 \
    TOKEN="$TOKEN_PLAINTEXT" \
    bash
```

**第六步：验证节点接入**

```bash
edgectl node list --gateway $GATEWAY_ID
```

看到节点 `ONLINE=true` 即接入成功。

### 4.2 节点身份吊销

```bash
# 查出节点 ID
edgectl node list --gateway <gateway-id>

# 吊销
edgectl node revoke <node-id>
```

吊销后节点会立即收到 `IdentityRevoked` 错误，停止正常工作。

### 4.3 本地 smoke test（最小链路验证）

```bash
# 启动所有依赖（Postgres / MinIO / Prometheus）
make docker-up

# 本地直接跑 apiserver 时，schema 需要手工迁移
make migrate-up

# 构建并启动 apiserver（后台）
go run ./cmd/apiserver &

# 跑 smoke 测试（已改为通过 edgectl gateway register 创建 gateway）
./scripts/smoke-test.sh
```

> 走 Helm / 生产环境时 `helm install` 已经自动跑过 `migrate up`，并且
> gateway-runtime 启动时会自调用 `edgectl gateway register` 写一行 gateway。
> 上面这两步**只在裸跑 apiserver / gateway-runtime** 时才需要。
