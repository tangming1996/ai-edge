package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/edgeai-platform/ai-edge/internal/agent"
	"github.com/edgeai-platform/ai-edge/internal/runtime"
)

func TestModelExecutor_InstallModel_MissingName(t *testing.T) {
	dir := t.TempDir()
	dl := agent.NewDownloader(agent.DownloaderConfig{DataDir: dir})
	ex := agent.NewModelExecutor(dl, dir)
	_, err := ex.Execute(context.Background(), "InstallModel", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing model_name")
	}
}

func TestModelExecutor_InstallModel_MissingVersion(t *testing.T) {
	dir := t.TempDir()
	dl := agent.NewDownloader(agent.DownloaderConfig{DataDir: dir})
	ex := agent.NewModelExecutor(dl, dir)
	_, err := ex.Execute(context.Background(), "InstallModel", []byte(`{"model_name":"m"}`))
	if err == nil {
		t.Fatal("expected error for missing model_version")
	}
}

func TestModelExecutor_DeleteModel_MissingName(t *testing.T) {
	dir := t.TempDir()
	dl := agent.NewDownloader(agent.DownloaderConfig{DataDir: dir})
	ex := agent.NewModelExecutor(dl, dir)
	_, err := ex.Execute(context.Background(), "DeleteModel", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing model_name")
	}
}

func TestModelExecutor_UnknownTaskType(t *testing.T) {
	dir := t.TempDir()
	dl := agent.NewDownloader(agent.DownloaderConfig{DataDir: dir})
	ex := agent.NewModelExecutor(dl, dir)
	_, err := ex.Execute(context.Background(), "NotARealTask", []byte(`{"model_name":"m","model_version":"v"}`))
	if err == nil {
		t.Fatal("expected error for unknown task type")
	}
}

func TestModelExecutor_InvalidPayload(t *testing.T) {
	dir := t.TempDir()
	dl := agent.NewDownloader(agent.DownloaderConfig{DataDir: dir})
	ex := agent.NewModelExecutor(dl, dir)
	_, err := ex.Execute(context.Background(), "InstallModel", []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestModelExecutor_DeleteModel_HappyPath(t *testing.T) {
	dir := t.TempDir()
	dl := agent.NewDownloader(agent.DownloaderConfig{DataDir: dir})
	ex := agent.NewModelExecutor(dl, dir)
	// Pre-create the model directory.
	if err := os.MkdirAll(filepath.Join(dir, "models", "m", "v"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err := ex.Execute(context.Background(), "DeleteModel", []byte(`{"model_name":"m","model_version":"v"}`))
	if err != nil {
		t.Fatalf("DeleteModel: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["deleted"] != true {
		t.Errorf("expected deleted=true, got %+v", got)
	}
}

func TestModelExecutor_DeleteModel_NotPresent(t *testing.T) {
	dir := t.TempDir()
	dl := agent.NewDownloader(agent.DownloaderConfig{DataDir: dir})
	ex := agent.NewModelExecutor(dl, dir)
	// Directory does not exist — must be treated as success.
	out, err := ex.Execute(context.Background(), "DeleteModel", []byte(`{"model_name":"m","model_version":"v"}`))
	if err != nil {
		t.Fatalf("DeleteModel: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["deleted"] != true {
		t.Errorf("expected deleted=true even when not present, got %+v", got)
	}
}

func TestRuntimeExecutor_MissingRuntime(t *testing.T) {
	m := runtime.NewManager()
	ex := agent.NewRuntimeExecutor(m)
	_, err := ex.Execute(context.Background(), "StartRuntime", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for missing runtime")
	}
}

func TestRuntimeExecutor_UnknownTaskType(t *testing.T) {
	m := runtime.NewManager()
	ex := agent.NewRuntimeExecutor(m)
	_, err := ex.Execute(context.Background(), "NotARealTask", []byte(`{"runtime":"llamacpp"}`))
	if err == nil {
		t.Fatal("expected error for unknown task type")
	}
}

func TestRuntimeExecutor_InvalidPayload(t *testing.T) {
	m := runtime.NewManager()
	ex := agent.NewRuntimeExecutor(m)
	_, err := ex.Execute(context.Background(), "StartRuntime", []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestRuntimeExecutor_StartRuntime_UnknownAdapter(t *testing.T) {
	m := runtime.NewManager()
	ex := agent.NewRuntimeExecutor(m)
	_, err := ex.Execute(context.Background(), "StartRuntime", []byte(`{"runtime":"unknown","model_name":"m","model_version":"v"}`))
	if err == nil {
		t.Fatal("expected error for unknown runtime adapter")
	}
}

func TestRuntimeExecutor_StartRuntime_HappyPath(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	ex := agent.NewRuntimeExecutor(m)
	out, err := ex.Execute(context.Background(), "StartRuntime", []byte(`{"runtime":"llamacpp","model_name":"m","model_version":"v"}`))
	if err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	if len(out) == 0 {
		t.Error("expected status result")
	}
}

func TestRuntimeExecutor_StopRuntime_HappyPath(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	ex := agent.NewRuntimeExecutor(m)
	// First start, then stop.
	if _, err := ex.Execute(context.Background(), "StartRuntime", []byte(`{"runtime":"llamacpp","model_name":"m","model_version":"v"}`)); err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	if _, err := ex.Execute(context.Background(), "StopRuntime", []byte(`{"runtime":"llamacpp","model_name":"m","model_version":"v"}`)); err != nil {
		t.Fatalf("StopRuntime: %v", err)
	}
}

func TestRuntimeExecutor_RestartRuntime_HappyPath(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	ex := agent.NewRuntimeExecutor(m)
	if _, err := ex.Execute(context.Background(), "RestartRuntime", []byte(`{"runtime":"llamacpp","model_name":"m","model_version":"v"}`)); err != nil {
		t.Fatalf("RestartRuntime: %v", err)
	}
}

func TestRuntimeExecutor_UpgradeRuntime_HappyPath(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	ex := agent.NewRuntimeExecutor(m)
	if _, err := ex.Execute(context.Background(), "UpgradeRuntime", []byte(`{"runtime":"llamacpp","model_name":"m","model_version":"v","artifact_uri":"s3://b/m"}`)); err != nil {
		t.Fatalf("UpgradeRuntime: %v", err)
	}
}

func TestRuntimeExecutor_StartRuntime_WithArtifact(t *testing.T) {
	// Install path is exercised when an artifact_uri is provided.
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	ex := agent.NewRuntimeExecutor(m)
	_, err := ex.Execute(context.Background(), "StartRuntime", []byte(`{"runtime":"llamacpp","model_name":"m","model_version":"v","artifact_uri":"s3://b/m"}`))
	if err != nil {
		t.Fatalf("StartRuntime with artifact: %v", err)
	}
}
