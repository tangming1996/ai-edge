package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds edge-agent runtime configuration.
type Config struct {
	GatewayAddr       string        `json:"gateway_addr"`
	GatewayID         string        `json:"gateway_id"`
	GatewayHTTPAddr   string        `json:"gateway_http_addr"`
	Token             string        `json:"token"`
	DataDir           string        `json:"data_dir"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	AgentVersion      string        `json:"agent_version"`
}

func (c *Config) applyDefaults() {
	if c.DataDir == "" {
		c.DataDir = "/etc/edge-agent"
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 10 * time.Second
	}
	if c.AgentVersion == "" {
		c.AgentVersion = "dev"
	}
}

func (c *Config) validate() error {
	if c.GatewayAddr == "" {
		return fmt.Errorf("agent: gateway_addr is required")
	}
	return nil
}

// KeyPath returns the path to the node private key.
func (c *Config) KeyPath() string { return c.DataDir + "/node.key" }

// CertPath returns the path to the node certificate.
func (c *Config) CertPath() string { return c.DataDir + "/node.crt" }

// CAPath returns the path to the CA certificate.
func (c *Config) CAPath() string { return c.DataDir + "/ca.crt" }

// NodeIDPath returns the path to the persisted node ID.
func (c *Config) NodeIDPath() string { return c.DataDir + "/node-id" }

// LoadConfig reads a JSON config file and merges environment variable overrides.
func LoadConfig(path string) (*Config, error) {
	var cfg Config

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("agent: read config: %w", err)
		}
		if err := unmarshalConfig(data, &cfg); err != nil {
			return nil, fmt.Errorf("agent: parse config: %w", err)
		}
	}

	if v := os.Getenv("EDGE_GATEWAY_ADDR"); v != "" {
		cfg.GatewayAddr = v
	}
	if v := os.Getenv("EDGE_GATEWAY_ID"); v != "" {
		cfg.GatewayID = v
	}
	if v := os.Getenv("EDGE_GATEWAY_HTTP_ADDR"); v != "" {
		cfg.GatewayHTTPAddr = v
	}
	if v := os.Getenv("EDGE_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("EDGE_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("EDGE_HEARTBEAT_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("agent: invalid EDGE_HEARTBEAT_INTERVAL: %w", err)
		}
		cfg.HeartbeatInterval = d
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func unmarshalConfig(data []byte, cfg *Config) error {
	var raw struct {
		GatewayAddr       string          `json:"gateway_addr"`
		GatewayID         string          `json:"gateway_id"`
		GatewayHTTPAddr   string          `json:"gateway_http_addr"`
		Token             string          `json:"token"`
		DataDir           string          `json:"data_dir"`
		HeartbeatInterval json.RawMessage `json:"heartbeat_interval"`
		AgentVersion      string          `json:"agent_version"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	cfg.GatewayAddr = raw.GatewayAddr
	cfg.GatewayID = raw.GatewayID
	cfg.GatewayHTTPAddr = raw.GatewayHTTPAddr
	cfg.Token = raw.Token
	cfg.DataDir = raw.DataDir
	cfg.AgentVersion = raw.AgentVersion

	if len(raw.HeartbeatInterval) > 0 && string(raw.HeartbeatInterval) != "null" {
		d, err := parseDurationValue(raw.HeartbeatInterval)
		if err != nil {
			return err
		}
		cfg.HeartbeatInterval = d
	}
	return nil
}

func parseDurationValue(raw json.RawMessage) (time.Duration, error) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return time.ParseDuration(asString)
	}

	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil {
		return time.Duration(asInt), nil
	}

	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		return time.Duration(asFloat), nil
	}

	return 0, fmt.Errorf("invalid duration value: %s", strconv.Quote(string(raw)))
}
