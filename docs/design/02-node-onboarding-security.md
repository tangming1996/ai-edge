# Node Onboarding & Security 详细设计

## 1. 设计目标

Node Onboarding 的核心目标不是“让 Agent 能安装起来”，而是：

```text
让节点安全接入
为节点签发可管理身份
让后续所有通信默认具备认证能力
支持续签、吊销和升级安全
```

V1 安全模型固定为：

```text
Bootstrap Token
+
Mutual TLS
+
Node Identity
```

***

## 2. 设计范围

本模块负责：

```text
BootstrapToken 创建与校验
首次注册
CSR 签发
EdgeIdentity 创建
证书轮换
节点吊销
Agent 升级校验
```

不负责：

```text
TPM 远程证明
Secure Boot 统一管理
硬件可信根编排
企业级 PKI 平台
```

这些能力可以在后续企业版演进。

***

## 3. V1 约束

为了保证安全边界清晰，同时控制复杂度，V1 固定以下约束：

```text
平台持有单一 Root CA
每区域一个 Intermediate CA，私钥只在 Control Plane Signer
Bootstrap Token 校验与证书签发统一由 Control Plane 完成
Gateway 只做 Bootstrap / Renew 的接入代理与 mTLS 终止，不持有 CA 私钥
Bootstrap Token 只用于首次接入
后续通信复用 mTLS 长连接，逐请求检查 identity 状态
节点主标识首次绑定后不可漂移
```

归属说明：保留区域 Intermediate CA 以维持“区域失陷只影响该区域”的信任隔离；但签名集中在云端，避免向大量 `gateway-runtime` 分发 CA 私钥。

***

## 4. 核心对象

## 4.1 BootstrapToken

示例：

```yaml
kind: BootstrapToken

metadata:
  name: factory-a

spec:
  gateway: gw-shanghai
  expiresIn: 24h
  maxUses: 100
  labels:
    region: shanghai
    factory: factory-a
```

用途：

```text
首次安装授权
区域归属限定
批次接入控制
```

设计要求：

- 不存储明文 Token
- 数据库存储 `SHA256(token)`
- 支持过期、冻结、作废
- 支持使用次数上限

推荐表结构：

```sql
bootstrap_tokens

id
token_hash
gateway_id
max_uses
used_count
expire_at
status
created_at
updated_at
```

## 4.2 EdgeIdentity

示例：

```yaml
kind: EdgeIdentity

spec:
  nodeID: node-6f98d7a1
  gateway: gw-shanghai
  serial: SN123456
  certFingerprint: xxx
  status: Active
  issuedAt: "2026-06-10T10:00:00Z"
  expireAt: "2026-09-08T10:00:00Z"
```

职责：

```text
节点身份记录
证书状态记录
生命周期追踪
吊销依据
```

推荐表结构：

```sql
edge_identities

id
node_id
serial
fingerprint
gateway_id
status
issued_at
expire_at
revoked_at
last_seen_at
created_at
updated_at
```

状态建议：

```text
Pending
Active
Revoked
Expired
Suspended
```

***

## 5. 首次接入流程

### Step 1. 管理员创建 BootstrapToken

Control Plane 为指定区域生成接入令牌。

输出：

```text
bt_xxx
```

### Step 2. 安装 Agent

安装命令：

```bash
curl https://gateway/install.sh | bash -s -- \
  --gateway gw-shanghai \
  --token bt_xxx
```

Agent 持久化初始配置，并随安装包获取一份 CA 信任材料用于首次连接：

```yaml
gateway: gw-shanghai
bootstrapToken: bt_xxx
caBundle: /etc/edge-agent/ca.crt   # 安装包内置，用于校验 Gateway 服务端证书
```

首次信任根说明：

- `Bootstrap` 阶段 Agent 还没有客户端证书，该请求只能是**服务端 TLS**（不是 mTLS）
- Agent 用安装包内置的 `caBundle`（区域 Intermediate + Root）校验 Gateway 服务端证书，避免 TOFU
- 安装包与 `caBundle` 的分发渠道应是可信渠道（内网制品库 / 带签名的安装器），不依赖明文 `curl | bash` 的传输安全

### Step 3. Agent 生成密钥对

推荐：

```text
ECDSA P256
```

生成：

```text
node.key
node.csr
```

### Step 4. Agent 发起注册

接口（由 Gateway 接入并转发到 Control Plane `NodeOnboardingService`）：

```text
rpc Bootstrap(BootstrapRequest) returns (BootstrapResponse)
```

请求消息：

```text
token
csr
hostname
serial
hardware
```

### Step 5. Control Plane 校验

由 Control Plane（非 Gateway）在云端主库的一个事务内校验：

```text
token_hash 是否匹配
token 是否过期
used_count 是否超限
接入区域是否匹配
serial 是否已绑定其他 nodeID
```

Gateway 在这一步只做转发与基础限流，不读写 Token / Identity 主表。

### Step 6. 签发身份

校验通过后，由 Control Plane 内置 `Signer` 在同一事务内：

```text
生成 nodeID
用对应区域 Intermediate CA 签发 client certificate
创建 EdgeIdentity
更新 used_count
```

返回消息：

```text
nodeID
certificate
ca
expireAt
```

### Step 7. 切换到 mTLS

Bootstrap Token 在注册成功后立即作废。

后续所有接口只接受：

```text
client certificate
```

***

## 6. 注册唯一性设计

V1 必须至少选择一个稳定主标识。

建议优先：

```text
serial
```

约束：

