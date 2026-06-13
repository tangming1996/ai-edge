package agent_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/agent"
)

// TestConfig_PathHelpers covers the four pure DataDir+filename helpers
// on Config. They are the most frequently called code paths in the
// package; locking down their contract avoids accidental rename.
func TestConfig_PathHelpers(t *testing.T) {
	cfg := &agent.Config{DataDir: "/var/lib/edge-agent"}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"KeyPath", cfg.KeyPath(), "/var/lib/edge-agent/node.key"},
		{"CertPath", cfg.CertPath(), "/var/lib/edge-agent/node.crt"},
		{"CAPath", cfg.CAPath(), "/var/lib/edge-agent/ca.crt"},
		{"NodeIDPath", cfg.NodeIDPath(), "/var/lib/edge-agent/node-id"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestLoadConfig_MissingFile covers the documented "no path" branch:
// LoadConfig returns the default config (with applyDefaults applied) and
// validates the required GatewayAddr. The env var fallback is tested
// separately.
func TestLoadConfig_MissingFile(t *testing.T) {
	// Clear relevant env vars to ensure clean defaults.
	for _, k := range []string{
		"EDGE_GATEWAY_ADDR", "EDGE_GATEWAY_ID", "EDGE_GATEWAY_HTTP_ADDR",
		"EDGE_TOKEN", "EDGE_DATA_DIR", "EDGE_HEARTBEAT_INTERVAL",
	} {
		t.Setenv(k, "")
	}
	if _, err := agent.LoadConfig(""); err == nil {
		t.Fatal("expected error when GatewayAddr is empty")
	}
}

// TestLoadConfig_ValidJSONFile covers the happy path: a valid config
// file is parsed, defaults are applied, validation passes.
func TestLoadConfig_ValidJSONFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
		"gateway_addr": "gw.example.com:7443",
		"gateway_id": "gw-1",
		"data_dir": "` + dir + `",
		"agent_version": "1.2.3"
	}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Clear env vars so we know we're exercising the file path.
	t.Setenv("EDGE_GATEWAY_ADDR", "")
	t.Setenv("EDGE_GATEWAY_ID", "")
	t.Setenv("EDGE_DATA_DIR", "")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "")

	cfg, err := agent.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.GatewayAddr != "gw.example.com:7443" {
		t.Errorf("GatewayAddr = %q", cfg.GatewayAddr)
	}
	if cfg.GatewayID != "gw-1" {
		t.Errorf("GatewayID = %q", cfg.GatewayID)
	}
	if cfg.DataDir != dir {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.AgentVersion != "1.2.3" {
		t.Errorf("AgentVersion = %q", cfg.AgentVersion)
	}
	if cfg.HeartbeatInterval != 10*time.Second {
		t.Errorf("default HeartbeatInterval not applied: %s", cfg.HeartbeatInterval)
	}
}

// TestLoadConfig_InvalidJSON covers the malformed-JSON branch.
func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("not-json"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EDGE_GATEWAY_ADDR", "")
	if _, err := agent.LoadConfig(path); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// TestLoadConfig_NonexistentFile covers the "file does not exist" branch.
func TestLoadConfig_NonexistentFile(t *testing.T) {
	t.Setenv("EDGE_GATEWAY_ADDR", "")
	if _, err := agent.LoadConfig("/tmp/this-file-does-not-exist-xyz.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestLoadConfig_EnvOverrides covers the documented behaviour: env vars
// always win over the JSON file.
func TestLoadConfig_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"gateway_addr": "from-file"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EDGE_GATEWAY_ADDR", "from-env")
	t.Setenv("EDGE_GATEWAY_ID", "")
	t.Setenv("EDGE_DATA_DIR", "")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "")

	cfg, err := agent.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.GatewayAddr != "from-env" {
		t.Fatalf("env did not override file: %q", cfg.GatewayAddr)
	}
}

// TestLoadConfig_EnvInvalidDuration covers the documented error path
// for malformed EDGE_HEARTBEAT_INTERVAL.
func TestLoadConfig_EnvInvalidDuration(t *testing.T) {
	t.Setenv("EDGE_GATEWAY_ADDR", "gw")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "not-a-duration")
	if _, err := agent.LoadConfig(""); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

// TestLoadConfig_EnvValidDuration covers the success path for the
// duration env var.
func TestLoadConfig_EnvValidDuration(t *testing.T) {
	t.Setenv("EDGE_GATEWAY_ADDR", "gw")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "30s")
	t.Setenv("EDGE_DATA_DIR", "")
	t.Setenv("EDGE_GATEWAY_ID", "")

	cfg, err := agent.LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.HeartbeatInterval != 30*time.Second {
		t.Errorf("HeartbeatInterval = %s, want 30s", cfg.HeartbeatInterval)
	}
}

