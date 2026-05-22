package http_test

import (
	"context"
	"time"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/internal/session"
)

// TestUnifiedHTTPServer_WebSocketOption tests that WithWebSocket option registers handler.
func TestUnifiedHTTPServer_WebSocketOption(t *testing.T) {
	msgBus := bus.New(nil, nil)
	cfg := http.DefaultServerConfig()

	srv := http.NewServer(cfg, nil, nil, nil, nil, nil, http.WithWebSocket(msgBus))

	if srv == nil {
		t.Fatal("failed to create server with WebSocket option")
	}
}

// TestUnifiedHTTPServer_MCPOption tests that WithMCP option registers handler.
func TestUnifiedHTTPServer_MCPOption(t *testing.T) {
	svcRegistry := &services.ServiceRegistry{}
	cfg := http.DefaultServerConfig()

	srv := http.NewServer(cfg, nil, nil, nil, svcRegistry, nil, http.WithMCP(svcRegistry, "/mcp"))

	if srv == nil {
		t.Fatal("failed to create server with MCP option")
	}
}

// TestUnifiedHTTPServer_BothOptions tests enabling both WebSocket and MCP.
func TestUnifiedHTTPServer_BothOptions(t *testing.T) {
	msgBus := bus.New(nil, nil)
	sessionStore := session.NewMemoryStore(nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: sessionStore,
	}
	cfg := http.DefaultServerConfig()

	srv := http.NewServer(cfg, nil, nil, nil, svcRegistry, nil,
		http.WithWebSocket(msgBus),
		http.WithMCP(svcRegistry, "/mcp"),
	)

	if srv == nil {
		t.Fatal("failed to create server with both options")
	}
}

// TestUnifiedHTTPServer_ContextCancellation tests graceful shutdown.
func TestUnifiedHTTPServer_ContextCancellation(t *testing.T) {
	cfg := http.DefaultServerConfig()
	cfg.Addr = ":0" // Let OS choose available port

	srv := http.NewServer(cfg, nil, nil, nil, nil, nil)
	if srv == nil {
		t.Fatal("failed to create server")
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Logf("server shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not shutdown within timeout")
	}
}
