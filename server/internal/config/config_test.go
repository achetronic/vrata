package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("{}"), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != ModeControlPlane {
		t.Errorf("expected mode %q, got %q", ModeControlPlane, cfg.Mode)
	}
}

func TestLoadProxyMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
mode: proxy
controlPlane:
  address: "http://10.0.0.1:8080"
`
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != ModeProxy {
		t.Errorf("expected mode %q, got %q", ModeProxy, cfg.Mode)
	}
	if cfg.ControlPlane.Address != "http://10.0.0.1:8080" {
		t.Errorf("expected address %q, got %q", "http://10.0.0.1:8080", cfg.ControlPlane.Address)
	}
	if cfg.ControlPlane.ReconnectInterval != "5s" {
		t.Errorf("expected reconnectInterval %q, got %q", "5s", cfg.ControlPlane.ReconnectInterval)
	}
}

func TestValidateUnknownMode(t *testing.T) {
	cfg := &Config{Mode: "badmode"}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestValidateProxyModeRequiresAddress(t *testing.T) {
	cfg := &Config{Mode: ModeProxy}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error when proxy mode has no controlPlane.address")
	}
}

func TestValidateProxyModeWithAddress(t *testing.T) {
	cfg := &Config{
		Mode: ModeProxy,
		ControlPlane: ControlPlaneConfig{
			Address: "http://localhost:8080",
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateControlPlaneMode(t *testing.T) {
	cfg := &Config{Mode: ModeControlPlane}
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateClusterStaticPeers(t *testing.T) {
	cfg := &Config{
		Mode: ModeControlPlane,
		Cluster: &ClusterConfig{
			NodeID:      "cp-0",
			BindAddress: ":7000",
			Peers:       []string{"cp-0=10.0.0.1:7000"},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateClusterDNSDiscovery(t *testing.T) {
	cfg := &Config{
		Mode: ModeControlPlane,
		Cluster: &ClusterConfig{
			NodeID:      "cp-0",
			BindAddress: ":7000",
			Discovery:   &ClusterDiscovery{DNS: "cp-headless.ns.svc.cluster.local"},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateClusterMissingNodeID(t *testing.T) {
	cfg := &Config{
		Mode: ModeControlPlane,
		Cluster: &ClusterConfig{
			BindAddress: ":7000",
			Peers:       []string{"cp-0=10.0.0.1:7000"},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Error("expected error for missing nodeId")
	}
}

func TestValidateClusterMissingPeersAndDNS(t *testing.T) {
	cfg := &Config{
		Mode: ModeControlPlane,
		Cluster: &ClusterConfig{
			NodeID:      "cp-0",
			BindAddress: ":7000",
		},
	}
	if err := Validate(cfg); err == nil {
		t.Error("expected error when neither peers nor DNS is set")
	}
}
