package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// Pins for bughunt A2-2 (Executor.security data race) and A2-5
// (result-cache read before the permission gates).
//
// One-line failing reverts:
//   - TestExecutorSecuritySnapshot_Race fails under -race: restore the
//     bare `if e.security == nil` / `e.security.CheckPermission` reads in
//     Execute/checkPermission — the writer goroutine's SetSecurityChecker
//     (e.mu.Lock) races the unsynchronized reads.
//   - TestCacheHitRespectsPermissionGate fails: move the cache block back
//     to the top of Execute — the poisoned entry answers success before
//     any gate runs.
//   - Existing cache pins (TestCacheHitProgressEvent) stay green under
//     either revert posture change only via the gate-ordered read.

// TestExecutorSecuritySnapshot_Race exercises concurrent SetSecurityChecker
// against Execute. Run with -race (the repo's race gate): the pre-fix code
// read e.security bare at the fail-closed nil-check and inside
// checkPermission, so the swap raced every in-flight call.
func TestExecutorSecuritySnapshot_Race(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "read", func(ctx context.Context, args map[string]any) (any, error) {
		return "ok", nil
	}))

	executor := NewExecutor(registry, security.NewPermissionChecker(security.Config{}))

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: swap the checker continuously (per-session fence-clone swap).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			executor.SetSecurityChecker(security.NewPermissionChecker(security.Config{}))
		}
	}()

	// Readers: many concurrent Execute calls through the security lane.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				res := executor.Execute(context.Background(), llm.ToolCall{
					ID:       "tc",
					Type:     "function",
					Function: llm.ToolCallFunction{Name: "file_read", Arguments: `{"path":"/tmp/x"}`},
				})
				if res == nil {
					t.Error("nil result")
					return
				}
			}
		}()
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(stop)
	}()
	wg.Wait()
}

// TestCacheHitRespectsPermissionGate (A2-5): a pre-populated cache entry
// for a tool the agent LACKS permission for must not short-circuit the
// permission check. The cache read moved after the fail-closed security
// check, the exposure gate, and checkPermission — so the call denies and
// the cache stays untouched.
func TestCacheHitRespectsPermissionGate(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	executed := false
	registry.Register(NewMockTool("file_read", "read", func(ctx context.Context, args map[string]any) (any, error) {
		executed = true
		return "fresh", nil
	}))

	cache := NewResultCache(CacheConfig{
		MaxEntries:   100,
		DefaultTTL:   5 * time.Minute,
		EnabledTools: []string{"file_read"},
	}, nil)

	// Blocked-path checker: /etc/** is denied for file_read.
	checker := security.NewPermissionChecker(security.Config{
		BlockedPaths: []string{"/etc/**"},
	})
	executor := NewExecutor(registry, checker, WithExecutorCache(cache))

	// Poison the cache the way the pre-fix code allowed a hit to serve:
	// an entry keyed by the exact args of a now-blocked call.
	args := map[string]any{"path": "/etc/shadow"}
	cache.Put("file_read", args, "POISONED")

	res := executor.Execute(context.Background(), llm.ToolCall{
		ID:       "tc_cache_denied",
		Type:     "function",
		Function: llm.ToolCallFunction{Name: "file_read", Arguments: `{"path":"/etc/shadow"}`},
	})
	if res.Success {
		t.Fatalf("cache hit served a permission-denied tool call (Cached=%v Result=%v)", res.Cached, res.Result)
	}
	if !strings.Contains(res.Error, "permission denied") {
		t.Fatalf("Error = %q, want permission denied", res.Error)
	}
	if res.Cached {
		t.Fatal("denied call must never report a cache hit")
	}
	if executed {
		t.Fatal("denied call must never execute the tool")
	}

	// Cache untouched: the entry is still there (no eviction-on-deny, no
	// overwrite) — Get from a permission-clean path still finds it.
	if _, hit := cache.Get("file_read", args); !hit {
		t.Fatal("cache entry was mutated by the denied call")
	}
}

// TestCacheHitStillWorksAfterGateMove guards the happy path of the
// reorder: an allowed call with a warm cache answers Cached=true without
// re-executing the tool.
func TestCacheHitStillWorksAfterGateMove(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	calls := 0
	registry.Register(NewMockTool("file_read", "read", func(ctx context.Context, args map[string]any) (any, error) {
		calls++
		return "fresh", nil
	}))
	cache := NewResultCache(CacheConfig{
		MaxEntries:   100,
		DefaultTTL:   5 * time.Minute,
		EnabledTools: []string{"file_read"},
	}, nil)
	checker := security.NewPermissionChecker(security.Config{})
	executor := NewExecutor(registry, checker, WithExecutorCache(cache))

	tc := llm.ToolCall{
		ID:       "tc1",
		Type:     "function",
		Function: llm.ToolCallFunction{Name: "file_read", Arguments: `{"path":"/tmp/inside"}`},
	}
	r1 := executor.Execute(context.Background(), tc)
	if !r1.Success || r1.Cached {
		t.Fatalf("first call: Success=%v Cached=%v", r1.Success, r1.Cached)
	}
	r2 := executor.Execute(context.Background(), tc)
	if !r2.Success || !r2.Cached {
		t.Fatalf("second call should be a cache hit: Success=%v Cached=%v Err=%s", r2.Success, r2.Cached, r2.Error)
	}
	if calls != 1 {
		t.Fatalf("tool executed %d times, want 1", calls)
	}
}
