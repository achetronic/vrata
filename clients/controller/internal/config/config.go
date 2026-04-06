// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package config handles loading and validating the controller configuration file.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all runtime configuration for the controller.
type Config struct {
	// ControlPlaneURL is the base URL of the Vrata control plane REST API
	// (e.g. "http://localhost:8080").
	ControlPlaneURL string `yaml:"controlPlaneUrl"`

	// TLS configures TLS for the connection to the control plane.
	// Cert and Key enable mutual TLS (the controller presents a client cert).
	// CA is the CA bundle used to verify the control plane server cert.
	// When absent, plain HTTP or system CA defaults are used.
	TLS *TLSConfig `yaml:"tls,omitempty"`

	// APIKey is the bearer token sent in the Authorization header on every
	// request to the control plane. When empty, no auth header is sent.
	APIKey string `yaml:"apiKey,omitempty"`

	// Watch controls which Kubernetes resources the controller observes.
	Watch WatchConfig `yaml:"watch"`

	// Snapshot controls batching of changes before creating a Vrata snapshot.
	Snapshot SnapshotConfig `yaml:"snapshot"`

	// Duplicates controls duplicate route detection behaviour.
	Duplicates DuplicatesConfig `yaml:"duplicates"`

	// Log controls logging format and verbosity.
	Log LogConfig `yaml:"log"`

	// LeaderElection controls leader election for running multiple replicas.
	// When enabled, only the leader replica reconciles. Others wait.
	LeaderElection LeaderElectionConfig `yaml:"leaderElection"`

	// Metrics controls Prometheus metrics exposition.
	Metrics MetricsConfig `yaml:"metrics"`
}

// WatchConfig controls which Kubernetes resources are watched.
type WatchConfig struct {
	// Namespaces restricts the watch to specific namespaces.
	// An empty list means all namespaces.
	Namespaces []string `yaml:"namespaces,omitempty"`

	// HTTPRoutes enables watching standard Gateway API HTTPRoute resources.
	// Default: true.
	HTTPRoutes *bool `yaml:"httpRoutes,omitempty"`

	// GRPCRoutes enables watching standard Gateway API GRPCRoute resources.
	// Default: true.
	GRPCRoutes *bool `yaml:"grpcRoutes,omitempty"`

	// SuperHTTPRoutes enables watching SuperHTTPRoute resources (HTTPRoute
	// without maxItems limits). Default: false.
	SuperHTTPRoutes *bool `yaml:"superHttpRoutes,omitempty"`

	// Gateways enables watching Gateway resources to sync Listeners.
	// Default: true.
	Gateways *bool `yaml:"gateways,omitempty"`

	// GatewayClassName restricts the controller to only reconcile Gateways
	// whose spec.gatewayClassName matches this value. When empty, the
	// controller reconciles all Gateways. Default: "vrata".
	GatewayClassName string `yaml:"gatewayClassName,omitempty"`
}

// SnapshotConfig controls snapshot batching.
type SnapshotConfig struct {
	// Debounce is how long to wait after the last change before creating
	// and activating a snapshot. Default: "5s".
	Debounce string `yaml:"debounce"`

	// MaxBatch is the maximum number of accumulated changes before forcing
	// a snapshot even if changes keep arriving. Default: 100.
	MaxBatch int `yaml:"maxBatch"`

	// BatchIdleTimeout is how long to wait after the last HTTPRoute belonging
	// to a vrata.io/batch group arrives before considering the group complete
	// and creating a snapshot. The timeout resets on every new member arrival.
	// Default: "10s".
	BatchIdleTimeout string `yaml:"batchIdleTimeout"`

	// BatchIncompletePolicy controls what happens when a batch group with a
	// known vrata.io/batch-size times out before all members arrive.
	// "apply" (default): log an error and create the snapshot with whatever
	//   members arrived — the proxy gets partially-applied config.
	// "reject": discard the incomplete batch entirely, log an error, and do
	//   not create a snapshot — the operator must re-deploy to converge.
	// This setting only takes effect when both vrata.io/batch and
	// vrata.io/batch-size annotations are present. Without batch-size, the
	// controller cannot detect incomplete batches and always applies.
	BatchIncompletePolicy BatchIncompletePolicy `yaml:"batchIncompletePolicy"`
}

