## ADDED Requirements

### Requirement: Bootstrap Token 生命周期
系统 SHALL 支持管理员创建一次性接入用 Bootstrap Token，绑定目标 Gateway、过期时间、最大使用次数与标签。Token 明文 MUST NOT 落库，仅存储 `SHA256(token)`。Token 校验与 `used_count` 更新 SHALL 只在 Control Plane 针对云端主库执行。

#### Scenario: 创建并下发 Token
- **WHEN** 管理员调用 `CreateBootstrapToken` 指定 gateway、expires_in、max_uses
- **THEN** 系统生成一次性 token 串返回给管理员
- **AND** 主库仅保存其 SHA256 哈希、过期时间与限额

#### Scenario: 校验过期或超限 Token
- **WHEN** Agent 使用已过期或 `used_count >= max_uses` 的 token 注册
- **THEN** 返回 `TOKEN_EXPIRED` 或 `TOKEN_EXHAUSTED` 错误，拒绝签发证书

### Requirement: 节点首次注册与证书签发
Agent 首次接入 SHALL 本地生成密钥对（建议 ECDSA P256）与 CSR，通过 `NodeOnboardingService/Bootstrap`（服务端 TLS + Bootstrap Token）发起注册。`Bootstrap` 的实现 SHALL 在 Control Plane，`gateway-runtime` 仅作接入转发，不读写 Token/Identity 主表、不持有 CA 私钥。证书 SHALL 由 Control Plane 内置 Signer 用对应区域 Intermediate CA 签发。

#### Scenario: 成功注册并返回身份
- **WHEN** Agent 提交合法 token + CSR + serial + hardware
- **THEN** Control Plane 在单事务内校验 token、自增 used_count、创建 EdgeIdentity 与 EdgeNode
- **AND** 返回 `node_id`、`certificate_pem`、`ca_pem`、`expire_at`

#### Scenario: Gateway 不匹配拒绝注册
- **WHEN** Token 绑定 `gw-shanghai`，但请求试图注册到其他 Gateway
- **THEN** 返回 `GATEWAY_MISMATCH` 错误

### Requirement: 节点主标识唯一绑定
系统 SHALL 选择稳定主标识（建议 `serial`）作为注册唯一性约束。首次注册成功后 `serial` 与 `nodeID` 绑定，后续 MUST NOT 被其他 nodeID 复用；重复注册 MUST 进入续签或显式重置流程，以防镜像克隆、重复抢注与身份漂移。

#### Scenario: 镜像克隆被拒绝
- **WHEN** 一个已绑定 `serial` 的设备被克隆后以同一 serial 重新 Bootstrap
- **THEN** 返回 `IDENTITY_CONFLICT`，不签发新的并行有效身份

### Requirement: CA 层级与私钥隔离
平台 SHALL 维护单一 Root CA，每区域一个 Intermediate CA。所有 Intermediate CA 私钥 MUST 仅保存在 Control Plane Signer，MUST NOT 下发到任何 `gateway-runtime` 实例。

#### Scenario: 区域失陷范围受限
- **WHEN** 某区域 Intermediate CA 失陷
- **THEN** 影响仅局限于该区域，其证书链仍带区域 Intermediate 以保证信任隔离

### Requirement: mTLS 通信与逐请求身份校验
首次注册成功后 Bootstrap Token SHALL 立即废弃，后续通信统一使用 mTLS 客户端证书。系统 SHALL 复用 mTLS 长连接但在每个请求上校验 certificate fingerprint、identity status（命中本地/区域短 TTL 缓存）与 gateway binding，而不仅在握手时校验。

#### Scenario: 每次请求校验身份状态
- **WHEN** 已建立的长连接发送一次 heartbeat/metrics/pull 请求
- **THEN** 服务在请求级再次校验指纹、身份状态与 gateway 绑定

### Requirement: 证书续签
节点证书 SHALL 设有限有效期（建议 90 天），Agent SHALL 在到期前（建议 30 天）通过 `NodeOnboardingService/Renew`（走现有 mTLS 身份）自动续签。

#### Scenario: 临期自动续签
- **WHEN** 证书剩余有效期低于续签阈值
- **THEN** Agent 提交新 CSR 经 Renew 获取新证书，旧身份平滑切换

### Requirement: 节点吊销
管理员 SHALL 能吊销节点身份；吊销事件 SHALL 经 `NotifyIdentityEvent` 推送到 `gateway-runtime` 刷新本地 revoked/identity 缓存。吊销生效点是事件到达或缓存短 TTL 到期后的下一次请求鉴权，不依赖重新握手，也不依赖 OCSP/大规模 CRL。

#### Scenario: 吊销后请求立即失效
- **WHEN** 管理员吊销 `node-001` 且事件已推送或缓存 TTL 已过期
- **THEN** 该节点的下一次请求鉴权失败，返回 `IDENTITY_REVOKED`
