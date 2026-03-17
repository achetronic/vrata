// Package config handles loading and validating the Rutoso configuration file.
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

	// Log controls the logging behaviour.
	Log LogConfig `yaml:"log"`
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
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Address == "" {
		cfg.Server.Address = ":8080"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "console"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
}