// BatchIncompletePolicy controls the behaviour when a batch group with a known
// expected size times out before all members arrive.
type BatchIncompletePolicy string

const (
	// BatchIncompletePolicyApply logs an error and creates the snapshot with
	// the members that arrived. This is the default.
	BatchIncompletePolicyApply BatchIncompletePolicy = "apply"

	// BatchIncompletePolicyReject discards the incomplete batch and does not
	// create a snapshot. The operator must re-deploy to converge.
	BatchIncompletePolicyReject BatchIncompletePolicy = "reject"
)

// DuplicateMode controls what happens when overlapping routes are detected.
type DuplicateMode string

const (
	// DuplicateModeOff disables overlap detection entirely.
	DuplicateModeOff DuplicateMode = "off"

	// DuplicateModeWarn logs a warning but syncs the route anyway.
	DuplicateModeWarn DuplicateMode = "warn"

	// DuplicateModeReject logs a warning and skips the overlapping route.
	// The HTTPRoute status is set to Accepted: False with reason OverlappingRoute.
	DuplicateModeReject DuplicateMode = "reject"
)

// DuplicatesConfig controls duplicate/overlapping route detection.
type DuplicatesConfig struct {
	// Mode controls the behaviour when overlapping routes are detected.
	// "off" = disabled, "warn" = log and sync, "reject" = log and skip.
	// Default: "warn".
	Mode DuplicateMode `yaml:"mode"`
}

// LogConfig controls logging format and verbosity.
type LogConfig struct {
	// Format is either "console" (human-readable text) or "json".
	// Default: "console".
	Format string `yaml:"format"`

	// Level is the minimum log level: "debug", "info", "warn", or "error".
	// Default: "info".
	Level string `yaml:"level"`
}

// LeaderElectionConfig controls leader election for HA deployments.
type LeaderElectionConfig struct {
	// Enabled activates leader election. When false (default), the controller
	// assumes it is the only replica and reconciles immediately.
	Enabled bool `yaml:"enabled"`

	// LeaseName is the name of the Lease resource used for leader election.
	// Default: "vrata-controller-leader".
	LeaseName string `yaml:"leaseName"`

	// LeaseNamespace is the namespace where the Lease is created.
	// Default: "default".
	LeaseNamespace string `yaml:"leaseNamespace"`

	// LeaseDuration is how long a leader holds the lease. Default: "15s".
	LeaseDuration string `yaml:"leaseDuration"`

	// RenewDeadline is how long the leader waits before renewing. Default: "10s".
	RenewDeadline string `yaml:"renewDeadline"`

	// RetryPeriod is how often non-leaders retry acquiring the lease. Default: "2s".
	RetryPeriod string `yaml:"retryPeriod"`
}

// MetricsConfig controls Prometheus metrics exposition.
type MetricsConfig struct {
	// Enabled activates Prometheus metrics. Default: false.
	Enabled bool `yaml:"enabled"`

	// Address is the host:port the metrics server listens on.
	// Default: ":9090".
	Address string `yaml:"address"`
}

// WatchHTTPRoutes returns whether HTTPRoute watching is enabled.
func (c *Config) WatchHTTPRoutes() bool {
	if c.Watch.HTTPRoutes == nil {
		return true
	}
	return *c.Watch.HTTPRoutes
}

// WatchSuperHTTPRoutes returns whether SuperHTTPRoute watching is enabled.
func (c *Config) WatchSuperHTTPRoutes() bool {
	if c.Watch.SuperHTTPRoutes == nil {
		return false
	}
	return *c.Watch.SuperHTTPRoutes
}

// WatchGRPCRoutes returns whether GRPCRoute watching is enabled.
func (c *Config) WatchGRPCRoutes() bool {
	if c.Watch.GRPCRoutes == nil {
		return true
	}
	return *c.Watch.GRPCRoutes
}

