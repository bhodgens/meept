package transport

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Transport != "rpc" {
		t.Errorf("DefaultConfig.Transport = %q, want %q", cfg.Transport, "rpc")
	}
	if cfg.SocketPath != "~/.meept/meept.sock" {
		t.Errorf("DefaultConfig.SocketPath = %q, want %q", cfg.SocketPath, "~/.meept/meept.sock")
	}
	if cfg.HTTPBaseURL != "https://localhost:8081" {
		t.Errorf("DefaultConfig.HTTPBaseURL = %q, want %q", cfg.HTTPBaseURL, "https://localhost:8081")
	}
	if cfg.Timeout != 120*time.Second {
		t.Errorf("DefaultConfig.Timeout = %v, want %v", cfg.Timeout, 120*time.Second)
	}
}

func TestNew_NilConfig(t *testing.T) {
	client, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) returned error: %v", err)
	}
	if client == nil {
		t.Fatal("New(nil) returned nil client")
	}
	// Default transport should be RPC; verify type by checking interface
	_ = client.IsConnected() // Not connected yet, which is expected
	client.Close()
}

func TestNew_RPCTransport(t *testing.T) {
	tests := []string{"rpc", "unix", "socket"}
	for _, transportName := range tests {
		t.Run(transportName, func(t *testing.T) {
			cfg := &Config{
				Transport:  transportName,
				SocketPath: "/tmp/test-meept.sock",
				Timeout:    5 * time.Second,
			}
			client, err := New(cfg)
			if err != nil {
				t.Fatalf("New(%q) returned error: %v", transportName, err)
			}
			if client == nil {
				t.Fatal("New() returned nil client")
			}
			client.Close()
		})
	}
}

func TestNew_HTTPTransport(t *testing.T) {
	cfg := &Config{
		Transport:   "http",
		HTTPBaseURL: "http://localhost:9999",
		Timeout:     5 * time.Second,
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New(http) returned error: %v", err)
	}
	if client == nil {
		t.Fatal("New() returned nil client")
	}
	client.Close()
}

func TestNew_UnknownTransport(t *testing.T) {
	cfg := &Config{
		Transport:   "websocket",
		HTTPBaseURL: "http://localhost:9999",
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("New(unknown transport) should return error")
	}
}

func TestConfigFields(t *testing.T) {
	cfg := &Config{
		Transport:   "http",
		SocketPath:  "/custom/path.sock",
		HTTPBaseURL: "http://example.com:9090",
		Timeout:     30 * time.Second,
	}

	if cfg.Transport != "http" {
		t.Errorf("Config.Transport = %q, want %q", cfg.Transport, "http")
	}
	if cfg.SocketPath != "/custom/path.sock" {
		t.Errorf("Config.SocketPath = %q, want %q", cfg.SocketPath, "/custom/path.sock")
	}
	if cfg.HTTPBaseURL != "http://example.com:9090" {
		t.Errorf("Config.HTTPBaseURL = %q, want %q", cfg.HTTPBaseURL, "http://example.com:9090")
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Config.Timeout = %v, want %v", cfg.Timeout, 30*time.Second)
	}
}
