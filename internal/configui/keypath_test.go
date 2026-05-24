package configui

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestGetKeypathString(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Daemon.LogLevel = "debug"

	val, err := GetKeypath(cfg, "daemon.log_level")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "debug" {
		t.Errorf("expected debug, got %s", val)
	}
}

func TestGetKeypathBool(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.RPC.Enabled = false

	val, err := GetKeypath(cfg, "transport.rpc.enabled")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "false" {
		t.Errorf("expected false, got %s", val)
	}
}

func TestGetKeypathInt(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Queue.MaxRetries = 5

	val, err := GetKeypath(cfg, "queue.max_retries")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "5" {
		t.Errorf("expected 5, got %s", val)
	}
}

func TestSetKeypathString(t *testing.T) {
	cfg := config.DefaultConfig()
	err := SetKeypath(cfg, "daemon.log_level", "warn")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Daemon.LogLevel != "warn" {
		t.Errorf("expected warn, got %s", cfg.Daemon.LogLevel)
	}
}

func TestSetKeypathBool(t *testing.T) {
	cfg := config.DefaultConfig()
	err := SetKeypath(cfg, "transport.rpc.enabled", "false")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport.RPC.Enabled {
		t.Error("expected false")
	}
}

func TestSetKeypathNotFound(t *testing.T) {
	cfg := config.DefaultConfig()
	err := SetKeypath(cfg, "nonexistent.field", "value")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}
