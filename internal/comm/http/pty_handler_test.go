package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/pty"
	"github.com/caimlas/meept/internal/security"
)

// fakePTYSession is a minimal pty.Session used to drive streamSessionOutput
// deterministically in regression tests.
type fakePTYSession struct {
	out chan []byte
}

func (f *fakePTYSession) Write([]byte) (int, error) { return 0, nil }
func (f *fakePTYSession) Read(context.Context, []byte) (int, error) {
	return 0, nil
}
func (f *fakePTYSession) Output() <-chan []byte { return f.out }
func (f *fakePTYSession) Errors() <-chan error  { return nil }
func (f *fakePTYSession) Size() (int, int)      { return 0, 0 }
func (f *fakePTYSession) Resize(int, int) error { return nil }
func (f *fakePTYSession) Close() error          { return nil }
func (f *fakePTYSession) IsRunning() bool       { return false }
func (f *fakePTYSession) ExitCode() int         { return 0 }

var _ pty.Session = (*fakePTYSession)(nil)

// TestPTYHandler_StreamSessionOutput_NoSendOnClosedChannel is a regression
// test for a panic where streamSessionOutput snapshotted subscriber channels
// under RLock, released the lock, then sent to each channel. The WebSocket
// handler's defer removed its channel from the slice and closed it AFTER
// releasing its own Lock — racing with the snapshot-based send and causing
// "send on closed channel" panics.
//
// We simulate the race: a goroutine continuously enqueues subscriber channels
// that immediately self-remove+close (mimicking rapid client disconnects),
// while streamSessionOutput pumps output. Under the previous bug, this would
// panic within a few iterations under -race; the fix holds RLock across the
// send loop so the close path cannot progress mid-send.
func TestPTYHandler_StreamSessionOutput_NoSendOnClosedChannel(t *testing.T) {
	h := &PTYHandler{
		logger: slog.Default(),
		subs:   make(map[string][]chan []byte),
	}

	const sessionID = "race-session"

	out := make(chan []byte, 50)
	sess := &fakePTYSession{out: out}

	// Producer: feed output.
	go func() {
		for range 200 {
			out <- []byte("x")
		}
		close(out)
	}()

	var wg sync.WaitGroup
	const subscribers = 8
	for range subscribers {
		wg.Go(func() {
			ch := make(chan []byte, 1)
			// Register subscriber.
			h.mu.Lock()
			h.subs[sessionID] = append(h.subs[sessionID], ch)
			h.mu.Unlock()

			// Immediately remove and close, simulating a fast client
			// disconnect that races with streamSessionOutput's send loop.
			h.mu.Lock()
			subs := h.subs[sessionID]
			for idx, c := range subs {
				if c == ch {
					h.subs[sessionID] = append(subs[:idx], subs[idx+1:]...)
					break
				}
			}
			if len(h.subs[sessionID]) == 0 {
				delete(h.subs, sessionID)
			}
			h.mu.Unlock()
			close(ch)
		})
	}

	// streamSessionOutput drains sess.Output() until it is closed.
	// Must not panic.
	h.streamSessionOutput(sessionID, sess)
	wg.Wait()
}

// TestGenerateSessionID_Unpredictable checks that 1000 consecutive session
// IDs are unique and properly prefixed.
func TestGenerateSessionID_Unpredictable(t *testing.T) {
	ids := make(map[string]struct{}, 1000)
	for range 1000 {
		id := generateSessionID()
		if !strings.HasPrefix(id, "pty-") {
			t.Fatalf("session id %q missing pty- prefix", id)
		}
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate session id generated: %s", id)
		}
		ids[id] = struct{}{}
	}
}

// TestGenerateSessionID_Length verifies the ID has the expected length:
// "pty-" (4) + 16 hex chars from 8 bytes (pkg/id.Generate format).
func TestGenerateSessionID_Length(t *testing.T) {
	id := generateSessionID()
	if len(id) != 4+16 {
		t.Errorf("session id length = %d, want %d", len(id), 4+16)
	}
}

// --- security-audit 2026-10-20 CRIT regression tests ---

// TestFenceCommandString pins that argv is folded into the fenced command
// string, so a path-like token in req.Args is surfaced to the fence (the old
// code checked req.Cmd alone and let args bypass the fence).
func TestFenceCommandString(t *testing.T) {
	if got := fenceCommandString("cat", nil); got != "cat" {
		t.Fatalf("no args: got %q want \"cat\"", got)
	}
	if got := fenceCommandString("grep", []string{"-n", "foo", "/etc/passwd"}); got != "grep -n foo /etc/passwd" {
		t.Fatalf("args fold: got %q", got)
	}
}

// TestFenceCommandString_FenceSeesArgs proves the fold actually changes the
// verdict: the bare command is inside the fence, but once the argv (an
// out-of-root path) is folded in the fence rejects it.
func TestFenceCommandString_FenceSeesArgs(t *testing.T) {
	root := t.TempDir()
	fc := security.NewFenceChecker(security.FenceConfig{Enabled: true, RootPath: root}, nil)

	outside := "/etc/hosts" // path-like token living outside the root
	// cmd alone ("cat") carries no path-like token -> fence passes.
	if err := fc.CheckCommand("cat", root); err != nil {
		t.Fatalf("bare cmd should pass the fence, got %v", err)
	}
	// Folded in the out-of-root argv -> fence must reject.
	if err := fc.CheckCommand(fenceCommandString("cat", []string{outside}), root); err == nil {
		t.Fatal("fence should reject a cat of an out-of-root path in args")
	}
}

// TestPTYHandler_FenceBlocksArgsOutsideRoot drives the HTTP handler end to
// end: a fence-bound PTY create whose argv points outside the root must be
// refused with 403, not spawned.
func TestPTYHandler_FenceBlocksArgsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	fc := security.NewFenceChecker(security.FenceConfig{Enabled: true, RootPath: root}, nil)
	h := NewPTYHandler(pty.NewManager(), slog.Default(), WithFenceChecker(fc))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := fmt.Sprintf(`{"cmd":"cat","args":["/etc/hosts"],"dir":%q}`, root)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pty/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("fence should block out-of-root argv, got status %d body %q", rec.Code, rec.Body.String())
	}
}

// TestPTYHandler_RejectDangerousEnvKey drives the handler to confirm a
// loader-interposition env key is rejected with 400 before any spawn.
func TestPTYHandler_RejectDangerousEnvKey(t *testing.T) {
	h := NewPTYHandler(pty.NewManager(), slog.Default()) // no fence: gate skipped, env check still runs
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"cmd":"bash","env":{"LD_PRELOAD":"/tmp/evil.so"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pty/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("LD_PRELOAD env should be rejected 400, got status %d body %q", rec.Code, rec.Body.String())
	}
}

// TestRejectDangerousEnvKey pins the key-denylist + empty-key rule directly.
func TestRejectDangerousEnvKey(t *testing.T) {
	for _, k := range []string{"LD_PRELOAD", "ld_preload", "DYLD_INSERT_LIBRARIES", "NODE_OPTIONS", ""} {
		if err := rejectDangerousEnvKey(k); err == nil {
			t.Errorf("rejectDangerousEnvKey(%q) = nil, want error", k)
		}
	}
	for _, k := range []string{"PATH", "HOME", "TERM"} {
		if err := rejectDangerousEnvKey(k); err != nil {
			t.Errorf("rejectDangerousEnvKey(%q) = %v, want nil", k, err)
		}
	}
}
