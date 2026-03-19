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
proxy:
  controlPlaneUrl: "http://10.0.0.1:8080"
`
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != ModeProxy {
		t.Errorf("expected mode %q, got %q", ModeProxy, cfg.Mode)
	}
	if cfg.Proxy.ControlPlaneURL != "http://10.0.0.1:8080" {
		t.Errorf("expected controlPlaneUrl %q, got %q", "http://10.0.0.1:8080", cfg.Proxy.ControlPlaneURL)
	}
	if cfg.Proxy.ReconnectInterval != "5s" {
		t.Errorf("expected reconnectInterval %q, got %q", "5s", cfg.Proxy.ReconnectInterval)
	}
}

func TestValidateUnknownMode(t *testing.T) {
	cfg := &Config{Mode: "badmode"}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestValidateProxyModeRequiresURL(t *testing.T) {
	cfg := &Config{Mode: ModeProxy}
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error when proxy mode has no proxy.controlPlaneUrl")
	}
}

func TestValidateProxyModeWithURL(t *testing.T) {
	cfg := &Config{
		Mode: ModeProxy,
		Proxy: ProxyConfig{
			ControlPlaneURL: "http://localhost:8080",
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

func TestValidateRaftStaticPeers(t *testing.T) {
	cfg := &Config{
		Mode: ModeControlPlane,
		ControlPlane: ControlPlaneConfig{
			Raft: &RaftConfig{
				NodeID:      "cp-0",
				BindAddress: ":7000",
				Peers:       []string{"cp-0=10.0.0.1:7000"},
			},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRaftDNSDiscovery(t *testing.T) {
	cfg := &Config{
		Mode: ModeControlPlane,
		ControlPlane: ControlPlaneConfig{
			Raft: &RaftConfig{
				NodeID:      "cp-0",
				BindAddress: ":7000",
				Discovery:   &RaftDiscovery{DNS: "cp-headless.ns.svc.cluster.local"},
			},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRaftMissingNodeID(t *testing.T) {
	cfg := &Config{
		Mode: ModeControlPlane,
		ControlPlane: ControlPlaneConfig{
			Raft: &RaftConfig{
				BindAddress: ":7000",
				Peers:       []string{"cp-0=10.0.0.1:7000"},
			},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Error("expected error for missing nodeId")
	}
}

func TestValidateRaftMissingPeersAndDNS(t *testing.T) {
	cfg := &Config{
		Mode: ModeControlPlane,
		ControlPlane: ControlPlaneConfig{
			Raft: &RaftConfig{
				NodeID:      "cp-0",
				BindAddress: ":7000",
			},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Error("expected error when neither peers nor DNS is set")
	}
}

func TestBoltDBPath(t *testing.T) {
	cp := ControlPlaneConfig{StorePath: "/data"}
	if got := cp.BoltDBPath(); got != "/data/vrata.db" {
		t.Errorf("expected /data/vrata.db, got %s", got)
	}
}

func TestRaftDataDir(t *testing.T) {
	cp := ControlPlaneConfig{StorePath: "/data"}
	if got := cp.RaftDataDir(); got != "/data/raft" {
		t.Errorf("expected /data/raft, got %s", got)
	}
}
