## ADDED Requirements

### Requirement: Agent 组成与安装
`edge-agent` SHALL 由 bootstrap、identity、heartbeat、downloader、runtime-manager、metrics、task-runner、updater 子模块组成，以单二进制 + systemd 方式安装（`curl install.sh | bash -s -- --gateway <gw> --token <bt>`），MUST NOT 以 Kubernetes DaemonSet/Node 方式接入。

#### Scenario: 通过安装脚本接入
- **WHEN** 管理员在边缘设备执行安装脚本并传入 gateway 与 bootstrap token
- **THEN** Agent 写入本地配置并由 systemd 启动，进入首次注册流程

### Requirement: 首次注册与本地身份
Agent SHALL 在首次启动时本地生成密钥对与 CSR，调用 `NodeOnboardingService/Bootstrap` 完成注册，并将 `node.key`/`node.crt`/`ca.crt` 保存至本地（如 `/etc/edge-agent/`）。注册成功后 SHALL 切换为 mTLS 通信并废弃 Bootstrap Token。

#### Scenario: 注册后切换 mTLS
- **WHEN** Agent 完成 Bootstrap 并收到证书
- **THEN** 后续所有 `AgentService` 调用使用 mTLS 客户端证书，不再使用 token

### Requirement: 心跳与状态上报
Agent SHALL 周期性调用 `ReportHeartbeat`（建议约 10 秒）、`ReportMetrics` 与 `ReportRuntimeState` 上报设备状态、资源指标与运行时状态。指标 SHALL 进入时序系统而非主库。

#### Scenario: 周期心跳维持在线状态
- **WHEN** Agent 正常运行
- **THEN** 按周期上报心跳，Control Plane 据此更新 `last_seen_at` 与在线状态

### Requirement: 任务拉取与执行
Agent SHALL 通过轮询式 `PullTasks` 拉取 NodeTask（不使用 server streaming），由 task-runner 执行并通过 `ReportTaskResult` 回写结果。Agent SHALL 按 `taskID` 本地去重，对已执行任务返回既有状态。

#### Scenario: 拉取并执行安装任务
- **WHEN** Agent 拉取到一个 `InstallModel` NodeTask
- **THEN** 经制品文件通道下载并校验后执行，再回传结果
- **AND** 同一 taskID 再次出现时不重复执行

### Requirement: 本机恢复现场
Agent SHALL 在本地维护模型缓存、任务工作目录、待补传状态与本机 Runtime 恢复现场（文件目录，必要时 SQLite），用于重启恢复、断网自治与升级中断恢复。该本地状态属于恢复现场，MUST NOT 作为中心数据库延伸。

#### Scenario: 重启后恢复执行现场
- **WHEN** Agent 在任务执行中被重启
- **THEN** 依据本地恢复现场重建未完成任务与运行态，不丢失已完成的执行结果

### Requirement: 边缘自治推理
当 Edge 与 Gateway 失联时，Agent SHALL 依赖本地缓存模型与本地 Runtime 继续提供推理，并缓冲待补传状态。

#### Scenario: 失联期继续推理
- **WHEN** Edge 与 Gateway 网络中断
- **THEN** 已加载模型继续对外提供推理，状态与结果在本地缓冲待恢复后补传

### Requirement: Agent 安全升级
`UpgradeAgent` 任务的执行 SHALL 校验 `sha256` 与签名（cosign 或 ed25519）后再 systemd 重启，并依据 release manifest（含 `minAllowedVersion`）拒绝低于安全基线的回滚版本。

#### Scenario: 拒绝不安全旧版本
- **WHEN** 下发的 Agent 版本低于 release manifest 的 `minAllowedVersion`
- **THEN** Agent 拒绝升级该版本

#### Scenario: 签名校验失败中止升级
- **WHEN** 下载的 agent 包签名校验失败
- **THEN** 不执行 systemd 重启，任务以不可恢复错误失败
