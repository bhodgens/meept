package agent

// fence_stage_test.go — regression pin for the per-session fence injection
// (agent-groups branch-01 leaf 01 T2/T4,
// docs/plans/20261030-agent-groups/01-permissions/01-fence-stage.md).
//
// The race this pins: ChatHandler.sessionLoop / Dispatcher.resolveAgent
// previously mutated ONE shared FenceChecker's RootPath per session, so
// under concurrent multi-session load session A's sandbox root was
// applied to session B's permission checks. The fix gives every
// session-scoped loop its OWN fence instance (via the session fence
// factory + injectSessionFence); these tests prove isolation and
// race-cleanliness of that path.

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/pkg/security"
)

// fakeSessionFence denies paths outside root and embeds its identity in
// the denial text so a crossed fence is detectable. Mutex-guarded call
// log (the FenceStage contract requires concurrency-safe Check calls).
type fakeSessionFence struct {
	id    string
	root  string
	mu    sync.Mutex
	calls int
}

func (f *fakeSessionFence) CheckPath(path, op string) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if !strings.HasPrefix(path, f.root) {
		return fmt.Errorf("fence[%s root=%s]: denied %s %s", f.id, f.root, path, op)
	}
	return nil
}

func (f *fakeSessionFence) CheckCommand(command, workDir string) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if !strings.HasPrefix(workDir, f.root) && workDir != "." {
		return fmt.Errorf("fence[%s root=%s]: denied command in %s", f.id, f.root, workDir)
	}
	return nil
}

// TestSessionFence_ConcurrentSessionsIsolated is the T2 regression pin:
// two concurrent sessions with different roots are each fenced to THEIR
// root — a denial from session A's checker names A's fence, never B's.
func TestSessionFence_ConcurrentSessionsIsolated(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()

	factory := func(rootPath string, noFence bool) security.FenceStage {
		if noFence || rootPath == "" {
			return nil
		}
		id := "A"
		root := rootA
		if rootPath == rootB {
			id = "B"
			root = rootB
		}
		return &fakeSessionFence{id: id, root: root}
	}

	base := security.NewPermissionChecker(security.Config{AllowedPaths: []string{"/"}})
	pcA := injectSessionFence(base, factory, rootA, false)
	pcB := injectSessionFence(base, factory, rootB, false)

	if pcA == base || pcB == base {
		t.Fatalf("injectSessionFence must return per-session clones, not the shared base checker")
	}
	if pcA == pcB {
		t.Fatalf("sessions A and B must not share a PermissionChecker instance")
	}

	const perSession = 50
	type verdict struct {
		session string
		idx     int
		allowed bool
		reason  string
	}
	// each session sends perSession*2 verdicts (own-root + cross-root path)
	results := make(chan verdict, perSession*4)
	var wg sync.WaitGroup

	run := func(name string, pc *security.PermissionChecker, root string) {
		defer wg.Done()
		other := rootB
		if root == rootB {
			other = rootA
		}
		for i := 0; i < perSession; i++ {
			// Inside own root: allowed. Inside the OTHER root: denied by
			// the session's OWN fence (the old shared-RootPath race would
			// let this through or deny with the wrong fence's root).
			for _, path := range []string{
				filepath.Join(root, fmt.Sprintf("own-%d.txt", i)),
				filepath.Join(other, fmt.Sprintf("cross-%d.txt", i)),
			} {
				got := pc.CheckPermissionForAgent("file_write", map[string]string{"path": path}, "")
				results <- verdict{session: name, idx: i, allowed: got.Allowed, reason: got.Reason}
			}
		}
	}
	wg.Add(2)
	go run("A", pcA, rootA)
	go run("B", pcB, rootB)
	wg.Wait()
	close(results)

	denies, allows := 0, 0
	for v := range results {
		ownFence := "A"
		if v.session == "B" {
			ownFence = "B"
		}
		if v.allowed {
			allows++
			continue
		}
		denies++
		// A cross-root deny MUST come from the session's OWN fence.
		if !strings.HasPrefix(v.reason, "fence["+ownFence+" ") {
			t.Fatalf("session %s: cross-root denial came from the WRONG fence: %q (want fence[%s ...])", v.session, v.reason, ownFence)
		}
	}
	if allows != perSession*2 {
		t.Fatalf("expected %d own-root allows (both sessions), got %d", perSession*2, allows)
	}
	if denies != perSession*2 {
		t.Fatalf("expected %d cross-root denies (both sessions), got %d", perSession*2, denies)
	}
}

// TestSessionFence_NilFactoryAndNoFencePassthrough: fencing disabled
// (nil factory) or a no-fence session must return the base checker
// unchanged — the legacy path, no fence stage.
func TestSessionFence_NilFactoryAndNoFencePassthrough(t *testing.T) {
	base := security.NewPermissionChecker(security.Config{})
	if got := injectSessionFence(base, nil, t.TempDir(), false); got != base {
		t.Fatalf("nil factory must pass the base checker through unchanged")
	}
	factory := func(string, bool) security.FenceStage { return &fakeSessionFence{id: "x", root: "/"} }
	if got := injectSessionFence(base, factory, t.TempDir(), true); got != base {
		t.Fatalf("noFence session must pass the base checker through unchanged (no fence)")
	}
	if got := injectSessionFence(base, factory, "", false); got != base {
		t.Fatalf("empty root must pass the base checker through unchanged (no fence)")
	}
	if injectSessionFence(nil, factory, t.TempDir(), false) != nil {
		t.Fatalf("nil base must yield nil (typed-nil guard)")
	}
}

// TestSessionFence_FactoryMintsDistinctInstances: two calls to the real
// factory shape (newSessionFenceFactory) must produce independent fence
// instances — the structural opposite of the retired shared checker.
func TestSessionFence_FactoryMintsDistinctInstances(t *testing.T) {
	factory := newSessionFenceFactory(testLoggerForFence())
	rootA := t.TempDir()
	rootB := t.TempDir()

	fa := factory(rootA, false)
	fb := factory(rootB, false)
	if fa == nil || fb == nil {
		t.Fatalf("factory returned nil stages for valid roots")
	}
	if fa == fb {
		t.Fatalf("factory must mint a DISTINCT fence instance per session (shared instance is the retired race)")
	}

	// Each fence enforces its own root: a path under rootA is denied by
	// fence B but allowed by fence A.
	pathA := filepath.Join(rootA, "x.txt")
	if err := fa.CheckPath(pathA, "write"); err != nil {
		t.Fatalf("fence A denied its own root: %v", err)
	}
	if err := fb.CheckPath(pathA, "write"); err == nil {
		t.Fatalf("fence B allowed a path outside its root (cross-session leak)")
	}

	// --nofence sessions get no fence at all.
	if got := factory(rootA, true); got != nil {
		t.Fatalf("noFence must yield a nil fence stage, got %T", got)
	}
	if got := factory("", false); got != nil {
		t.Fatalf("empty root must yield a nil fence stage, got %T", got)
	}
}

// testLoggerForFence returns a silent logger for factory construction
// (the fence checker logs misconfiguration warnings through it).
func testLoggerForFence() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
