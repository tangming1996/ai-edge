package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// modelTaskPayload is the JSON payload for model lifecycle tasks.
type modelTaskPayload struct {
	ModelName    string `json:"model_name"`
	ModelVersion string `json:"model_version"`
	Checksum     string `json:"checksum,omitempty"`
	ArtifactURI  string `json:"artifact_uri,omitempty"`
}

// modelTaskResult is the JSON result returned after a model task completes.
type modelTaskResult struct {
	ModelName    string `json:"model_name"`
	ModelVersion string `json:"model_version"`
	LocalPath    string `json:"local_path,omitempty"`
	Checksum     string `json:"checksum,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	Deleted      bool   `json:"deleted,omitempty"`
}

// ModelExecutor implements TaskExecutor for InstallModel and DeleteModel tasks.
type ModelExecutor struct {
	downloader *Downloader
	dataDir    string
}

// NewModelExecutor creates a ModelExecutor backed by the given Downloader.
func NewModelExecutor(downloader *Downloader, dataDir string) *ModelExecutor {
	return &ModelExecutor{
		downloader: downloader,
		dataDir:    dataDir,
	}
}

// Execute dispatches to install or delete based on task type.
func (e *ModelExecutor) Execute(ctx context.Context, taskType string, payload []byte) ([]byte, error) {
	var p modelTaskPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("model executor: unmarshal payload: %w", err)
	}

	if p.ModelName == "" {
		return nil, fmt.Errorf("model executor: model_name is required")
	}
	if p.ModelVersion == "" {
		return nil, fmt.Errorf("model executor: model_version is required")
	}

	switch taskType {
	case "InstallModel":
		return e.installModel(ctx, &p)
	case "DeleteModel":
		return e.deleteModel(ctx, &p)
	default:
		return nil, fmt.Errorf("model executor: unknown task type %q", taskType)
	}
}

func (e *ModelExecutor) installModel(_ context.Context, p *modelTaskPayload) ([]byte, error) {
	log.Printf("model executor: installing %s:%s", p.ModelName, p.ModelVersion)

	result, err := e.downloader.Download(p.ModelName, p.ModelVersion, p.Checksum)
	if err != nil {
		return nil, fmt.Errorf("model executor: download %s:%s: %w", p.ModelName, p.ModelVersion, err)
	}

	log.Printf("model executor: installed %s:%s → %s (%d bytes, sha256=%s)",
		p.ModelName, p.ModelVersion, result.LocalPath, result.SizeBytes, result.Checksum)

	out := modelTaskResult{
		ModelName:    p.ModelName,
		ModelVersion: p.ModelVersion,
		LocalPath:    result.LocalPath,
		Checksum:     result.Checksum,
		SizeBytes:    result.SizeBytes,
	}
	return json.Marshal(out)
}

func (e *ModelExecutor) deleteModel(_ context.Context, p *modelTaskPayload) ([]byte, error) {
	log.Printf("model executor: deleting %s:%s", p.ModelName, p.ModelVersion)

	modelDir := filepath.Join(e.dataDir, "models", sanitizeName(p.ModelName), p.ModelVersion)

	if _, err := os.Stat(modelDir); os.IsNotExist(err) {
		log.Printf("model executor: %s:%s not found locally, treating as success", p.ModelName, p.ModelVersion)
	} else if err != nil {
		return nil, fmt.Errorf("model executor: stat %s: %w", modelDir, err)
	} else {
		if err := os.RemoveAll(modelDir); err != nil {
			return nil, fmt.Errorf("model executor: remove %s: %w", modelDir, err)
		}
		log.Printf("model executor: deleted %s", modelDir)
	}

	out := modelTaskResult{
		ModelName:    p.ModelName,
		ModelVersion: p.ModelVersion,
		Deleted:      true,
	}
	return json.Marshal(out)
}