- `serial` 首次绑定成功后，不允许被其他 `nodeID` 复用
- 同一设备再次注册时，必须进入“续签”或“显式重置”流程
- 不能把“重复 bootstrap”当作正常更新手段

这样可以控制：

```text
镜像克隆
重复抢注
身份漂移
```

***

## 7. CA 层级设计

V1 推荐 PKI 层级：

```text
Platform Root CA
  ↓
Region Intermediate CA（每区域一个，私钥只在 Control Plane Signer）
  ↓
Node Client Certificate（由 Control Plane Signer 签发）
```

设计原因：

- 平台级 Root CA 离线保管，不暴露到任何在线服务
- 证书链仍带区域 Intermediate，区域信任隔离成立：某区域 Intermediate 失陷只影响该区域
- CA 私钥集中在 Control Plane，不下发任何 `gateway-runtime`，私钥泄露面最小
- 证书校验链简单，可控且便于实现

证书中至少应包含：

```text
nodeID
gateway
serial
```

服务端校验时同时检查：

```text
证书链
证书指纹
identity 状态
gateway 归属
```

***

## 8. 后续通信模型

V1 统一采用：

```text
复用 mTLS 长连接（连接级证书链校验）
逐请求做身份状态校验（不只在握手时）
gRPC unary 为主
```

对应 RPC 包括：

```text
ReportHeartbeat
ReportMetrics
PullTasks
ReportTaskResult
```

连接建立时完成 mTLS 握手；之后**每个请求**都额外检查：

```text
EdgeIdentity 状态（命中 gateway-runtime 本地缓存，短 TTL）
certificate fingerprint
Gateway 归属
```

为什么不“每请求重新握手”：

- 在数万节点、心跳/轮询高频的场景下，每请求全量 TLS 握手 + 证书链校验 + 查库不可扩展
- 复用长连接 + 逐请求查身份状态缓存，既能扩展又能保证吊销及时
- 吊销通过 `NotifyIdentityEvent` 主动推送 + 缓存短 TTL 兜底，不依赖重新握手

好处：

- 吊销语义清晰（事件到达或 TTL 到期后下一次请求即失效）
- 不需要实现复杂的长连接会话回收 / OCSP / 大规模 CRL
- 与工业现场不稳定网络更兼容

***

## 9. 证书轮换

不要使用永久证书。

推荐：

```text
有效期 90 天
到期前 30 天自动续签
```

流程（Renew 同样经 Gateway 接入、转发到 Control Plane）：

```text
Agent
  ↓
Generate Renew CSR（用现有 mTLS 身份发起）
  ↓
Gateway 转发到 Control Plane
  ↓
Control Plane Verify EdgeIdentity（须 Active）
  ↓
Signer Issue New Cert
  ↓
Agent Hot Reload / Replace Local Cert
```

续签要求：

- 续签基于当前有效身份，不重新使用 Bootstrap Token
- 续签前校验原证书状态与 `EdgeIdentity`
- 新证书签发成功后记录新的 `fingerprint`
- 续签依赖 Control Plane Signer 在线；由于证书 90 天有效、到期前 30 天开始续签，只要云端在该窗口内恢复即可，区域短暂断网不影响存量证书继续可用

本地文件：

```text
/etc/edge-agent/node.key
/etc/edge-agent/node.crt
/etc/edge-agent/ca.crt
```

***

## 10. 节点吊销

管理入口：

```bash
edgectl revoke node-001
```

吊销动作：

```text
将 EdgeIdentity 状态改为 Revoked
记录 revoked_at
加入 revoked identities 列表
```

V1 不强调复杂 `CRL/OCSP`，先以服务端在线校验为主。

生效语义：

```text
新请求立即失效
```

说明：

- Control Plane 通过 `NotifyIdentityEvent` 把吊销事件推送到 `gateway-runtime`，实例更新本地 revoked / identity 缓存
- 生效点是事件到达或缓存短 TTL 到期后的下一次请求鉴权
- 该语义与“复用长连接 + 逐请求身份校验”的通信模型一致

***

## 11. 升级安全

升级任务：

```yaml
kind: UpgradeAgentTask

spec:
  version: 1.2.0
```

Agent 下载：

```text
agent.tar.gz
agent.sha256
agent.sig
release.manifest
```

校验项：

```text
sha256
signature
version policy
```

签名建议：

```text
cosign
```

或者：

```text
ed25519
```

`release.manifest` 至少包含：

```text
version
sha256
signature
minAllowedVersion
```

Agent 必须拒绝：

```text
低于安全基线的回滚版本
```

验证通过后执行：

```text
systemd restart edge-agent
```

***

## 12. 失败场景与处理

### 12.1 Token 失效

返回：

```text
401 / 403
```

Agent 不自动重试无限次，避免空转。

### 12.2 重复注册

若 `serial` 已被其他 `nodeID` 绑定：

```text
拒绝注册
```

并要求进入显式重置流程。

### 12.3 续签失败

若原证书仍有效：

```text
继续使用旧证书通信
```

并在后台重试。

### 12.4 吊销后请求

服务端直接拒绝：

```text
401 / 403
```

并记录审计日志。

***

## 13. V1 非目标

V1 暂不实现：

- TPM Remote Attestation
- Secure Boot 集成
- 硬件指纹多维交叉校验
- 离线 CRL 大规模同步
- 多级 CA 自动轮转平台

***

## 14. 后续细化项

需要继续补充：

- `Bootstrap` / `Renew` proto 字段与错误码
- `Revoke` 管理 RPC 设计
- 证书主题与 SAN 规范
- 安装脚本细节
- 显式重置流程
