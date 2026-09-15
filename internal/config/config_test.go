package config

import (
	"os"
	"strings"
	"testing"
)

func TestCreateDefaultConfigUsesLoopbackHost(t *testing.T) {
	configHome := t.TempDir()
	dataHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	if err := CreateDefaultConfig(); err != nil {
		t.Fatalf("CreateDefaultConfig() error = %v", err)
	}

	data, err := os.ReadFile(Default().ConfigPath())
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if !strings.Contains(string(data), "host: 127.0.0.1") {
		t.Fatalf("generated config does not bind to loopback:\n%s", data)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Server.Host != "127.0.0.1" {
		t.Fatalf("Load().Server.Host = %q, want 127.0.0.1", loaded.Server.Host)
	}
}

func TestLoadAddsHostToLegacyConfig(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := Default()
	if err := os.MkdirAll(cfg.configDir, 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(cfg.ConfigPath(), []byte("server:\n  port: 5555\n"), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Server.Host != "127.0.0.1" {
		t.Fatalf("Load().Server.Host = %q, want 127.0.0.1", loaded.Server.Host)
	}
}
