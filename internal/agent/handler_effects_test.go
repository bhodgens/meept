package agent

import (
	"context"
	"sync"
	"testing"
	"time"
)

// countingResumeHook records invocations of the resume reconcile hook.
type countingResumeHook struct {
	mu     sync.Mutex
	calls  int
	ctxs   []context.Context
	panic_ bool // panic on invocation
}

func (c *countingResumeHook) hook(ctx context.Context) {
	c.mu.Lock()
	c.calls++
	c.ctxs = append(c.ctxs, ctx)
	panics := c.panic_
	c.mu.Unlock()
	if panics {
		panic("hook explosion")
	}
}

func (c *countingResumeHook) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// nilLoopHandler builds a ChatHandler whose loop is nil — the hook must run
// BEFORE any loop dereference, so resume with a nil-loop handler proves the
// hook fires at the top of the method. (The post-hook path would nil-panic
// on sessionLoop; we only assert the hook side-effect, and the nil-loop
// panic is out of scope for these tests.)
func newHookTestHandler() *ChatHandler {
	h := &ChatHandler{logger: testLogger()}
	return h
}

func TestChatHandler_ResumeEffectsHook(t *testing.T) {
	t.Run("both resume methods invoke the hook once", func(t *testing.T) {
		h := newHookTestHandler()
		hook := &countingResumeHook{}
		h.SetEffectsResumeHook(hook.hook)

		// Resume proceeds past the hook into the turn body, which fails
		// naturally (nil loop) after sendResponse — irrelevant to the hook
		// assertion. Recover the expected nil-loop panic.
		defer func() {
			if rec := recover(); rec != nil {
				t.Log("expected post-hook panic from nil loop:", rec)
			}
		}()
		ctx := context.Background()
		h.resumeQuotaParkedTurn(ctx, QuotaParkedTurn{SessionID: "s1", ConversationID: "c1"})
		if got := hook.count(); got != 1 {
			t.Fatalf("quota resume hook calls = %d, want 1", got)
		}

		h.resumeParkedTurn(ctx, ParkedTurn{SessionID: "s2", ConversationID: "c2"})
		if got := hook.count(); got != 2 {
			t.Fatalf("budget resume hook calls = %d, want 2 total", got)
		}
	})

	t.Run("hook context carries a 15s deadline", func(t *testing.T) {
		h := newHookTestHandler()
		hook := &countingResumeHook{}
		h.SetEffectsResumeHook(hook.hook)

		defer func() {
			if rec := recover(); rec != nil {
				t.Log("expected post-hook panic from nil loop:", rec)
			}
		}()
		ctx := context.Background()
		h.resumeQuotaParkedTurn(ctx, QuotaParkedTurn{SessionID: "s1", ConversationID: "c1"})

		hook.mu.Lock()
		hookCtx := hook.ctxs[0]
		hook.mu.Unlock()

		deadline, ok := hookCtx.Deadline()
		if !ok {
			t.Fatal("hook context has no deadline; want the 15s reconcile bound")
		}
		if remaining := time.Until(deadline); remaining > 15*time.Second || remaining < 14*time.Second {
			t.Errorf("hook deadline remaining = %v, want ~15s", remaining)
		}
	})

	t.Run("nil hook is a no-op", func(t *testing.T) {
		h := newHookTestHandler()
		// must not panic before the (unreachable) nil-loop body
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Log("expected post-hook panic from nil loop:", rec)
				}
			}()
			h.resumeQuotaParkedTurn(context.Background(), QuotaParkedTurn{SessionID: "s1", ConversationID: "c1"})
		}()
		if h.effectsResumeHook != nil {
			t.Error("hook must remain nil after construction without a setter")
		}
	})

	t.Run("panicking hook does not break resume", func(t *testing.T) {
		h := newHookTestHandler()
		hook := &countingResumeHook{panic_: true}
		h.SetEffectsResumeHook(hook.hook)

		// The hook wrapper recovers the panic; resume proceeds past it
		// (and then fails naturally on the nil loop, which is fine — we
		// only care that the hook's recover() worked and that the
		// hook's OWN panic never leaks).
		defer func() {
			if rec := recover(); rec != nil {
				if rec == "hook explosion" {
					t.Fatal("hook panic leaked through runEffectsResumeHook")
				}
				t.Log("expected post-hook panic from nil loop:", rec)
			}
		}()
		h.resumeQuotaParkedTurn(context.Background(), QuotaParkedTurn{SessionID: "s1", ConversationID: "c1"})
		if got := hook.count(); got != 1 {
			t.Fatalf("hook calls = %d, want 1", got)
		}
	})

	t.Run("setter nil guards", func(t *testing.T) {
		var nilHandler *ChatHandler
		nilHandler.SetEffectsResumeHook(func(ctx context.Context) {}) // must not panic

		h := newHookTestHandler()
		h.SetEffectsResumeHook(nil) // nil hook ignored
		if h.effectsResumeHook != nil {
			t.Error("SetEffectsResumeHook(nil) must not wire a hook")
		}
	})
}
