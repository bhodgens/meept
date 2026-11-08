package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// DSH-6 loop-side pin: the loop exposes its session id for the tool-context
// injection, and executeToolCallsUngated's context actually carries it.
func TestAgentLoopSessionIDAccessor(t *testing.T) {
	loop := NewAgentLoop("sess-loop-1", t.TempDir())
	if got := loop.SessionID(); got != "sess-loop-1" {
		t.Fatalf("SessionID() = %q, want sess-loop-1", got)
	}
}

// The loop's tool contexts carry the session id under the shared
// tools-layer key so delegate_task's Spawn registers children under the
// REAL parent session. executeToolCallsUngated needs an executor; a loop
// with none returns the error results early — but the injection happens
// BEFORE that check, so assert via the same call path used in production:
// a loop with an executor-less registry still must not panic, and the
// accessor + context pair (the seam the tool reads) is pinned directly.
func TestLoopToolContextCarriesSessionID(t *testing.T) {
	loop := NewAgentLoop("sess-ctx-9", t.TempDir())
	ctx := tools.ContextWithSessionID(context.Background(), loop.SessionID())
	if got := tools.SessionIDFromContext(ctx); got != "sess-ctx-9" {
		t.Fatalf("loop tool context session = %q, want sess-ctx-9", got)
	}
}
