package runtime

import "context"

// Status represents the current state of a runtime instance.
type Status struct {
	Running bool              `json:"running"`
	State   string            `json:"state"`
	Info    map[string]string `json:"info,omitempty"`
}

// InstallConfig holds parameters for installing a model runtime.
type InstallConfig struct {
	ModelName     string            `json:"model_name"`
	ModelVersion  string            `json:"model_version"`
	ArtifactURI   string            `json:"artifact_uri"`
	RuntimeConfig map[string]string `json:"runtime_config,omitempty"`
}

// RuntimeAdapter is the interface that runtime implementations must satisfy.
// Each adapter handles a specific runtime engine (e.g. llama.cpp, ONNX Runtime).
type RuntimeAdapter interface {
	// Name returns the unique name of this runtime adapter.
	Name() string

	// Install downloads and prepares the model for serving.
	Install(ctx context.Context, cfg InstallConfig) error

	// Start launches the runtime process for the given model.
	Start(ctx context.Context, modelName, modelVersion string) error

	// Stop gracefully shuts down the runtime process.
	Stop(ctx context.Context, modelName, modelVersion string) error

	// Restart performs a stop followed by start.
	Restart(ctx context.Context, modelName, modelVersion string) error

	// Uninstall removes model artifacts and cleans up.
	Uninstall(ctx context.Context, modelName, modelVersion string) error

	// Status returns the current status of the runtime for a model.
	Status(ctx context.Context, modelName, modelVersion string) (*Status, error)
}
