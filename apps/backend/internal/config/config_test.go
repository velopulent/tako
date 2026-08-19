package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[server]\nunknown = \"value\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, false); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestMonitoringAndAdminDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	payload := "[monitoring]\ndefault_interval = \"5s\"\nhistory_retention = \"6h\"\n[admin]\nidle_timeout = \"10m\"\n"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MonitoringInterval != 5*time.Second || cfg.HistoryRetention != 6*time.Hour || cfg.AdminIdleTimeout != 10*time.Minute {
		t.Fatalf("unexpected durations: %+v", cfg)
	}
}

func TestDevelopmentDefaultsUseLoopback(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "127.0.0.1:9090" || !cfg.Development {
		t.Fatalf("unsafe development defaults: %#v", cfg)
	}
}

func TestAllowedOriginAcceptsCommaSeparatedList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	payload := "[server]\nallowed_origin = \"https://localhost:9090, https://127.0.0.1:9090\"\n"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://localhost:9090" || cfg.AllowedOrigins[1] != "https://127.0.0.1:9090" {
		t.Fatalf("origins=%q", cfg.AllowedOrigins)
	}
}

func TestRejectsUnsafeMonitoringInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[monitoring]\ndefault_interval = \"100ms\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, false); err == nil {
		t.Fatal("unsafe interval accepted")
	}
}
