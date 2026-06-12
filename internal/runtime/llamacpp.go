package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// LlamaCppAdapter implements RuntimeAdapter for llama.cpp based inference.
type LlamaCppAdapter struct {
	dataDir string

	mu       sync.Mutex
	running  map[string]bool // key: "modelName:modelVersion"
}

// NewLlamaCppAdapter creates a new llama.cpp runtime adapter.
func NewLlamaCppAdapter(dataDir string) *LlamaCppAdapter {
	return &LlamaCppAdapter{
		dataDir: dataDir,
		running: make(map[string]bool),
	}
}

func (a *LlamaCppAdapter) Name() string { return "llamacpp" }

func (a *LlamaCppAdapter) Install(ctx context.Context, cfg InstallConfig) error {
	log.Printf("llamacpp: installing model %s:%s from %s", cfg.ModelName, cfg.ModelVersion, cfg.ArtifactURI)
	// TODO: download GGUF model file to dataDir
	return nil
}

func (a *LlamaCppAdapter) Start(ctx context.Context, modelName, modelVersion string) error {
	key := modelKey(modelName, modelVersion)
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running[key] {
		return fmt.Errorf("llamacpp: %s already running", key)
	}

	log.Printf("llamacpp: starting model %s", key)
	// TODO: launch llama-server process with the model file
	a.running[key] = true
	return nil
}

func (a *LlamaCppAdapter) Stop(ctx context.Context, modelName, modelVersion string) error {
	key := modelKey(modelName, modelVersion)
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running[key] {
		return fmt.Errorf("llamacpp: %s not running", key)
	}

	log.Printf("llamacpp: stopping model %s", key)
	// TODO: gracefully stop the llama-server process
	delete(a.running, key)
	return nil
}

func (a *LlamaCppAdapter) Restart(ctx context.Context, modelName, modelVersion string) error {
	if err := a.Stop(ctx, modelName, modelVersion); err != nil {
		log.Printf("llamacpp: stop before restart: %v (proceeding)", err)
	}
	return a.Start(ctx, modelName, modelVersion)
}

func (a *LlamaCppAdapter) Uninstall(ctx context.Context, modelName, modelVersion string) error {
	_ = a.Stop(ctx, modelName, modelVersion)
	log.Printf("llamacpp: uninstalling model %s:%s", modelName, modelVersion)
	// TODO: remove model files from dataDir
	return nil
}

func (a *LlamaCppAdapter) Status(ctx context.Context, modelName, modelVersion string) (*Status, error) {
	key := modelKey(modelName, modelVersion)
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running[key] {
		return &Status{Running: true, State: "Running"}, nil
	}
	return &Status{Running: false, State: "Stopped"}, nil
}

func modelKey(name, version string) string {
	return name + ":" + version
}