// TestLoadConfig_EnvDataDir covers the EDGE_DATA_DIR fallback.
func TestLoadConfig_EnvDataDir(t *testing.T) {
	t.Setenv("EDGE_GATEWAY_ADDR", "gw")
	t.Setenv("EDGE_DATA_DIR", "/tmp/custom-edge-agent")
	t.Setenv("EDGE_GATEWAY_ID", "")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "")

	cfg, err := agent.LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DataDir != "/tmp/custom-edge-agent" {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
}

// TestLoadConfig_ApplyDefaults covers the default values applied when
// the JSON file is empty and env vars are clear.
func TestLoadConfig_ApplyDefaults(t *testing.T) {
	t.Setenv("EDGE_GATEWAY_ADDR", "gw")
	t.Setenv("EDGE_DATA_DIR", "")
	t.Setenv("EDGE_GATEWAY_ID", "")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "")

	cfg, err := agent.LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DataDir != "/etc/edge-agent" {
		t.Errorf("default DataDir not applied: %q", cfg.DataDir)
	}
	if cfg.AgentVersion != "dev" {
		t.Errorf("default AgentVersion not applied: %q", cfg.AgentVersion)
	}
	if cfg.HeartbeatInterval != 10*time.Second {
		t.Errorf("default HeartbeatInterval not applied: %s", cfg.HeartbeatInterval)
	}
}

// TestLoadConfig_DurationAsInt covers the duration parser's int branch
// (the JSON file can encode duration as a raw integer of nanoseconds).
func TestLoadConfig_DurationAsInt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Duration encoded as a raw integer of nanoseconds.
	body := `{
		"gateway_addr": "gw",
		"heartbeat_interval": 2000000000
	}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EDGE_GATEWAY_ADDR", "")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "")

	cfg, err := agent.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.HeartbeatInterval != 2*time.Second {
		t.Errorf("int duration not parsed: %s", cfg.HeartbeatInterval)
	}
}

// TestLoadConfig_DurationAsFloat covers the float branch of the parser.
func TestLoadConfig_DurationAsFloat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
		"gateway_addr": "gw",
		"heartbeat_interval": 1.5e9
	}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EDGE_GATEWAY_ADDR", "")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "")

	cfg, err := agent.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.HeartbeatInterval != 1500*time.Millisecond {
		t.Errorf("float duration not parsed: %s", cfg.HeartbeatInterval)
	}
}

// TestLoadConfig_DurationAsInvalid covers the parser's "all branches
// fail" path: an unparseable value must produce a clear error.
func TestLoadConfig_DurationAsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
		"gateway_addr": "gw",
		"heartbeat_interval": {"nested": "object"}
	}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EDGE_GATEWAY_ADDR", "")
	t.Setenv("EDGE_HEARTBEAT_INTERVAL", "")

	if _, err := agent.LoadConfig(path); err == nil {
		t.Fatal("expected error for invalid duration value")
	}
}

// TestConfig_JSONRoundTrip documents the public JSON contract for
// the Config struct.
func TestConfig_JSONRoundTrip(t *testing.T) {
	in := agent.Config{
		GatewayAddr:       "gw:7443",
		GatewayID:         "gw-1",
		GatewayHTTPAddr:   "http://gw:8080",
		Token:             "secret",
		DataDir:           "/var/lib",
		HeartbeatInterval: 5 * time.Second,
		AgentVersion:      "1.0.0",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out agent.Config
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.GatewayAddr != in.GatewayAddr {
		t.Errorf("GatewayAddr round-trip: %q vs %q", out.GatewayAddr, in.GatewayAddr)
	}
	if out.HeartbeatInterval != in.HeartbeatInterval {
		t.Errorf("HeartbeatInterval round-trip: %s vs %s", out.HeartbeatInterval, in.HeartbeatInterval)
	}
}
