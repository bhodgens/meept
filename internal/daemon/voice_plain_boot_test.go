package daemon

// Boot tests for the plain-listener wiring (audit HIGH: the knob was
// dead). These drive VoiceWiring.startPlainListener directly with a
// hand-built wiring — the hook itself is the unit under test; the full
// WireVoice construction path is pinned by voice_wiring_test.go.

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/config"
)

func plainBootWiring(t *testing.T, addr string) *VoiceWiring {
	t.Helper()
	routes, err := commhttp.NewVoiceRoutes(nil, commhttp.VerifyFuncs{}, commhttp.VoiceRoutesConfig{Enabled: false}, nil)
	if err != nil {
		t.Fatalf("NewVoiceRoutes: %v", err)
	}
	return &VoiceWiring{
		cfg:    config.VoiceConfig{LoopbackPlainAddr: addr},
		Routes: routes,
		logger: slog.New(slog.DiscardHandler),
	}
}

// freeLoopbackAddr returns a 127.0.0.1:port that is free at check time.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("probe close: %v", err)
	}
	return addr
}

func TestPlainBootEmptyAddrIsNoop(t *testing.T) {
	w := plainBootWiring(t, "")
	if err := w.startPlainListener(context.Background()); err != nil {
		t.Fatalf("empty addr: err = %v, want nil (knob off)", err)
	}
}

func TestPlainBootNonLoopbackFailsLoudly(t *testing.T) {
	w := plainBootWiring(t, "0.0.0.0:1")
	err := w.startPlainListener(context.Background())
	if !errors.Is(err, commhttp.ErrNonLoopbackVoiceAddr) {
		t.Fatalf("non-loopback addr: err = %v, want ErrNonLoopbackVoiceAddr wrapping", err)
	}
}

func TestPlainBootNilRoutesWithAddrFails(t *testing.T) {
	w := &VoiceWiring{
		cfg:    config.VoiceConfig{LoopbackPlainAddr: freeLoopbackAddr(t)},
		logger: slog.New(slog.DiscardHandler),
	}
	if err := w.startPlainListener(context.Background()); err == nil {
		t.Fatal("addr set with nil routes must fail the boot")
	}
}

func TestPlainBootArmsAndShutsDownWithContext(t *testing.T) {
	addr := freeLoopbackAddr(t)
	w := plainBootWiring(t, addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	if err := w.startPlainListener(ctx); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("boot blocked %s; must return once the listener is armed", elapsed)
	}

	// The listener is actually serving: probe a non-voice path -> 404
	// (mux alive), not connection refused.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/v1/health")
	if err != nil {
		t.Fatalf("plain probe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("plain probe: status = %d, want 404", resp.StatusCode)
	}

	// Cancelling the subsystem context shuts it down.
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			_ = ln.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plain listener port never released on ctx cancel")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
