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

### 1.4 deployment — 模型部署管理

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

### 1.5 task — 任务查看

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
| `GATEWAY_ID` | 是 | 无 | 逻辑 Gateway ID，需与数据库 `gateways` 表中记录一致 |
| `CONTROL_PLANE_ADDR` | 是 | `localhost:9090` | Control Plane apiserver gRPC 地址 |
| `HTTP_ADDR` | 否 | `:8081` | artifact HTTP、healthz、metrics 监听端口 |
| `GRPC_ADDR` | 否 | `:9443` | gRPC 监听端口 |
| `GATEWAY_TLS_CERT_PATH` | 否 | 无 | mTLS 服务器证书路径 |
| `GATEWAY_TLS_KEY_PATH` | 否 | 无 | mTLS 私钥路径 |
| `GATEWAY_CA_CERT_PATH` | 否 | 无 | mTLS 客户端 CA 证书路径 |
| `CACHE_DIR` | 否 | `./var/lib/gateway-runtime/cache` | 模型制品本地缓存目录 |
| `UPSTREAM_BASE_URL` | 否 | `http://localhost:9000` | MinIO/S3 对象存储地址 |
| `IDENTITY_CACHE_TTL` | 否 | `30s` | 节点身份缓存 TTL |
| `TASK_CLAIM_DURATION` | 否 | `5m` | 任务 claim 锁定时长 |
| `CONNECTIVITY_CHECK_INTERVAL` | 否 | `10s` | 云端连通性检测间隔 |
| `CONNECTIVITY_TIMEOUT` | 否 | `5s` | 连通性检测超时 |
| `CLOUD_HEALTH_URL` | 否 | 无 | 云端健康检查 URL，为空则跳过连通性检测 |

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

# 数据库迁移
make migrate-up   # 执行所有 pending migrations
make migrate-down # 回滚最后一个 migration

# 本地开发依赖（启动 Postgres / MinIO / Prometheus）
make docker-up
make docker-down
```

---

## 四、常见工作流

### 4.1 新节点接入（完整流程）

**第一步：在 Control Plane 创建 Gateway（如果尚未创建）**

```bash
docker compose exec postgres psql -U postgres -d edgeai -c \
  "INSERT INTO gateways (name, region, labels, endpoint, status) \
   VALUES ('local-gw', 'local', '{}', 'grpc://localhost:9443', 'Active') RETURNING id;"
```

**第二步：创建 Bootstrap Token**

```bash
GATEWAY_ID=<上一步返回的gateway-id>
edgectl token create --gateway $GATEWAY_ID --expires-in 24h --max-uses 10 --description "node-provisioning"
```

复制输出的 `Plaintext` 字段。

**第三步：在节点上准备 edge-agent 配置**

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

**第四步：启动 edge-agent**

```bash
go run ./cmd/edge-agent --config /etc/edge-agent/edge-agent.json
```

**第五步：验证节点接入**

```bash
edgectl node list --gateway <gateway-id>
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
# 启动所有依赖
make docker-up

# 执行数据库迁移
make migrate-up

# 构建并启动 apiserver（后台）
go run ./cmd/apiserver &

# 运行 smoke 测试
./scripts/smoke-test.sh
```
