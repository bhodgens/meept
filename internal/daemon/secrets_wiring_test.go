package daemon

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestWireSecretsProxy_DisabledIsNoOp verifies the nil-safe no-op path when
// [secrets.proxy] is disabled (the default).
func TestWireSecretsProxy_DisabledIsNoOp(t *testing.T) {
	c := &Components{
		Config: &config.Config{},
		Logger: discardLogger(),
	}
	if err := c.wireSecretsProxy(context.Background()); err != nil {
		t.Fatalf("disabled proxy must not error, got %v", err)
	}
	if c.SecretBroker != nil || c.SecretsProxy != nil {
		t.Fatal("disabled proxy must leave SecretBroker/SecretsProxy nil")
	}
	if got := c.SecretsProxyStatus(); got != nil {
		t.Fatalf("status must be nil when disabled, got %v", got)
	}
}

// TestWireSecretsProxy_EnabledWiresBrokerAndAddr verifies the leaf contract:
// when enabled, Components carries a loaded broker and a started proxy whose
// bound loopback address is exposed for profile wiring.
func TestWireSecretsProxy_EnabledWiresBrokerAndAddr(t *testing.T) {
	t.Setenv("MEEPT_TEST_WIRE_TOKEN", "wired-value")
	c := &Components{
		Config: &config.Config{
			Secrets: config.SecretsConfig{
				Sources: config.SecretSources{
					"tok": {Kind: "env", Name: "MEEPT_TEST_WIRE_TOKEN", Hosts: []string{"api.test"}},
				},
				Proxy: config.SecretsProxyConfig{Enabled: true},
			},
		},
		Logger: discardLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.wireSecretsProxy(ctx); err != nil {
		t.Fatalf("wireSecretsProxy failed: %v", err)
	}

	if c.SecretBroker == nil {
		t.Fatal("SecretBroker must be wired when enabled")
	}
	if c.SecretsProxy == nil {
		t.Fatal("SecretsProxy must be wired and started when enabled")
	}
	addr := c.SecretsProxy.Addr()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("bound addr %q must be loopback ephemeral", addr)
	}

	st := c.SecretsProxyStatus()
	if st == nil {
		t.Fatal("status surface must report the running proxy")
	}
	if st["addr"] != addr {
		t.Fatalf("status addr = %v, want %v", st["addr"], addr)
	}
	if _, ok := st["leak_attempts"]; !ok {
		t.Fatal("status must include leak_attempts counter")
	}
}

// TestWireSecretsProxy_BrokerFailureFailsSoft verifies a broken source
// degrades to log-only (nil components) instead of aborting startup —
// matching the shadow-manager precedent.
func TestWireSecretsProxy_BrokerFailureFailsSoft(t *testing.T) {
	c := &Components{
		Config: &config.Config{
			Secrets: config.SecretsConfig{
				Sources: config.SecretSources{
					"gone": {Kind: "env", Name: "MEEPT_TEST_MISSING_ENV_VAR_XYZ"},
				},
				Proxy: config.SecretsProxyConfig{Enabled: true},
			},
		},
		Logger: discardLogger(),
	}
	if err := c.wireSecretsProxy(context.Background()); err != nil {
		t.Fatalf("broker failure must fail soft, got %v", err)
	}
	if c.SecretBroker != nil || c.SecretsProxy != nil {
		t.Fatal("failed broker load must leave both components nil")
	}
}

// TestWireSecretsProxy_NonLoopbackListenErrors verifies an explicitly
// misconfigured listen address surfaces as a hard wiring error.
func TestWireSecretsProxy_NonLoopbackListenErrors(t *testing.T) {
	t.Setenv("MEEPT_TEST_WIRE_TOKEN2", "v")
	c := &Components{
		Config: &config.Config{
			Secrets: config.SecretsConfig{
				Sources: config.SecretSources{
					"tok": {Kind: "env", Name: "MEEPT_TEST_WIRE_TOKEN2"},
				},
				Proxy: config.SecretsProxyConfig{Enabled: true, Listen: "0.0.0.0:8080"},
			},
		},
		Logger: discardLogger(),
	}
	err := c.wireSecretsProxy(context.Background())
	if err == nil {
		t.Fatal("non-loopback listen must return an error")
	}
	if c.SecretsProxy != nil {
		t.Fatal("failed start must leave SecretsProxy nil")
	}
}
