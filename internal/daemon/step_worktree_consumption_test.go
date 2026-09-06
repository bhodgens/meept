package daemon

// Worktree-consumption tests for phase-frontier-parallel leaf 04 (Contract
// C): when the orchestrator holds a provisioned per-phase worktree for a
// step's phase, that path WINS in step working-dir resolution; every
// no-worktree case — serial mode (provisioner never ran), no provisioner
// wired, orchestrator not wired at all, step without a phase — must leave
// the existing session precedence (WorktreePath > ProjectPath > CWD > "")
// untouched.
//
// The "worktree wins" case cannot be synthesized from outside the agent
// package (phaseWorktrees is populated only by the real startPhase), so it
// is driven end-to-end through the wiring fixture: flag on + provisioner →
// a real phase start provisions and caches → the processor consumes it.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// TestResolveStepWorkingDirFor_NoWorktree table: with NO phase worktree in
// play, resolveStepWorkingDirFor behaves exactly like the legacy
// resolveStepWorkingDir. The seeded session carries all three precedence
// levels (/wt, /proj, /cwd); /wt must win in every row.
func TestResolveStepWorkingDirFor_NoWorktree(t *testing.T) {
	tests := []struct {
		name     string
		parallel bool
		wireProv bool
		phase    string
		wireOrch bool // wire an orchestrator at all (nil-ref safety)
	}{
		{name: "no orchestrator wired at all", wireOrch: false},
		{name: "serial mode: provisioner never ran", wireOrch: true, parallel: false, wireProv: true, phase: "B"},
		{name: "flag on but no provisioner wired", wireOrch: true, parallel: true, phase: "B"},
		{name: "step without phase skips worktree lookup", wireOrch: true, parallel: true, wireProv: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ss, ts := newTestAgentJobProcessor(t)
			if tt.wireOrch {
				orch := agent.NewOrchestrator(agent.OrchestratorDeps{})
				orch.SetParallelPhases(tt.parallel)
				if tt.wireProv {
					orch.SetPhaseWorktreeProvisioner(func(context.Context, string, string, string) (string, error) {
						return "/phase-wt/never-invoked", nil
					})
				}
				p.WithOrchestrator(orch)
			}

			seedTestSession(t, ss, func(s *session.Session) {
				s.ConversationID = "conv-wt-none"
				s.WorktreePath = "/wt"
				s.ProjectPath = "/proj"
				s.DetectionContext = &session.DetectionContext{CWD: "/cwd"}
			})
			seedTestTask(t, ts, "task-wt-none", []string{"conv-wt-none"})

			store := ts.StepStore()
			step := task.NewTaskStep("task-wt-none", "step under test", 1)
			step.ID = "wsx-none-1"
			step.Phase = tt.phase
			if err := store.Create(step); err != nil {
				t.Fatalf("create step: %v", err)
			}

			job := &queue.Job{TaskID: "task-wt-none"}
			// Legacy behavior intact, and the new wrapper matches it.
			if got := p.resolveStepWorkingDir(job); got != "/wt" {
				t.Errorf("resolveStepWorkingDir = %q, want /wt", got)
			}
			if got := p.resolveStepWorkingDirFor(job, step.ID); got != "/wt" {
				t.Errorf("resolveStepWorkingDirFor = %q, want /wt (precedence unchanged)", got)
			}
		})
	}
}

// TestDaemonWiring_PhaseWorktreeWinsInDispatch drives provisioning through
// the REAL startPhase path (bus event → frontier/serial dispatch →
// startPhase → provisioner → phaseWorktrees) and then proves the daemon's
// step-dispatch consumption: the per-phase worktree wins for steps of that
// phase, while the legacy resolution stays on the session worktree.
func TestDaemonWiring_PhaseWorktreeWinsInDispatch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name     string
		parallel bool
		wantB    string // expected working dir for phase-B's step after B starts
	}{
		{
			name:     "flag on: provisioned phase worktree wins for phase-B step",
			parallel: true,
			wantB:    "/phase-wt/B",
		},
		{
			name:     "flag off: startPhase skips provisioning, session worktree still wins",
			parallel: false,
			wantB:    "/wt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newWiringFixture(t, tt.parallel, func(_ context.Context, _, _, phaseName string) (string, error) {
				return "/phase-wt/" + phaseName, nil
			})
			f.seedPlan(t)

			// A real processor over the SAME task store, consuming the SAME
			// orchestrator — the production wiring shape. A session bound
			// to /wt (worktree) / /proj (project) / /cwd gives the legacy
			// resolution chain a real winner at every level.
			ss := session.NewMemoryStore(logger)
			p := NewAgentJobProcessor(nil, logger)
			p.WithTaskStore(f.taskStore)
			p.WithSessionStore(ss)
			p.WithOrchestrator(f.orch)
			seedTestSession(t, ss, func(s *session.Session) {
				s.ConversationID = "conv-wt-e2e"
				s.WorktreePath = "/wt"
				s.ProjectPath = "/proj"
				s.DetectionContext = &session.DetectionContext{CWD: "/cwd"}
			})
			if err := f.taskStore.LinkSession(f.taskID, "conv-wt-e2e"); err != nil {
				t.Fatalf("link session: %v", err)
			}

			if err := f.store.SetState("wsA1", task.StepCompleted); err != nil {
				t.Fatalf("complete wsA1: %v", err)
			}
			if err := f.store.SetState("wsA2", task.StepCompleted); err != nil {
				t.Fatalf("complete wsA2: %v", err)
			}
			f.completeJob("job-wsA2", map[string]any{"success": true})

			// B starts (both modes) — that start is what provisions B's
			// worktree when the flag is on.
			waitUntil(t, 5*time.Second, func() bool {
				s, err := f.store.GetByID("wsB1")
				if err != nil || s == nil {
					return false
				}
				return s.ConversationID == "phase-pB-wsB1"
			}, "phase B start (wsB1 stamped)")

			job := &queue.Job{TaskID: f.taskID}
			if got := p.resolveStepWorkingDirFor(job, "wsB1"); got != tt.wantB {
				t.Errorf("resolveStepWorkingDirFor(wsB1) = %q, want %q", got, tt.wantB)
			}
			// Legacy resolution untouched: session worktree still wins there.
			if got := p.resolveStepWorkingDir(job); got != "/wt" {
				t.Errorf("resolveStepWorkingDir = %q, want /wt (legacy path unchanged)", got)
			}

			// C also starts under the frontier and gets its own worktree.
			if tt.parallel {
				waitUntil(t, 5*time.Second, func() bool {
					s, err := f.store.GetByID("wsC1")
					if err != nil || s == nil {
						return false
					}
					return s.ConversationID == "phase-pC-wsC1"
				}, "phase C start (wsC1 stamped)")
				if got := f.orch.PhaseWorktree(f.taskID, "C"); got != "/phase-wt/C" {
					t.Errorf("PhaseWorktree(task, C) = %q, want /phase-wt/C", got)
				}
			}
		})
	}
}
