package runtime_test

import (
	"context"
	"testing"

	"github.com/edgeai-platform/ai-edge/internal/runtime"
)

func TestStatus_ZeroValue(t *testing.T) {
	var s runtime.Status
	if s.Running {
		t.Error("zero value Running should be false")
	}
	if s.State != "" {
		t.Errorf("zero value State = %q, want empty", s.State)
	}
	if s.Info != nil {
		t.Errorf("zero value Info = %v, want nil", s.Info)
	}
}

func TestInstallConfig_Fields(t *testing.T) {
	c := runtime.InstallConfig{
		ModelName:    "m",
		ModelVersion: "v",
		ArtifactURI:  "s3://b/m.bin",
		RuntimeConfig: map[string]string{
			"threads": "4",
		},
	}
	if c.ModelName != "m" || c.ModelVersion != "v" || c.ArtifactURI != "s3://b/m.bin" {
		t.Errorf("InstallConfig fields lost: %+v", c)
	}
	if c.RuntimeConfig["threads"] != "4" {
		t.Errorf("RuntimeConfig not set: %+v", c.RuntimeConfig)
	}
}

func TestManager_Register_Get(t *testing.T) {
	m := runtime.NewManager()
	ad := runtime.NewLlamaCppAdapter(t.TempDir())
	m.Register(ad)
	got, err := m.Get("llamacpp")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name() != "llamacpp" {
		t.Errorf("Name = %q, want llamacpp", got.Name())
	}
}

func TestManager_Get_Unknown(t *testing.T) {
	m := runtime.NewManager()
	_, err := m.Get("unknown")
	if err == nil {
		t.Fatal("expected error for unknown adapter")
	}
}

func TestManager_Register_DuplicatePanics(t *testing.T) {
	m := runtime.NewManager()
	ad := runtime.NewLlamaCppAdapter(t.TempDir())
	m.Register(ad)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate register")
		}
	}()
	m.Register(ad)
}

func TestManager_ListAdapters_Empty(t *testing.T) {
	m := runtime.NewManager()
	if got := m.ListAdapters(); len(got) != 0 {
		t.Errorf("ListAdapters = %v, want empty", got)
	}
}

func TestManager_ListAdapters_AfterRegister(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	got := m.ListAdapters()
	if len(got) != 1 || got[0] != "llamacpp" {
		t.Errorf("ListAdapters = %v, want [llamacpp]", got)
	}
}

