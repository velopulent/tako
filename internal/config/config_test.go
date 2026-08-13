package config

import (
	"os"
	"path/filepath"
	"testing"
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

func TestDevelopmentDefaultsUseLoopback(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "127.0.0.1:9090" || !cfg.Development {
		t.Fatalf("unsafe development defaults: %#v", cfg)
	}
}
