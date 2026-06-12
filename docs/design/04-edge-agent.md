# Edge Agent 详细设计

## 1. 设计目标

`edge-agent` 是部署在边缘设备上的轻量执行代理。

它的目标不是把边缘设备变成 Kubernetes Node，而是负责：

```text
安全接入
任务拉取
任务执行
状态上报
模型下载
Runtime 管理
Agent 自升级
```

***

## 2. 模块组成

建议内部结构：

```text
edge-agent

├── bootstrap
├── identity
├── heartbeat
├── downloader
├── runtime-manager
├── metrics
├── task-runner
└── updater
```

***

## 3. 模块职责

## 3.1 bootstrap

负责：

```text
读取 bootstrapToken 与内置 caBundle
生成密钥对
生成 CSR
以服务端 TLS（用 caBundle 校验 Gateway）调用 NodeOnboardingService/Bootstrap
接收 nodeID 与证书
```

说明：

- `Bootstrap` 阶段尚无客户端证书，连接是**服务端 TLS**，不是 mTLS
- 用安装包内置的 `caBundle` 校验 Gateway 服务端证书，避免 TOFU
- 请求经 Gateway 转发到 Control Plane 完成校验与签发（Gateway 不签发）

## 3.2 identity

负责：

```text
加载 node.key / node.crt / ca.crt
管理本地身份状态
触发证书续签
提供 mTLS 客户端能力
```

## 3.3 heartbeat

负责：

```text
周期性上报在线状态
上报 agentVersion
上报 lastSeen
```

## 3.4 downloader

负责：

```text
模型 / Agent 包下载（走 Gateway 制品文件端点，HTTP Range，断点续传）
sha256 校验
signature 校验
校验通过后才落盘入缓存
```

说明：

- 制品走独立文件通道（见 `03-gateway.md` 9.2 与 `06-model-registry-runtime.md`），不走 gRPC
- 用与 gRPC 相同的 mTLS 客户端身份访问制品端点
- 下载中断的文件先落临时区，校验通过后再原子移入 `cache/`

## 3.5 runtime-manager

负责：

```text
启动 Runtime
停止 Runtime
升级 Runtime
回收 Runtime
加载模型
```

## 3.6 metrics

负责：

```text
采集 CPU
采集 Memory
采集 GPU
采集 Runtime 专属指标
```

## 3.7 task-runner

负责：

```text
拉取任务
执行任务
状态回写
失败重试
结果上报
```

## 3.8 updater

负责：

```text
下载 Agent 升级包
校验签名
校验版本策略
执行替换与重启
```

***

## 4. 本地目录布局

建议目录：

```text
/etc/edge-agent/
  config.yaml
  node.key
  node.crt
  ca.crt

/var/lib/edge-agent/
  tasks/
  cache/
  runtime/
  state/

/var/log/edge-agent/
  agent.log
```

说明：

- `/etc/edge-agent` 放配置与身份材料
- `/var/lib/edge-agent` 放运行期状态与缓存
- `/var/log/edge-agent` 放日志

进一步约定：

- `cache/` 保存模型与升级包本地副本
- `tasks/` 保存任务工作目录与任务结果快照
- `runtime/` 保存本机 Runtime 运行数据
- `state/` 保存重启恢复与断网补传所需状态

V1 不要求 `Edge` 运行独立数据库服务。

推荐：

```text
本地磁盘
+
状态文件
```

必要时可以增加：

```text
SQLite
```

用于保存更稳定的本地恢复状态，但不是强制要求。

***

## 5. 启动流程

## 5.1 首次启动

```text
加载 config
  ↓
检测本地证书是否存在
  ↓
不存在则进入 bootstrap
  ↓
生成 key / csr
  ↓
向 Gateway 注册
  ↓
保存证书
  ↓
启动 heartbeat / metrics / task-runner
```

## 5.2 常规启动

```text
加载本地身份材料
  ↓
建立 mTLS 客户端
  ↓
启动任务拉取
  ↓
启动状态上报
  ↓
启动续签检查
```

***

## 6. 配置设计

基础配置建议：

```yaml
gateway: gw-shanghai
gatewayEndpoint: dns:///gw-shanghai.edge.svc.cluster.local:8443
artifactEndpoint: https://gw-shanghai.edge.svc.cluster.local:8443
bootstrapToken: bt_xxx
caBundle: /etc/edge-agent/ca.crt

heartbeatInterval: 10s
metricsInterval: 15s
taskPollInterval: 5s

dataDir: /var/lib/edge-agent
runtimeDir: /var/lib/edge-agent/runtime
cacheDir: /var/lib/edge-agent/cache
```

原则：

- 首次注册后 `bootstrapToken` 可清除或标记失效
- 本地保留尽量少的长期敏感信息
- 关键路径配置保持显式可见
- 与 Gateway 的协议优先使用 `proto + gRPC`
- 本地状态以“可恢复”为目标，而不是做完整本地控制面

