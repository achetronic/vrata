// Package config handles loading and validating the Vrata configuration file.
package config

import (
	"fmt"
	"os"

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

	// Server contains the REST API HTTP server settings.
	// Only used in controlplane mode.
	Server ServerConfig `yaml:"server"`

	// ControlPlane holds settings for connecting to a remote control plane.
	// Only used in proxy mode.
	ControlPlane ControlPlaneConfig `yaml:"controlPlane"`

	// Cluster enables Raft-based HA replication across multiple control
	// plane instances. When absent, the control plane runs in single-node
	// mode with a local bbolt database.
	Cluster *ClusterConfig `yaml:"cluster,omitempty"`

	// Log controls the logging behaviour.
	Log LogConfig `yaml:"log"`

	// SessionStore configures the external session backend used by the
	// STICKY destination balancing algorithm. When absent, STICKY falls
	// back to WEIGHTED_CONSISTENT_HASH with a warning.
	SessionStore *SessionStoreConfig `yaml:"sessionStore,omitempty"`
}

// ClusterConfig enables multi-node HA for the control plane via embedded
// Raft consensus. All nodes must have the same peers configuration.
type ClusterConfig struct {
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

	// DataDir is the directory where Raft logs and snapshots are stored.
	// Default: "/data/raft"
	DataDir string `yaml:"dataDir"`

	// Peers is the explicit list of all cluster members, including this node.
	// Format: "nodeId=host:port". Used when DNS discovery is not configured.
	// Example: ["cp-0=10.0.0.1:7000", "cp-1=10.0.0.2:7000", "cp-2=10.0.0.3:7000"]
	Peers []string `yaml:"peers"`

	// Discovery configures automatic peer discovery. When set, Peers is
	// ignored. When absent, Peers is used.
	Discovery *ClusterDiscovery `yaml:"discovery,omitempty"`
}

// ClusterDiscovery configures automatic peer discovery for the Raft cluster.
// Only one discovery method may be set at a time.
type ClusterDiscovery struct {
	// DNS is a hostname that resolves to all cluster nodes via A or AAAA
	// records. Typical value in Kubernetes is the headless Service FQDN:
	// "vrata-headless.namespace.svc.cluster.local".
	// Vrata resolves this name periodically and uses the returned IPs
	// combined with BindAddress port as the peer list.
	DNS string `yaml:"dns"`
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
	if cfg.Cluster != nil && cfg.Cluster.DataDir == "" {
		cfg.Cluster.DataDir = "/data/raft"
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
	if cfg.Cluster != nil {
		if cfg.Cluster.NodeID == "" {
			return fmt.Errorf("cluster.nodeId is required when cluster is configured")
		}
		if cfg.Cluster.BindAddress == "" {
			return fmt.Errorf("cluster.bindAddress is required when cluster is configured")
		}
		hasDNS := cfg.Cluster.Discovery != nil && cfg.Cluster.Discovery.DNS != ""
		hasPeers := len(cfg.Cluster.Peers) > 0
		if !hasDNS && !hasPeers {
			return fmt.Errorf("cluster requires either discovery.dns or at least one peer")
		}
	}
	return nil
}
