## ADDED Requirements

### Requirement: 用户不直接操作底层运行时
用户 SHALL NOT 直接操作 `vLLM`、`TensorRT`、`llama.cpp`、`ONNX Runtime` 等底层运行时，而统一通过平台抽象（如 `runtime: auto`）声明意图。

#### Scenario: 声明式运行时意图
- **WHEN** 用户在部署中指定 `runtime: auto`
- **THEN** 平台负责为目标硬件选择具体运行时，用户无需感知底层引擎

### Requirement: RuntimeProfile 选择规则
系统 SHALL 通过 `RuntimeProfile`（selector + runtime + priority + runtime_config）定义硬件到运行时的映射规则，支持按硬件特征（如 GPU 型号）选择运行时（例：Orin→TensorRT、A100→vLLM、CPU→llama.cpp）。

#### Scenario: 按硬件匹配运行时
- **WHEN** 目标节点 GPU 为 `orin` 且存在匹配的 RuntimeProfile
- **THEN** 解析结果选择 `tensorrt` 运行时

#### Scenario: 优先级裁决
- **WHEN** 多个 RuntimeProfile 同时匹配某节点
- **THEN** 选择 `priority` 最高者

### Requirement: runtime: auto 在生成 NodeTask 时解析
`runtime: auto` 的解析 SHALL 在生成 `NodeTask` 时完成，并把解析后的具体 runtime 与参数写入 `NodeTask.payload`。Edge SHALL NOT 反查 `RuntimeProfile`。

#### Scenario: 解析结果下发到节点
- **WHEN** 为某节点生成 InstallModel/StartRuntime NodeTask 且部署声明 `runtime: auto`
- **THEN** payload 中携带已解析的具体运行时与参数
- **AND** Edge 直接按 payload 执行，不再查询 RuntimeProfile

### Requirement: 运行时适配器统一接口
runtime-manager SHALL 通过统一适配器接口管理异构运行时的安装、启动、停止、重启与卸载，向上层屏蔽各引擎差异。

#### Scenario: 统一生命周期操作
- **WHEN** 收到 `StartRuntime`/`StopRuntime`/`RestartRuntime` 任务
- **THEN** runtime-manager 通过对应运行时适配器执行相应生命周期操作并回报状态
