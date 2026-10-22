package daemon

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
)

// TestWireMemoryCaptureNilRecorderNoPanic pins the typed-nil wiring defect
// observed live 2026-10-22: with memory.l0_enabled=false, components.go
// passes a nil *l0.Recorder; WireMemoryCapture stored it into the
// l0Appender interface field, boxing nil-as-non-nil, so every captured
// turn panicked in (*Recorder).Append (nil receiver deref). Capture must
// now treat a nil recorder as "L0 disabled" and complete without panic.
func TestWireMemoryCaptureNilRecorderNoPanic(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewSQLiteStore(filepath.Join(dir, "sessions.db"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Create("test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessages(sess.ID, []session.Message{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, nil))

	b := bus.New(nil, slog.New(slog.DiscardHandler))
	// nil recorder (L0 disabled), nil enqueuer, nil extractor: the live
	// installed-config shape.
	if sub := WireMemoryCapture(b, store, nil, nil, nil, logger); sub == nil {
		t.Fatal("WireMemoryCapture returned nil subscriber")
	}

	bus.PublishT(b, agent.TopicTurnTerminal, "test", agent.TurnTerminalEvent{
		ConversationID: sess.ID,
		SessionID:      sess.ID,
		TurnID:         "turn-1",
		Status:         "completed",
		Reply:          "hi there",
	})
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	logged := buf.String()
	mu.Unlock()
	if strings.Contains(logged, "panicked") {
		t.Fatalf("memory capture panicked with nil recorder (typed-nil regression):\n%s", logged)
	}
}

// lockedWriter serializes slog writes into the shared buffer.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
