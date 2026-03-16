package config

import (
	"os"
	"testing"
)

func TestSessionStoreFromEnv(t *testing.T) {
	os.Setenv("SESSION_STORE_TYPE", "redis")
	os.Setenv("REDIS_ADDRESS", "localhost:6379")
	defer os.Unsetenv("SESSION_STORE_TYPE")
	defer os.Unsetenv("REDIS_ADDRESS")

	cfg, err := Load("../../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionStore == nil {
		t.Fatal("SessionStore is nil")
	}
	if cfg.SessionStore.Type != SessionStoreRedis {
		t.Errorf("expected %q, got %q", SessionStoreRedis, cfg.SessionStore.Type)
	}
	if cfg.SessionStore.Redis == nil {
		t.Fatal("Redis config is nil")
	}
	if cfg.SessionStore.Redis.Address != "localhost:6379" {
		t.Errorf("expected localhost:6379, got %q", cfg.SessionStore.Redis.Address)
	}
}

func TestSessionStoreEmpty(t *testing.T) {
	os.Unsetenv("SESSION_STORE_TYPE")
	os.Unsetenv("REDIS_ADDRESS")
	os.Unsetenv("REDIS_PASSWORD")

	cfg, err := Load("../../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionStore != nil && cfg.SessionStore.Type != "" {
		t.Errorf("expected no session store type, got %q", cfg.SessionStore.Type)
	}
}
