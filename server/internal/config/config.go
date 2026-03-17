// Package config handles loading and validating the Rutoso configuration file.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Mode determines how the Rutoso process operates.
type Mode string

const (
	// ModeControlPlane runs the full control plane: REST API, persistent
	// store, gateway, and proxy. This is the default mode.
	ModeControlPlane Mode = "controlplane"

	// ModeProxy runs only the proxy layer. No local store, no REST API.
	// The process connects to a remote control plane via SSE and applies
	// configuration snapshots atomically as they arrive.
	ModeProxy Mode = "proxy"
)

// Config holds all runtime configuration for Rutoso.
type Config struct {
	// Mode selects the operating mode. Default: "controlplane".
	Mode Mode `yaml:"mode"`

	// Server contains the REST API HTTP server settings.
	// Only used in controlplane mode.
	Server ServerConfig `yaml:"server"`

	// ControlPlane holds settings for connecting to a remote control plane.
	// Only used in proxy mode.
	ControlPlane ControlPlaneConfig `yaml:"controlPlane"`

	// Log controls the logging behaviour.
	Log LogConfig `yaml:"log"`
}

// ControlPlaneConfig holds settings for proxy-mode instances that connect
// to a remote control plane to receive configuration via SSE.
type ControlPlaneConfig struct {
	// Address is the base URL of the control plane (e.g. "http://10.0.0.1:8080").
	Address string `yaml:"address"`

	// ReconnectInterval is how long to wait before reconnecting after a
	// stream disconnection. Accepts Go duration strings. Default: "5s".
	ReconnectInterval string `yaml:"reconnectInterval"`
}

// ServerConfig holds configuration for the REST API HTTP server.
type ServerConfig struct {
	// Address is the host:port the HTTP server listens on.
	// Default: ":8080"
	Address string `yaml:"address"`
}

// LogConfig controls logging format and verbosity.
type LogConfig struct {
	// Format is either "console" (human-readable text) or "json".
	// Default: "console"
	Format string `yaml:"format"`

	// Level is the minimum log level: "debug", "info", "warn", or "error".
	// Default: "info"
	Level string `yaml:"level"`
}

// Load reads the YAML file at path, expands environment variables in its
// raw content, unmarshals it into a Config, and applies defaults.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	expanded := os.ExpandEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	applyDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Mode == "" {
		cfg.Mode = ModeControlPlane
	}
	if cfg.Server.Address == "" {
		cfg.Server.Address = ":8080"
	}
	if cfg.ControlPlane.ReconnectInterval == "" {
		cfg.ControlPlane.ReconnectInterval = "5s"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "console"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
}

// Validate checks that the configuration is internally consistent.
func Validate(cfg *Config) error {
	switch cfg.Mode {
	case ModeControlPlane, ModeProxy:
	default:
		return fmt.Errorf("unknown mode %q: must be %q or %q", cfg.Mode, ModeControlPlane, ModeProxy)
	}
	if cfg.Mode == ModeProxy && cfg.ControlPlane.Address == "" {
		return fmt.Errorf("proxy mode requires controlPlane.address to be set")
	}
	return nil
}
