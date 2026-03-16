// Package config handles loading and validating the Rutoso configuration file.
// The config file is a YAML document specified via the --config flag.
// All string values support ${ENV_VAR} substitution, applied before parsing.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all runtime configuration for Rutoso.
type Config struct {
	// Server contains the REST API HTTP server settings.
	Server ServerConfig `yaml:"server"`

	// XDS contains the gRPC xDS server settings.
	XDS XDSConfig `yaml:"xds"`

	// Log controls the logging behaviour.
	Log LogConfig `yaml:"log"`
}

// ServerConfig holds configuration for the REST API HTTP server.
type ServerConfig struct {
	// Address is the host:port the HTTP server listens on.
	// Default: ":8080"
	Address string `yaml:"address"`
}

// XDSConfig holds configuration for the gRPC xDS control-plane server.
type XDSConfig struct {
	// Address is the host:port the gRPC server listens on.
	// Default: ":18000"
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
// raw content, unmarshals it into a Config, and applies defaults for any
// fields that were not specified.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	// Expand ${ENV_VAR} references before parsing so that any string value in
	// the YAML can reference environment variables.
	expanded := os.ExpandEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.Server.Address == "" {
		cfg.Server.Address = ":8080"
	}
	if cfg.XDS.Address == "" {
		cfg.XDS.Address = ":18000"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "console"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
}
