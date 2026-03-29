// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package config handles loading and validating the Vrata configuration file.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Mode determines how the Vrata process operates.
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

// Config holds all runtime configuration for Vrata.
type Config struct {
	// Mode selects the operating mode. Default: "controlplane".
	Mode Mode `yaml:"mode"`

	// ControlPlane holds settings for the control plane process: HTTP API
	// address, persistent storage, and optional Raft HA. Only used in
	// controlplane mode.
	ControlPlane ControlPlaneConfig `yaml:"controlPlane"`

	// Proxy holds settings for proxy-mode instances that connect to a
	// remote control plane. Only used in proxy mode.
	Proxy ProxyConfig `yaml:"proxy"`

	// Log controls the logging behaviour.
	Log LogConfig `yaml:"log"`

	// SessionStore configures the external session backend used by the
	// STICKY destination balancing algorithm. When absent, STICKY falls
	// back to WEIGHTED_CONSISTENT_HASH with a warning.
	SessionStore *SessionStoreConfig `yaml:"sessionStore,omitempty"`
}

// ControlPlaneConfig holds settings for the control plane process.
type ControlPlaneConfig struct {
	// Address is the host:port the HTTP API server listens on.
	// Default: ":8080"
	Address string `yaml:"address"`

	// XDSAddress is the host:port the xDS gRPC server listens on.
	// Envoy nodes connect here to receive configuration via ADS.
	// Default: ":18000"
	XDSAddress string `yaml:"xdsAddress"`

	// StorePath is the root directory for all control plane state. Vrata
	// places the bbolt database at <storePath>/vrata.db and, when Raft is
	// enabled, Raft logs and snapshots at <storePath>/raft/.
	// Default: "/data"
	StorePath string `yaml:"storePath"`

	// Raft enables multi-node HA replication via embedded Raft consensus.
	// When absent, the control plane runs in single-node mode.
	Raft *RaftConfig `yaml:"raft,omitempty"`
}

// BoltDBPath returns the path to the bbolt database file, derived from StorePath.
func (c ControlPlaneConfig) BoltDBPath() string {
	return filepath.Join(c.StorePath, "vrata.db")
}

// RaftDataDir returns the path to the Raft data directory, derived from StorePath.
func (c ControlPlaneConfig) RaftDataDir() string {
	return filepath.Join(c.StorePath, "raft")
}

// RaftConfig enables multi-node HA for the control plane via embedded
// Raft consensus. All nodes must have the same peers configuration.
type RaftConfig struct {
	// NodeID is the unique identifier for this node in the Raft cluster.
	// Must be unique across all nodes. Example: "cp-0".
	NodeID string `yaml:"nodeId"`

	// BindAddress is the host:port this node listens on for Raft
	// peer-to-peer communication. Example: ":7000".
	BindAddress string `yaml:"bindAddress"`

	// AdvertiseAddress is the host:port that other nodes use to reach
	// this node. Required when BindAddress uses 0.0.0.0 or has no host.
	// In Kubernetes, set this to the pod IP:port via the downward API.
	// Example: "${POD_IP}:7000"
	AdvertiseAddress string `yaml:"advertiseAddress"`

	// Peers is the explicit list of all cluster members, including this
	// node. Format: "nodeId=host:port". Used when DNS discovery is not
	// configured.
	// Example:
	//   - "cp-0=10.0.0.1:7000"
	//   - "cp-1=10.0.0.2:7000"
	//   - "cp-2=10.0.0.3:7000"
	Peers []string `yaml:"peers,omitempty"`

	// Discovery configures automatic peer discovery. When set, Peers is
	// ignored.
	Discovery *RaftDiscovery `yaml:"discovery,omitempty"`
}

// RaftDiscovery configures automatic peer discovery for the Raft cluster.
type RaftDiscovery struct {
	// DNS is a hostname that resolves to all cluster nodes via A or AAAA
	// records. Typical Kubernetes value is the headless Service FQDN:
	//   "vrata-headless.namespace.svc.cluster.local"
	// Vrata resolves this name periodically and uses the returned IPs
	// combined with BindAddress port as the peer list.
	DNS string `yaml:"dns"`
}

// ProxyConfig holds settings for proxy-mode instances that connect to a
// remote control plane to receive configuration via SSE.
type ProxyConfig struct {
	// ControlPlaneURL is the base URL of the control plane this proxy
	// connects to (e.g. "http://10.0.0.1:8080").
	ControlPlaneURL string `yaml:"controlPlaneUrl"`

	// ReconnectInterval is how long to wait before reconnecting after a
	// stream disconnection. Accepts Go duration strings. Default: "5s".
	ReconnectInterval string `yaml:"reconnectInterval"`
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

// SessionStoreType identifies the session store backend.
type SessionStoreType string

const (
	// SessionStoreRedis uses Redis as the session store backend.
	SessionStoreRedis SessionStoreType = "redis"
)

// SessionStoreConfig configures the external session store for STICKY sessions.
type SessionStoreConfig struct {
	// Type selects the backend. Currently only "redis".
	Type SessionStoreType `yaml:"type"`

	// Redis holds Redis-specific connection settings.
	// Only used when Type is "redis".
	Redis *RedisConfig `yaml:"redis,omitempty"`
}

// RedisConfig holds connection parameters for a Redis session store.
type RedisConfig struct {
	// Address is the Redis host:port. Default: "localhost:6379".
	Address string `yaml:"address"`

	// Password is the Redis AUTH password. Use ${REDIS_PASSWORD} for secrets.
	Password string `yaml:"password"`

	// DB is the Redis database number. Default: 0.
	DB int `yaml:"db"`
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
	if cfg.ControlPlane.Address == "" {
		cfg.ControlPlane.Address = ":8080"
	}
	if cfg.ControlPlane.XDSAddress == "" {
		cfg.ControlPlane.XDSAddress = ":18000"
	}
	if cfg.ControlPlane.StorePath == "" {
		cfg.ControlPlane.StorePath = "/data"
	}
	if cfg.Proxy.ReconnectInterval == "" {
		cfg.Proxy.ReconnectInterval = "5s"
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
	if cfg.ControlPlane.Raft != nil {
		r := cfg.ControlPlane.Raft
		if r.NodeID == "" {
			return fmt.Errorf("controlPlane.raft.nodeId is required when raft is configured")
		}
		if r.BindAddress == "" {
			return fmt.Errorf("controlPlane.raft.bindAddress is required when raft is configured")
		}
		hasDNS := r.Discovery != nil && r.Discovery.DNS != ""
		hasPeers := len(r.Peers) > 0
		if !hasDNS && !hasPeers {
			return fmt.Errorf("controlPlane.raft requires either discovery.dns or at least one peer")
		}
	}
	return nil
}
