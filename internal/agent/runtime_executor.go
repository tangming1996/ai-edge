package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/edgeai-platform/ai-edge/internal/runtime"
)

// runtimeTaskPayload is the JSON payload for runtime-related tasks.
type runtimeTaskPayload struct {
	DeploymentID string `json:"deployment_id"`
	ModelName    string `json:"model_name"`
	ModelVersion string `json:"model_version"`
	Runtime      string `json:"runtime"`
	Action       string `json:"action"`
	ArtifactURI  string `json:"artifact_uri,omitempty"`
}

// RuntimeExecutor implements TaskExecutor for runtime lifecycle tasks:
// StartRuntime, StopRuntime, RestartRuntime, UpgradeRuntime.
type RuntimeExecutor struct {
	manager *runtime.Manager
}

// NewRuntimeExecutor creates a RuntimeExecutor backed by the given runtime Manager.
func NewRuntimeExecutor(manager *runtime.Manager) *RuntimeExecutor {
	return &RuntimeExecutor{manager: manager}
}

// Execute dispatches to the appropriate runtime operation based on task type.
func (e *RuntimeExecutor) Execute(ctx context.Context, taskType string, payload []byte) ([]byte, error) {
	var p runtimeTaskPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("runtime executor: unmarshal payload: %w", err)
	}

	if p.Runtime == "" {
		return nil, fmt.Errorf("runtime executor: runtime field is required")
	}

	var err error
	switch taskType {
	case "StartRuntime":
		err = e.startRuntime(ctx, &p)
	case "StopRuntime":
		err = e.stopRuntime(ctx, &p)
	case "RestartRuntime":
		err = e.restartRuntime(ctx, &p)
	case "UpgradeRuntime":
		err = e.upgradeRuntime(ctx, &p)
	default:
		return nil, fmt.Errorf("runtime executor: unknown task type %q", taskType)
	}

	if err != nil {
		return nil, err
	}

	st, err := e.manager.Status(ctx, p.Runtime, p.ModelName, p.ModelVersion)
	if err != nil {
		return nil, fmt.Errorf("runtime executor: get status after %s: %w", taskType, err)
	}

	result, _ := json.Marshal(st)
	return result, nil
}

func (e *RuntimeExecutor) startRuntime(ctx context.Context, p *runtimeTaskPayload) error {
	if p.ArtifactURI != "" {
		cfg := runtime.InstallConfig{
			ModelName:    p.ModelName,
			ModelVersion: p.ModelVersion,
			ArtifactURI:  p.ArtifactURI,
		}
		if err := e.manager.Install(ctx, p.Runtime, cfg); err != nil {
			return fmt.Errorf("install: %w", err)
		}
	}
	return e.manager.Start(ctx, p.Runtime, p.ModelName, p.ModelVersion)
}

func (e *RuntimeExecutor) stopRuntime(ctx context.Context, p *runtimeTaskPayload) error {
	return e.manager.Stop(ctx, p.Runtime, p.ModelName, p.ModelVersion)
}

func (e *RuntimeExecutor) restartRuntime(ctx context.Context, p *runtimeTaskPayload) error {
	return e.manager.Restart(ctx, p.Runtime, p.ModelName, p.ModelVersion)
}

func (e *RuntimeExecutor) upgradeRuntime(ctx context.Context, p *runtimeTaskPayload) error {
	if err := e.manager.Stop(ctx, p.Runtime, p.ModelName, p.ModelVersion); err != nil {
		// Best-effort stop before upgrade
		_ = err
	}

	if p.ArtifactURI != "" {
		cfg := runtime.InstallConfig{
			ModelName:    p.ModelName,
			ModelVersion: p.ModelVersion,
			ArtifactURI:  p.ArtifactURI,
		}
		if err := e.manager.Install(ctx, p.Runtime, cfg); err != nil {
			return fmt.Errorf("upgrade install: %w", err)
		}
	}

	return e.manager.Start(ctx, p.Runtime, p.ModelName, p.ModelVersion)
}