***

## 7. 任务执行模型

V1 使用：

```text
单机拉取
本地串行执行为主
必要时支持有限并发
通过 gRPC unary 拉取与回报
```

原因：

- 降低任务冲突
- 简化状态管理
- 降低本地资源竞争

典型任务：

```text
InstallModel
DeleteModel
StartRuntime
StopRuntime
RestartRuntime
UpgradeRuntime
UpgradeAgent
CollectLogs
```

执行要求：

- 每个任务有本地工作目录
- 每个任务结果必须可回传
- 任务中断后支持恢复或明确失败

本地至少保留：

```text
任务当前阶段
任务临时文件路径
最近一次结果快照
待重试上报队列
```

***

## 8. Runtime 管理设计

Agent 不暴露底层 Runtime 细节给用户，但内部必须能够适配：

```text
vLLM
TensorRT
llama.cpp
ONNX Runtime
```

`runtime-manager` 对外统一提供：

```text
prepare
start
stop
restart
status
cleanup
```

V1 采用适配器模式：

```text
RuntimeManager
  ↓
RuntimeAdapter
  ├── TensorRTAdapter
  ├── VLLMAdapter
  ├── LlamaCppAdapter
  └── ONNXAdapter
```

这样可以保证上层任务语义稳定。

Runtime 解析归属：

- `runtime: auto` 的解析（依据 `RuntimeProfile`）发生在**上游**（控制面下发任务时，或 Gateway 拆分 NodeTask 时）
- Edge 不反查 `RuntimeProfile` CRD，NodeTask payload 里直接带解析后的具体 runtime 与必要参数
- 这样 Edge 只依赖任务内容即可执行，不依赖对控制面对象的实时访问

***

## 9. 状态上报设计

Agent 上报三类信息：

### 9.1 NodeState

```json
{
  "online": true,
  "agentVersion": "1.0.0",
  "lastSeen": "..."
}
```

### 9.2 Metrics

```json
{
  "cpuUsage": 35,
  "memoryUsage": 50,
  "gpuUsage": 80
}
```

### 9.3 RuntimeState

```json
{
  "loadedModels": [
    {
      "name": "qwen3-8b",
      "version": "v1"
    }
  ]
}
```

原则：

- 设备静态信息不高频上报
- 状态与指标分离
- Runtime 专属数据单独上报

### 9.4 断网与重启恢复

当 `Edge` 与 `Gateway` 短暂失联时：

```text
继续本地运行已启动 Runtime
继续使用本地模型缓存
把待上报状态落到 state/
连接恢复后补传
```

当 `edge-agent` 重启时：

```text
从 state/ 恢复本地任务快照
恢复待重试上报队列
重新扫描 runtime/ 与 cache/
```

V1 不要求 Edge 在断网时接受新的全局调度，只要求：

```text
本机继续运行
本机状态可恢复
恢复后可补传
```

***

## 10. 证书续签

`identity` 模块负责证书续签。

规则：

```text
证书有效期 90 天
到期前 30 天开始续签
```

流程：

```text
检查 expireAt
  ↓
生成续签 CSR
  ↓
调用 NodeOnboardingService/Renew
  ↓
收到新证书
  ↓
替换本地证书
```

要求：

- 续签失败时继续使用旧证书，直到旧证书过期
- 不重新使用 Bootstrap Token
- 替换证书过程尽量无损

***

## 11. 升级设计

升级流程：

```text
收到 UpgradeAgentTask
  ↓
下载 agent.tar.gz / sha256 / sig / manifest
  ↓
校验 hash
  ↓
校验 signature
  ↓
校验 minAllowedVersion
  ↓
替换二进制
  ↓
systemd restart edge-agent
```

升级要求：

- 必须防回滚
- 必须记录升级结果
- 必须可定位失败原因

V1 可以不做双分区升级，但必须保留失败日志。

***

## 12. 异常处理

### 12.1 Gateway 不可达

行为：

```text
保留本地运行中的 Runtime
延迟重试连接
缓存待上报状态
```

### 12.2 任务执行失败

行为：

```text
回写失败状态
保留错误日志
按任务策略重试
```

### 12.3 下载失败

行为：

```text
删除不完整文件
避免污染缓存
重试或回报失败
```

### 12.4 证书失效

行为：

```text
停止正常业务通信
进入身份恢复或人工干预流程
```

***

## 13. V1 非目标

V1 暂不实现：

- 复杂插件系统
- 多 Agent 进程协同
- 节点内复杂作业调度器
- 长连接双向流控
- 自动修复所有本地环境问题

***

## 14. 后续细化项

需要继续补充：

- 本地状态文件格式
- 任务工作目录结构
- Runtime Adapter 接口定义
- systemd 服务文件
- 错误码与退出码
