## ADDED Requirements

### Requirement: Edge agent process wiring
`edge-agent` MUST provide a real process entrypoint that loads local configuration, prepares the data directory, loads or bootstraps node identity, establishes its long-lived mTLS gRPC connection, and starts the background loops required for an installed agent.

#### Scenario: Edge agent starts as a long-running process
- **WHEN** the `edge-agent` binary starts with a valid config file or environment overrides
- **THEN** it loads configuration, prepares identity, establishes its gateway connection, starts its background loops, and remains running instead of printing a placeholder message and exiting

### Requirement: Edge agent startup order
The `edge-agent` process MUST NOT start heartbeat, task execution, or certificate renewal loops before identity preparation has succeeded, because those loops depend on a valid node ID and client certificate context.

#### Scenario: Bootstrap failure prevents background loops
- **WHEN** the `edge-agent` process cannot load an existing identity and bootstrap also fails
- **THEN** it exits with a non-zero status and does not start heartbeat, task runner, or certificate renewal loops

### Requirement: Edge agent task execution assembly
The `edge-agent` process MUST assemble the task runner with a concrete executor that can dispatch to the existing runtime and model execution modules, so that pulled tasks can be executed by the long-running agent process.

#### Scenario: Agent process can execute pulled tasks
- **WHEN** the `edge-agent` process has started successfully and receives a task through the existing pull loop
- **THEN** the task runner forwards the task to the assembled executor and reports the result through the configured gateway connection

### Requirement: Edge agent graceful shutdown
When the `edge-agent` process is stopping, it MUST cancel its background loops, close its gRPC connection, and exit without corrupting persisted identity or local recovery state.

#### Scenario: Agent shutdown preserves local state
- **WHEN** the `edge-agent` process receives a shutdown signal while background loops are active
- **THEN** it cancels the loops, closes the connection, and leaves persisted identity and local state files intact