func TestManager_Install_Unknown(t *testing.T) {
	m := runtime.NewManager()
	if err := m.Install(context.Background(), "nope", runtime.InstallConfig{}); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestManager_Start_Unknown(t *testing.T) {
	m := runtime.NewManager()
	if err := m.Start(context.Background(), "nope", "m", "v"); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestManager_Stop_Unknown(t *testing.T) {
	m := runtime.NewManager()
	if err := m.Stop(context.Background(), "nope", "m", "v"); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestManager_Restart_Unknown(t *testing.T) {
	m := runtime.NewManager()
	if err := m.Restart(context.Background(), "nope", "m", "v"); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestManager_Uninstall_Unknown(t *testing.T) {
	m := runtime.NewManager()
	if err := m.Uninstall(context.Background(), "nope", "m", "v"); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestManager_Status_Unknown(t *testing.T) {
	m := runtime.NewManager()
	if _, err := m.Status(context.Background(), "nope", "m", "v"); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestLlamaCppAdapter_Name(t *testing.T) {
	ad := runtime.NewLlamaCppAdapter(t.TempDir())
	if ad.Name() != "llamacpp" {
		t.Errorf("Name = %q, want llamacpp", ad.Name())
	}
}

func TestLlamaCppAdapter_Install(t *testing.T) {
	ad := runtime.NewLlamaCppAdapter(t.TempDir())
	if err := ad.Install(context.Background(), runtime.InstallConfig{
		ModelName:    "m",
		ModelVersion: "v",
		ArtifactURI:  "s3://b/m.bin",
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}
}

func TestLlamaCppAdapter_StartStop(t *testing.T) {
	ad := runtime.NewLlamaCppAdapter(t.TempDir())
	if err := ad.Start(context.Background(), "m", "v"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ad.Start(context.Background(), "m", "v"); err == nil {
		t.Fatal("Start of already running should fail")
	}
	if err := ad.Stop(context.Background(), "m", "v"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := ad.Stop(context.Background(), "m", "v"); err == nil {
		t.Fatal("Stop of not running should fail")
	}
}

func TestLlamaCppAdapter_Restart(t *testing.T) {
	ad := runtime.NewLlamaCppAdapter(t.TempDir())
	if err := ad.Restart(context.Background(), "m", "v"); err != nil {
		t.Fatalf("Restart from cold: %v", err)
	}
	if err := ad.Restart(context.Background(), "m", "v"); err != nil {
		t.Fatalf("Restart while running: %v", err)
	}
}

func TestLlamaCppAdapter_Status(t *testing.T) {
	ad := runtime.NewLlamaCppAdapter(t.TempDir())

	s, err := ad.Status(context.Background(), "m", "v")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s.Running || s.State != "Stopped" {
		t.Errorf("initial status = %+v", s)
	}

	if err := ad.Start(context.Background(), "m", "v"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s, err = ad.Status(context.Background(), "m", "v")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !s.Running || s.State != "Running" {
		t.Errorf("running status = %+v", s)
	}
}

func TestLlamaCppAdapter_Uninstall(t *testing.T) {
	ad := runtime.NewLlamaCppAdapter(t.TempDir())
	// Uninstall of a not-running model: the Stop call inside Uninstall
	// is best-effort and may fail; the contract is that Uninstall
	// returns nil regardless.
	if err := ad.Uninstall(context.Background(), "m", "v"); err != nil {
		t.Errorf("Uninstall of cold model: %v", err)
	}
	// Uninstall of a running model: must also return nil and stop it.
	if err := ad.Start(context.Background(), "m", "v"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := ad.Uninstall(context.Background(), "m", "v"); err != nil {
		t.Errorf("Uninstall of running model: %v", err)
	}
	s, _ := ad.Status(context.Background(), "m", "v")
	if s.Running {
		t.Error("Uninstall should stop the model")
	}
}

func TestManager_InstallDispatches(t *testing.T) {
	m := runtime.NewManager()
	ad := runtime.NewLlamaCppAdapter(t.TempDir())
	m.Register(ad)
	if err := m.Install(context.Background(), "llamacpp", runtime.InstallConfig{
		ModelName: "m", ModelVersion: "v", ArtifactURI: "u",
	}); err != nil {
		t.Fatalf("Install via manager: %v", err)
	}
}

func TestManager_StartDispatches(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	if err := m.Start(context.Background(), "llamacpp", "m", "v"); err != nil {
		t.Fatalf("Start via manager: %v", err)
	}
}

func TestManager_StopDispatches(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	if err := m.Start(context.Background(), "llamacpp", "m", "v"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop(context.Background(), "llamacpp", "m", "v"); err != nil {
		t.Fatalf("Stop via manager: %v", err)
	}
}

func TestManager_StatusDispatches(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	if err := m.Start(context.Background(), "llamacpp", "m", "v"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s, err := m.Status(context.Background(), "llamacpp", "m", "v")
	if err != nil {
		t.Fatalf("Status via manager: %v", err)
	}
	if !s.Running {
		t.Error("Status should report running")
	}
}

func TestManager_RestartDispatches(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	if err := m.Restart(context.Background(), "llamacpp", "m", "v"); err != nil {
		t.Fatalf("Restart via manager: %v", err)
	}
}

func TestManager_UninstallDispatches(t *testing.T) {
	m := runtime.NewManager()
	m.Register(runtime.NewLlamaCppAdapter(t.TempDir()))
	if err := m.Uninstall(context.Background(), "llamacpp", "m", "v"); err != nil {
		t.Fatalf("Uninstall via manager: %v", err)
	}
}