// WatchGateways returns whether Gateway watching is enabled.
func (c *Config) WatchGateways() bool {
	if c.Watch.Gateways == nil {
		return true
	}
	return *c.Watch.Gateways
}

// GatewayClass returns the configured gatewayClassName filter.
func (c *Config) GatewayClass() string {
	return c.Watch.GatewayClassName
}

// DuplicatesMode returns the configured duplicate detection mode.
func (c *Config) DuplicatesMode() DuplicateMode {
	if c.Duplicates.Mode == "" {
		return DuplicateModeWarn
	}
	return c.Duplicates.Mode
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
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}
	return &cfg, nil
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.ControlPlaneURL == "" {
		cfg.ControlPlaneURL = "http://localhost:8080"
	}
	if cfg.Snapshot.Debounce == "" {
		cfg.Snapshot.Debounce = "5s"
	}
	if cfg.Snapshot.MaxBatch == 0 {
		cfg.Snapshot.MaxBatch = 100
	}
	if cfg.Snapshot.BatchIdleTimeout == "" {
		cfg.Snapshot.BatchIdleTimeout = "10s"
	}
	if cfg.Snapshot.BatchIncompletePolicy == "" {
		cfg.Snapshot.BatchIncompletePolicy = BatchIncompletePolicyApply
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "console"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.LeaderElection.LeaseName == "" {
		cfg.LeaderElection.LeaseName = "vrata-controller-leader"
	}
	if cfg.LeaderElection.LeaseNamespace == "" {
		cfg.LeaderElection.LeaseNamespace = "default"
	}
	if cfg.LeaderElection.LeaseDuration == "" {
		cfg.LeaderElection.LeaseDuration = "15s"
	}
	if cfg.LeaderElection.RenewDeadline == "" {
		cfg.LeaderElection.RenewDeadline = "10s"
	}
	if cfg.LeaderElection.RetryPeriod == "" {
		cfg.LeaderElection.RetryPeriod = "2s"
	}
	if cfg.Metrics.Address == "" {
		cfg.Metrics.Address = ":9090"
	}
	if cfg.Watch.GatewayClassName == "" {
		cfg.Watch.GatewayClassName = "vrata"
	}
}

// validate checks that the configuration is internally consistent.
func validate(cfg *Config) error {
	if cfg.ControlPlaneURL == "" {
		return fmt.Errorf("controlPlaneUrl is required")
	}
	if cfg.TLS != nil {
		if cfg.TLS.Cert != "" && cfg.TLS.Key == "" {
			return fmt.Errorf("tls: key is required when cert is set")
		}
		if cfg.TLS.Key != "" && cfg.TLS.Cert == "" {
			return fmt.Errorf("tls: cert is required when key is set")
		}
	}
	for _, entry := range []struct {
		name, value string
	}{
		{"snapshot.debounce", cfg.Snapshot.Debounce},
		{"snapshot.batchIdleTimeout", cfg.Snapshot.BatchIdleTimeout},
		{"leaderElection.leaseDuration", cfg.LeaderElection.LeaseDuration},
		{"leaderElection.renewDeadline", cfg.LeaderElection.RenewDeadline},
		{"leaderElection.retryPeriod", cfg.LeaderElection.RetryPeriod},
	} {
		if entry.value != "" {
			if _, err := time.ParseDuration(entry.value); err != nil {
				return fmt.Errorf("%s: invalid duration %q: %w", entry.name, entry.value, err)
			}
		}
	}
	return nil
}

// TLSConfig holds TLS parameters for the connection to the control plane.
type TLSConfig struct {
	// Cert is the PEM-encoded client certificate for mTLS.
	Cert string `yaml:"cert,omitempty"`

	// Key is the PEM-encoded private key matching Cert.
	Key string `yaml:"key,omitempty"`

	// CA is the PEM-encoded CA bundle to verify the control plane server cert.
	CA string `yaml:"ca,omitempty"`
}
