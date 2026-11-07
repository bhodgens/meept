package security

// fence_stage_concurrent_test.go — RED test for the concurrent-fence pin
// (docs/plans/20261030-agent-groups/01-permissions/00T-tests.md, T3).
//
// RED reason: security.FenceStage / PermissionChecker.SetFence do not
// exist yet — the same missing-symbol compile error as fence_stage_test.go.
//
// Regression pin for the shared-FenceChecker RootPath race
// (internal/agent/handler.go previously mutated ONE shared FenceChecker's
// RootPath per session): two checkers, each with its OWN fence instance,
// run concurrent checks; every result must match its own fence. The test
// must be -race clean IN SHAPE: no shared captured variables are written
// from test goroutines — each worker collects results into a locally
// allocated slice handed back over a channel, and the fake fence's own
// call log is mutex-guarded.
//
// This test deliberately does NOT exercise SetRootPath mutation on a
// shared instance: the whole point of the port is that no shared mutable
// fence exists anymore.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fixedFence denies paths under denyPrefix and workdirs under
// denyWorkdirPrefix; unlike fakeFence it is fully deterministic and its
// call counter is mutex-guarded (safe for concurrent Check calls, as the
// FenceStage contract requires).
type fixedFence struct {
	denyPathPrefix    string
	denyWorkdirPrefix string

	mu    sync.Mutex
	calls int
}

func (f *fixedFence) CheckPath(path, op string) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.denyPathPrefix != "" && strings.HasPrefix(path, f.denyPathPrefix) {
		return fmt.Errorf("fence %p: denied %s %s", f, path, op)
	}
	return nil
}

func (f *fixedFence) CheckCommand(command, workDir string) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.denyWorkdirPrefix != "" && strings.HasPrefix(workDir, f.denyWorkdirPrefix) {
		return fmt.Errorf("fence %p: denied command in %s", f, workDir)
	}
	return nil
}

// errTextFor identifies which fence produced an error message: the fence
// pointer is embedded in the denial text, so a crossed fence is detectable.
func (f *fixedFence) ownErrorText() string {
	return fmt.Sprintf("fence %p:", f)
}

// TestConcurrentFence_Pin: two checkers with distinct fences, 100 parallel
// checks; each result must match its OWN fence. Race-clean shape.
func TestConcurrentFence_Pin(t *testing.T) {
	const totalChecks = 100

	fenceA := &fixedFence{denyPathPrefix: "/fenceA-denied"}
	fenceB := &fixedFence{denyPathPrefix: "/fenceB-denied"}

	pcA := NewPermissionChecker(Config{AllowedPaths: []string{"/"}})
	pcA.SetFence(fenceA)
	pcB := NewPermissionChecker(Config{AllowedPaths: []string{"/"}})
	pcB.SetFence(fenceB)

	// result is collected per worker into a local slice (never a shared
	// captured map/slice written from goroutines) and shipped over a
	// channel — the -race-clean shape for the pin.
	type result struct {
		checker   string // "A" | "B"
		idx       int
		allowed   bool
		reason    string
		wantDeny  bool
		denyOwner string // "A" | "B" | "" (expected deny source)
	}

	results := make(chan result, totalChecks)
	var wg sync.WaitGroup

	for i := 0; i < totalChecks; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Worker-local snapshots: everything the goroutine touches is
			// either a local copy or an effectively-immutable value.
			isA := idx%2 == 0
			checkerName := "B"
			pc := pcB
			fence := fenceB
			if isA {
				checkerName = "A"
				pc = pcA
				fence = fenceA
			}

			// Alternate between a path the checker's own fence denies and
			// one it allows, and between file and shell actions so both
			// FenceStage methods run concurrently.
			var res CheckResult
			if idx%4 < 2 {
				// file action: idx%4==0 → own-fence deny; idx%4==1 → allow
				denyOwn := idx%4 == 0
				path := "/workspace/ok.txt"
				if denyOwn {
					if isA {
						path = "/fenceA-denied/x.txt"
					} else {
						path = "/fenceB-denied/x.txt"
					}
				}
				res = pc.CheckPermissionForAgent("file_write", map[string]string{"path": path}, "")
				// denyOwner pins the expected deny SOURCE: a denial must
				// name the checker's OWN fence, never the sibling's.
				denyOwner := ""
				if denyOwn {
					if isA {
						denyOwner = "A"
					} else {
						denyOwner = "B"
					}
				}
				local := result{
					checker:   checkerName,
					idx:       idx,
					allowed:   res.Allowed,
					reason:    res.Reason,
					wantDeny:  denyOwn,
					denyOwner: denyOwner,
				}
				results <- local
				_ = fence // fence captured read-only (pointer value copied)
				return
			}

			// shell action: workdir under own-fence deny prefix on
			// idx%4==2, allowed workdir on idx%4==3.
			denyOwn := idx%4 == 2
			workdir := "/workspace"
			if denyOwn {
				if isA {
					workdir = "/fenceA-denied/repo"
				} else {
					workdir = "/fenceB-denied/repo"
				}
			}
			res = pc.CheckPermissionForAgent("shell_execute", map[string]string{
				"command": "make build",
				"workdir": workdir,
			}, "")
			// denyOwner pins the expected deny SOURCE for the assertion.
			denyOwner := ""
			if denyOwn {
				if isA {
					denyOwner = "A"
				} else {
					denyOwner = "B"
				}
			}
			results <- result{
				checker:   checkerName,
				idx:       idx,
				allowed:   res.Allowed,
				reason:    res.Reason,
				wantDeny:  denyOwn,
				denyOwner: denyOwner,
			}
		}(i)
	}

	wg.Wait()
	close(results)

	got := 0
	for r := range results {
		got++
		if r.wantDeny && r.allowed {
			t.Errorf("check %d (checker %s): expected own-fence deny, got allow (reason %q)", r.idx, r.checker, r.reason)
		}
		if !r.wantDeny && !r.allowed {
			t.Errorf("check %d (checker %s): unexpected deny: %q", r.idx, r.checker, r.reason)
		}
		// Cross-fence detection: a denial must name the checker's OWN fence
		// (pointer embedded in the fake's error text), never the other one.
		// denyOwner is the worker-side expectation of that source; a
		// deny-owned check whose reason names the OTHER fence fails here.
		if r.wantDeny {
			own := fenceB
			if r.checker == "A" {
				own = fenceA
			}
			if !strings.HasPrefix(r.reason, own.ownErrorText()) {
				t.Errorf("check %d (checker %s): deny reason %q does not come from its own fence", r.idx, r.checker, r.reason)
			}
			wantOwner := "B"
			if r.checker == "A" {
				wantOwner = "A"
			}
			if r.denyOwner != wantOwner {
				t.Errorf("check %d (checker %s): expected deny source %s, recorded %q", r.idx, r.checker, wantOwner, r.denyOwner)
			}
		}
	}
	if got != totalChecks {
		t.Fatalf("collected %d results, want %d", got, totalChecks)
	}

	// Each fence must have been consulted (no silent fence skip under
	// concurrency); the counters are internal, the assertion only checks
	// liveness, not exact counts (ordering is nondeterministic).
	if fenceACalls := fenceCalls(fenceA); fenceACalls == 0 {
		t.Errorf("fence A never consulted")
	}
	if fenceBCalls := fenceCalls(fenceB); fenceBCalls == 0 {
		t.Errorf("fence B never consulted")
	}
}

// fenceCalls reads the guarded counter (kept out of the worker closure to
// keep the -race-clean shape obvious).
func fenceCalls(f *fixedFence) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
