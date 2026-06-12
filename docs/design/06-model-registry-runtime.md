# Model Registry & Runtime 详细设计

## 1. 设计目标

该模块包含两部分能力：

```text
Model Registry
Runtime Abstraction
```

它解决的问题是：

```text
模型如何统一管理
模型如何安全分发
模型如何在异构设备上以统一方式运行
```

***

## 2. 设计范围

本模块负责：

```text
模型元数据管理
Artifact 存储
版本管理
校验与签名信息管理
RuntimeProfile 管理
Runtime 自动映射
缓存策略定义
```

不负责：

```text
训练流程
微调流水线
在线推理协议网关
完整模型生命周期 MLOps 平台
```

***

## 3. Model Registry 设计

## 3.1 目标

`Model Registry` 负责统一管理模型制品。

V1 重点不是做一个复杂仓库产品，而是保证：

```text
模型可上传
版本可追踪
文件可校验
部署可引用
```

## 3.2 Artifact 类型

V1 支持：

```text
GGUF
TensorRT
ONNX
Safetensors
```

必要时可扩展：

```text
tokenizer
config
runtime plugin
```

## 3.3 元数据结构

示例：

```yaml
kind: Model

spec:
  name: qwen3-8b
  version: v1.0.0
  format: gguf
  checksum: xxx
  size: 8GB
```

建议扩展字段：

```text
artifactURI
signatureURI
framework
precision
supportedRuntime
labels
```

## 3.4 存储设计

推荐：

```text
对象存储 + 元数据数据库
```

例如：

```text
MinIO + PostgreSQL
```

原因：

- 足够轻量
- 易于实现
- 便于被 Gateway / Edge 下载

***

## 4. 模型版本策略

建议版本规则：

```text
name + version 唯一
checksum 不可变
已发布版本默认不可覆盖
```

状态建议：

```text
Draft
Published
Deprecated
Archived
```

V1 不做复杂审批流，但必须避免同名版本被覆盖。

***

## 5. 校验与签名

每个模型 Artifact 至少需要：

```text
model file
sha256
signature
```

下载顺序建议：

```text
download artifact
  ↓
verify sha256
  ↓
verify signature
  ↓
move into cache
```

签名建议：

```text
cosign
```

或者：

```text
ed25519
```

原则：

- 未通过校验的文件不得进入缓存
- 校验失败必须保留错误信息
- Control Plane 必须记录签名元数据

***

## 6. Runtime 抽象设计

平台不希望用户直接操作底层 Runtime。

对用户暴露的统一语义是：

```yaml
runtime: auto
```

内部通过 `RuntimeProfile` 与 `Runtime Mapping` 选择实际 Runtime。

## 6.1 RuntimeProfile

示例：

```yaml
kind: RuntimeProfile

spec:
  selector:
    gpu: orin
  runtime: tensorrt
```

职责：

```text
根据设备能力选择推荐 Runtime
定义兼容关系
避免用户手工绑定复杂底层细节
```

## 6.2 Runtime Mapping

示例：

```text
Orin
  ↓
TensorRT

A100
  ↓
vLLM

CPU
  ↓
llama.cpp
```

V1 先做静态规则映射，不做复杂动态调优。

解析位置：`runtime: auto` 在生成 `NodeTask` 时由上游（控制面或 Gateway 拆分阶段）解析为具体 runtime，并写入 `NodeTask.payload`；Edge 不在运行时反查 `RuntimeProfile`。

***

## 7. 部署引用模型

`ModelDeployment` 不直接引用底层文件路径，而是引用：

```text
model name
model version
runtime profile
target gateway / labels
```

这样可以把：

```text
文件位置
缓存位置
具体 Runtime 选择
```

从用户声明中抽离出去。

***

## 8. 缓存层级设计

缓存链路：

```text
Cloud Registry
  ↓
Gateway Cache
  ↓
Edge Cache
```

默认策略：

```text
Gateway 保留最近 10 版本
Edge 保留最近 3 版本
淘汰使用 LRU
```

缓存元数据：

```text
model
version
checksum
size
path
last_access_at
ref_count
```

说明：

- `Gateway` 缓存解决大规模回源问题
- `Edge` 缓存解决重启与断网问题

***

## 9. 下载与分发流程

### 9.1 Cloud 到 Gateway

```text
Gateway 收到部署任务
  ↓
检查本地缓存是否命中
  ↓
未命中则从 Registry 下载
  ↓
校验 hash / signature
  ↓
写入区域缓存
```

### 9.2 Gateway 到 Edge

```text
Edge 收到 InstallModel 任务
  ↓
通过 Gateway 制品文件端点下载（HTTP Range，断点续传）
  ↓
校验 hash / signature
  ↓
写入本地缓存
```

V1 默认 Edge 不直接回源 Registry，尽量通过 Gateway 分发。

### 9.3 传输通道边界

大文件不走 gRPC，单独走制品文件通道：

```text
端点：GET /v1/artifacts/models/{name}/{version}
      GET /v1/artifacts/agents/{version}
鉴权：与 gRPC 相同的 mTLS 客户端身份
特性：HTTP Range、断点续传
```

职责切分：

- gRPC 控制面只下发“下载哪个制品、`checksum`、`signatureURI`”
- 制品文件通道只负责搬运字节
- 校验（`sha256 + signature`）在 Edge 落盘前完成，未通过不得入缓存

***

## 10. Runtime 执行接口

为了让上层任务统一，Runtime Adapter 至少需要实现：

```text
prepare(model)
start(instance)
stop(instance)
status(instance)
remove(model)
metrics(instance)
```

这样 Task Engine 和 Edge Agent 只依赖统一接口，而不依赖某个具体 Runtime。

***

## 11. V1 非目标

V1 暂不实现：

- 自动模型量化流水线
- 自动 Benchmark 驱动 Runtime 选择
- 多模型联合调度
- 在线热迁移
- 全功能 MLOps Registry

***

## 12. 后续细化项

需要继续补充：

- `Model` / `RuntimeProfile` 字段定义
- Artifact 命名规范
- 签名公钥分发方案
- Gateway 缓存目录结构
- Runtime Adapter 接口草案
