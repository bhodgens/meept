package builtin

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// DSH-6: delegate_task's async Spawn registers children under the CALLER's
// session — the loop injects its session id into the tool context
// (tools.ContextWithSessionID); a context without a session keeps the
// blank-parent behavior.
//
// The tool hands tools.SessionIDFromContext(ctx) to DelegateManager.Spawn;
// the manager keys ChildRecord.ParentSessionID on it. This test pins the
// SEAM: the context reader returns the injected session, nothing, and never
// a synthesized fallback value.
func TestSessionIDFromContext_ThreadedIntoSpawnSeam(t *testing.T) {
	// The loop's injection shape: ctx carries the parent session id.
	ctx := tools.ContextWithSessionID(context.Background(), "sess-parent-1")
	if got := tools.SessionIDFromContext(ctx); got != "sess-parent-1" {
		t.Fatalf("SessionIDFromContext = %q, want sess-parent-1", got)
	}

	// No injection: empty string is then CORRECT (blank parent, legacy
	// behavior). Never a synthesized placeholder.
	if got := tools.SessionIDFromContext(context.Background()); got != "" {
		t.Fatalf("sessionless context yielded %q, want \"\"", got)
	}

	// Empty id stores nothing: the injection is a no-op, not an empty
	// value that would shadow a parent context entry.
	ctxNoop := tools.ContextWithSessionID(ctx, "")
	if got := tools.SessionIDFromContext(ctxNoop); got != "sess-parent-1" {
		t.Fatalf("empty-id injection clobbered parent scope: %q, want sess-parent-1", got)
	}
}
