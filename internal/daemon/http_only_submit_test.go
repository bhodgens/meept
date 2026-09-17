package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// Week bughunt 2026-09-17 Group 1 pin (finding 17): POST /api/v1/chat/submit
// must be served when the Unix RPC transport is disabled. Pre-fix the shared
// submit handler was constructed only inside the `rpcServer != nil && proxy
// != nil` gate in daemon.go, so an HTTP-only daemon answered 503 — the route
// was registered but its submitter was always nil.
//
// Part 1 drives the daemon's real composition: an RPC-disabled config must
// produce an HTTP server whose chat submitter is non-nil. Part 2 proves the
// handler wired by the HTTP-only path serves a live ack (never 503) through
// the same Submit seam the HTTP endpoint calls.
func TestHTTPOnlySubmitConfigServesChatSubmit(t *testing.T) {
	// --- Part 1: composition with RPC disabled exposes a chat submitter ---
	tmpDir := t.TempDir()
	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 2 * time.Second,
		ModelsConfig:    createTestModelsConfig(),
		FullConfig:      config.DefaultConfig(),
	}
	// HTTP-only: Unix RPC transport disabled.
	cfg.FullConfig.Transport.RPC.Enabled = false
	cfg.FullConfig.Transport.HTTP.Enabled = true
	cfg.FullConfig.Transport.HTTP.Addr = "127.0.0.1:0"

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("daemon New: %v", err)
	}
	if d.httpServer == nil {
		t.Fatal("HTTP-only daemon produced no HTTP server")
	}
	submitter := d.httpServer.ChatSubmitter()
	if submitter == nil {
		t.Fatal("chat submitter is nil with RPC disabled — POST /api/v1/chat/submit would 503 (finding 17)")
	}

	// --- Part 2: the submitter serves a live ack through its HTTP seam ---
	params, err := json.Marshal(map[string]any{
		"message":    "http-only submit",
		"session_id": "sess-g1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := submitter.Submit(t.Context(), params)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ackMap, ok := ack.(map[string]any)
	if !ok {
		t.Fatalf("ack = %T, want map", ack)
	}
	if ackMap["accepted"] != true {
		t.Errorf("accepted = %v, want true", ackMap["accepted"])
	}
	if turnID, _ := ackMap["turn_id"].(string); turnID == "" {
		t.Error("turn_id must be minted by the HTTP-only submit handler")
	}
}
