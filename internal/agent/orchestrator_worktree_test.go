package agent

// Tests for phase-frontier-parallel leaf 03 (Contracts D + E).
//
// TestPhaseWorktreeProvisioner covers Contract D: the per-phase worktree
// provisioning hook fires inside startPhase only when parallelPhases is
// enabled, is panic-safe, degrades on error (Warn + continue without
// isolation), skips caching for ("", nil), and is invoked exactly once per
// phase start thanks to the leaf-02 re-entrancy guard.
//
// TestAdvancePhases_HookPerPhaseStart covers Contract E verification: a
// 3-phase frontier activation (A→B, A→C in one advance; D later) produces
// exactly 3 hook calls total, one per phase start, frontier starts carrying
// fromPhase == "" — and the two phases activated in the same advance get
// disjoint conversationID sets.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// worktreeProbe records provisioner invocations for Contract D assertions.
type worktreeProbe struct {
	mu    sync.Mutex
	calls []string // "taskID|phaseID|phaseName" per invocation
}

func (p *worktreeProbe) record(taskID, phaseID, phaseName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, taskID+"|"+phaseID+"|"+phaseName)
}

func (p *worktreeProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

// countPhase counts invocations for one phase name — the exact-once-per-
// phase-start assertion (a frontier advance legitimately starts several
// phases, each provisioned once).
func (p *worktreeProbe) countPhase(phaseName string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.calls {
		if parts := strings.Split(c, "|"); len(parts) == 3 && parts[2] == phaseName {
			n++
		}
	}
	return n
}

// TestPhaseWorktreeProvisioner is the leaf-03 Task 1 table: each case pins
// one skip/cache/panic contract from Contract D.
func TestPhaseWorktreeProvisioner(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		taskID     string
		parallel   bool
		wire       bool
		provision  func(ctx context.Context, taskID, phaseID, phaseName string) (string, error)
		wantCached string // PhaseWorktree(taskID, "B") after the trigger
		wantCalls  int
		check      func(t *testing.T, f *frontierFixtures, probe *worktreeProbe, logBuf *bytes.Buffer)
	}{
		{
			name:     "serial mode skips provisioner",
			taskID:   "task-wt-serial",
			parallel: false,
			wire:     true,
			provision: func(ctx context.Context, taskID, phaseID, phaseName string) (string, error) {
				return "/tmp/wt-" + phaseName, nil
			},
			wantCached: "",
			wantCalls:  0,
			check: func(t *testing.T, f *frontierFixtures, probe *worktreeProbe, logBuf *bytes.Buffer) {
				// Flag off must remain byte-identical legacy behavior:
				// the phase starts, but the provisioner never fired.
				if got := stepOf(t, f.store, "fsB1").ConversationID; got != "phase-pB-fsB1" {
					t.Errorf("fsB1 ConversationID = %q; want phase-pB-fsB1 (serial start must still work)", got)
				}
			},
		},
		{
			name:     "flag on plus provisioner wired caches worktree",
			taskID:   "task-wt-wired",
			parallel: true,
			wire:     true,
			provision: func(ctx context.Context, taskID, phaseID, phaseName string) (string, error) {
				return "/tmp/wt-" + phaseName, nil
			},
			wantCached: "/tmp/wt-B",
			wantCalls:  2, // one per started phase: B and C in this advance
			check: func(t *testing.T, f *frontierFixtures, probe *worktreeProbe, logBuf *bytes.Buffer) {
				if got := stepOf(t, f.store, "fsB1").ConversationID; got != "phase-pB-fsB1" {
					t.Errorf("fsB1 ConversationID = %q; want stamped normally", got)
				}
				if got := stepOf(t, f.store, "fsB1").AccumulatedContext; got == "" {
					t.Error("fsB1 AccumulatedContext empty; want startup context stamped")
				}
				// C got its own worktree entry too.
				if got := f.o.PhaseWorktree("task-wt-wired", "C"); got != "/tmp/wt-C" {
					t.Errorf("PhaseWorktree(C) = %q; want /tmp/wt-C", got)
				}
			},
		},
		{
			name:       "flag on provisioner nil starts phase without panic",
			taskID:     "task-wt-nil",
			parallel:   true,
			wire:       false,
			provision:  nil,
			wantCached: "",
			wantCalls:  0,
			check: func(t *testing.T, f *frontierFixtures, probe *worktreeProbe, logBuf *bytes.Buffer) {
				if got := stepOf(t, f.store, "fsB1").ConversationID; got != "phase-pB-fsB1" {
					t.Errorf("fsB1 ConversationID = %q; want phase started", got)
				}
			},
		},
		{
			name:     "provisioner error warns and phase continues without isolation",
			taskID:   "task-wt-err",
			parallel: true,
			wire:     true,
			provision: func(ctx context.Context, taskID, phaseID, phaseName string) (string, error) {
				return "", errors.New("worktree clone failed")
			},
			wantCached: "",
			wantCalls:  2, // B and C both attempted; both errored
			check: func(t *testing.T, f *frontierFixtures, probe *worktreeProbe, logBuf *bytes.Buffer) {
				if out := logBuf.String(); !bytes.Contains(logBuf.Bytes(), []byte("phase worktree provisioning failed")) {
					t.Errorf("expected provisioning-failure Warn in log; got: %s", out)
				}
				if got := stepOf(t, f.store, "fsB1").ConversationID; got != "phase-pB-fsB1" {
					t.Errorf("fsB1 ConversationID = %q; want phase STILL started after error", got)
				}
			},
		},
		{
			name:     "provisioner returns empty means no worktree needed",
			taskID:   "task-wt-empty",
			parallel: true,
			wire:     true,
			provision: func(ctx context.Context, taskID, phaseID, phaseName string) (string, error) {
				return "", nil // no-file-write plan: provisioner's decision
			},
			wantCached: "",
			wantCalls:  2, // invoked for every starting phase, cached for none
			check: func(t *testing.T, f *frontierFixtures, probe *worktreeProbe, logBuf *bytes.Buffer) {
				if got := stepOf(t, f.store, "fsB1").ConversationID; got != "phase-pB-fsB1" {
					t.Errorf("fsB1 ConversationID = %q; want phase started", got)
				}
			},
		},
		{
			name:     "provisioner panic is contained and phase still starts",
			taskID:   "task-wt-panic",
			parallel: true,
			wire:     true,
			provision: func(ctx context.Context, taskID, phaseID, phaseName string) (string, error) {
				panic("provisioner exploded")
			},
			wantCached: "",
			wantCalls:  2, // both phases attempted; both panics contained
			check: func(t *testing.T, f *frontierFixtures, probe *worktreeProbe, logBuf *bytes.Buffer) {
				if out := logBuf.String(); !bytes.Contains(logBuf.Bytes(), []byte("phase worktree provisioning failed")) {
					t.Errorf("expected panic-degrade Warn in log; got: %s", out)
				}
				if got := stepOf(t, f.store, "fsB1").ConversationID; got != "phase-pB-fsB1" {
					t.Errorf("fsB1 ConversationID = %q; want phase STILL started after panic", got)
				}
			},
		},
		{
			name:     "idempotent re-invocation provisions exactly once per phase start",
			taskID:   "task-wt-idem",
			parallel: true,
			wire:     true,
			provision: func(ctx context.Context, taskID, phaseID, phaseName string) (string, error) {
				return "/tmp/wt-" + phaseName, nil
			},
			wantCached: "/tmp/wt-B",
			wantCalls:  2, // advance 1 starts B+C (asserted here); advance 2's D is asserted per-phase in check
			check: func(t *testing.T, f *frontierFixtures, probe *worktreeProbe, logBuf *bytes.Buffer) {
				// B finished: D (dependent on B) starts via a second
				// advance. Each phase must have been provisioned exactly
				// once — the re-entrancy guard prevents the double terminal
				// event from restarting B or C.
				completeEquivalenceStep(t, f.store, "fsB1")
				f.o.maybeTransitionPhase(ctx, "fsB1", "task-wt-idem")
				for _, phase := range []string{"B", "C", "D"} {
					if got := probe.countPhase(phase); got != 1 {
						t.Errorf("provisioner calls for phase %s = %d; want exactly 1", phase, got)
					}
				}
				// And B's cached worktree survives the later advance.
				if got := f.o.PhaseWorktree("task-wt-idem", "B"); got != "/tmp/wt-B" {
					t.Errorf("PhaseWorktree(B) = %q; want /tmp/wt-B after later advance", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFrontierFixtures(t, ctx, tt.taskID)
			f.o.SetParallelPhases(tt.parallel)
			// Capture log output so Warn paths are asserted, not assumed.
			var buf bytes.Buffer
			f.o.logger = slog.New(slog.NewTextHandler(&buf, nil))

			// Wire the provisioner through a probe wrapper so invocation
			// counting works even for the panic case (the probe records
			// BEFORE the panic fires).
			probe := &worktreeProbe{}
			inner := tt.provision
			if tt.wire {
				f.o.SetPhaseWorktreeProvisioner(func(c context.Context, taskID, phaseID, phaseName string) (string, error) {
					probe.record(taskID, phaseID, phaseName)
					if inner == nil {
						return "", nil
					}
					return inner(c, taskID, phaseID, phaseName)
				})
			}

			// Trigger: A completes -> frontier activates B and C.
			f.o.artifacts.Add(Artifact{Name: "x", Kind: "file"}, "fsA2")
			completeEquivalenceStep(t, f.store, "fsA1")
			completeEquivalenceStep(t, f.store, "fsA2")
			f.o.maybeTransitionPhase(ctx, "fsA2", tt.taskID)

			if got := probe.count(); got != tt.wantCalls {
				t.Errorf("provisioner calls = %d; want %d", got, tt.wantCalls)
			}
			if got := f.o.PhaseWorktree(tt.taskID, "B"); got != tt.wantCached {
				t.Errorf("PhaseWorktree(%q, B) = %q; want %q", tt.taskID, got, tt.wantCached)
			}
			if tt.check != nil {
				tt.check(t, f, probe, &buf)
			}
		})
	}
}

// TestAdvancePhases_HookPerPhaseStart verifies Contract E under frontier
// dispatch: a 3-phase activation (A→B, A→C in one advance, D later) fires
// the phase-transition hook exactly once per phase start — 3 calls total,
// frontier starts carrying fromPhase == "" — and the two phases activated
// in the same advance produce disjoint conversationID sets.
func TestAdvancePhases_HookPerPhaseStart(t *testing.T) {
	ctx := context.Background()

	t.Run("three phase starts fire three hook calls", func(t *testing.T) {
		const taskID = "task-hooks3"
		f := newFrontierFixtures(t, ctx, taskID)
		f.o.SetParallelPhases(true)

		var mu sync.Mutex
		var calls []string // "fromPhase->toPhase"
		f.o.SetPhaseTransitionHook(func(taskID, fromPhase, toPhase string) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, fromPhase+"->"+toPhase)
		})

		// Advance 1: A completes -> B and C start together.
		f.o.artifacts.Add(Artifact{Name: "x", Kind: "file"}, "fsA2")
		completeEquivalenceStep(t, f.store, "fsA1")
		completeEquivalenceStep(t, f.store, "fsA2")
		f.o.maybeTransitionPhase(ctx, "fsA2", taskID)

		mu.Lock()
		if len(calls) != 2 {
			t.Fatalf("after advance 1: hook calls = %v; want exactly 2 (B, C)", calls)
		}
		for _, c := range calls {
			from, to, ok := planSplitArrow(c)
			if !ok {
				t.Fatalf("malformed hook record %q", c)
			}
			if from != "" {
				t.Errorf("hook fromPhase = %q; want \"\" on frontier activation of %s", from, to)
			}
			if to != "B" && to != "C" {
				t.Errorf("unexpected hook target %q; want B or C", to)
			}
		}
		mu.Unlock()

		// Advance 2: B completes -> D starts. Third and final call.
		completeEquivalenceStep(t, f.store, "fsB1")
		f.o.maybeTransitionPhase(ctx, "fsB1", taskID)

		mu.Lock()
		defer mu.Unlock()
		if len(calls) != 3 {
			t.Fatalf("after both advances: hook calls = %v; want exactly 3 (one per phase start)", calls)
		}
		last := calls[2]
		from, to, ok := planSplitArrow(last)
		if !ok || from != "" || to != "D" {
			t.Errorf("third hook call = %q; want \"->D\" frontier activation", last)
		}
	})

	t.Run("parallel starts produce disjoint conversationIDs", func(t *testing.T) {
		const taskID = "task-hooks-disjoint"
		f := newFrontierFixtures(t, ctx, taskID)
		f.o.SetParallelPhases(true)

		f.o.artifacts.Add(Artifact{Name: "x", Kind: "file"}, "fsA2")
		completeEquivalenceStep(t, f.store, "fsA1")
		completeEquivalenceStep(t, f.store, "fsA2")
		f.o.maybeTransitionPhase(ctx, "fsA2", taskID)

		// The two phases activated in the SAME advance must own disjoint
		// conversationID sets (phase-<phaseID>-<stepID> is phase-scoped
		// by construction — this pins it).
		seen := map[string]string{} // conversationID -> owning phase step
		for _, id := range []string{"fsB1", "fsC1"} {
			cid := stepOf(t, f.store, id).ConversationID
			if cid == "" {
				t.Fatalf("%s has no conversationID; phase did not start", id)
			}
			if owner, dup := seen[cid]; dup {
				t.Errorf("conversationID collision: %q owned by both %s and %s", cid, owner, id)
			}
			seen[cid] = id
		}
		if got := stepOf(t, f.store, "fsB1").ConversationID; got != "phase-pB-fsB1" {
			t.Errorf("fsB1 conversationID = %q; want phase-pB-fsB1", got)
		}
		if got := stepOf(t, f.store, "fsC1").ConversationID; got != "phase-pC-fsC1" {
			t.Errorf("fsC1 conversationID = %q; want phase-pC-fsC1", got)
		}
	})
}

// planSplitArrow splits the test hook-record format "from->to".
func planSplitArrow(s string) (from, to string, ok bool) {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '-' && s[i+1] == '>' {
			return s[:i], s[i+2:], true
		}
	}
	return "", "", false
}
