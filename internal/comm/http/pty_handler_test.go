package http

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/pty"
)

// fakePTYSession is a minimal pty.Session used to drive streamSessionOutput
// deterministically in regression tests.
type fakePTYSession struct {
	out chan []byte
}

func (f *fakePTYSession) Write([]byte) (int, error)         { return 0, nil }
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
		for i := 0; i < 200; i++ {
			out <- []byte("x")
		}
		close(out)
	}()

	var wg sync.WaitGroup
	const subscribers = 8
	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		}()
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
	for i := 0; i < 1000; i++ {
		id := generateSessionID()
		if !strings.HasPrefix(id, "pty-") {
			t.Fatalf("session id %q missing pty- prefix", id)
		}
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate session id after %d generations: %s", i, id)
		}
		ids[id] = struct{}{}
	}
}

// TestGenerateSessionID_Length verifies the ID has the expected length:
// "pty-" (4) + 32 hex chars from 16 bytes.
func TestGenerateSessionID_Length(t *testing.T) {
	id := generateSessionID()
	if len(id) != 4+32 {
		t.Errorf("session id length = %d, want %d", len(id), 4+32)
	}
}
