package slack

import (
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/acp"
)

// TestSlackChannelWaitDrainsInFlight pins the Shutdown/teardown seam (bughunt
// A1-14): after firing an in-flight permission-card post, Wait() must return
// once the goroutine finishes. The daemon's Components.Stop now calls
// SlackChannel.Wait() to join in-flight posts before tearing down; this
// channel-level test verifies the join actually completes (and doesn't hang)
// against a real (test) HTTP server.
func TestSlackChannelWaitDrainsInFlight(t *testing.T) {
	cap := newTestAPIServer(t, http.StatusOK, map[string]any{"ok": true, "ts": "1700000000.000100"})
	ch := NewSlackChannel(testSlackCfg(cap.srv.URL), slog.Default())

	if !ch.Notifier().NotifyPermissionRequested(acp.PendingPermission{ID: "perm-wait", Summary: "run rm -rf"}) {
		t.Fatal("NotifyPermissionRequested = false, want true")
	}

	done := make(chan struct{})
	go func() {
		ch.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Joined cleanly: the in-flight post completed.
	case <-time.After(5 * time.Second):
		t.Fatal("Wait() did not return within 5s (in-flight post not joined)")
	}
}
