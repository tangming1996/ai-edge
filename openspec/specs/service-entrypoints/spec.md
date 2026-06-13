# service-entrypoints Specification

## Purpose
TBD - created by archiving change wire-runtime-entrypoints. Update Purpose after archive.
## Requirements
### Requirement: Controller process entrypoint
The system SHALL provide a real `controller` process entrypoint that loads database configuration, constructs the deployment controller dependencies, starts the reconcile loop, and remains running until shutdown is requested.

#### Scenario: Controller enters reconcile loop
- **WHEN** the `controller` binary starts with valid database configuration
- **THEN** it connects to the metadata store, constructs the deployment controller, and starts the long-running reconcile loop instead of exiting immediately

#### Scenario: Controller fails fast on invalid configuration
- **WHEN** the `controller` binary starts without required database configuration or the database connection fails
- **THEN** it exits with a non-zero status after logging the startup failure

### Requirement: Process lifecycle management
Each runtime binary introduced by this change (`gateway-runtime`, `edge-agent`, and `controller`) MUST install signal handling for `SIGINT` and `SIGTERM`, cancel background work through a shared root context, and close listeners or connections before process exit.

#### Scenario: Graceful shutdown on termination signal
- **WHEN** one of the runtime binaries receives `SIGTERM`
- **THEN** it stops accepting new work, cancels background loops, closes owned resources, and exits cleanly

### Requirement: Startup configuration contract
Each runtime binary introduced by this change MUST expose a documented startup configuration contract whose values come from existing deployment mechanisms already used by the repository, so that local runs, systemd units, and Kubernetes manifests can start the same process without custom patches.

#### Scenario: Existing deployment artifact can supply startup config
- **WHEN** an operator starts a runtime binary through the repository's existing install script, systemd unit, or Kubernetes manifest
- **THEN** the binary reads its required configuration from the expected environment variables or config file path without requiring code changes

