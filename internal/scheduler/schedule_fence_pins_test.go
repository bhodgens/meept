package scheduler

// Execution-time session-fence pins (bughunt 2026-11-27 P1, conservative
// branch).
//
// The P1 finding: schedule_create/cron_create had NO creation-time fence and
// the scheduler's execution-time guard carried a nil fence input, so a
// persisted shell job could target paths outside the creating session's root
// and fire unjudged. The creation-time half landed in 06fb3b657
// (internal/agent + pkg/security); this file pins the EXECUTION-TIME half:
// the JobFence seam on ShellJob (internal/scheduler, this lane's owned
// files), nil = legacy byte-identical posture.
//
// The production fence mint lives in agent.NewSessionFenceStage (jail
// semantics: pathWithin on symlink-resolved forms, ErrOutsideSessionRoot
// outside the root). These pins use a test double with the same two-method
// surface — the seam is structural, so the production stage satisfies it
// unchanged.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// fencedGuardDouble is a minimal JobFence double mirroring the session
// fence's CheckCommand shape: it extracts path-like tokens the way the real
// fence does (coarse, substring-level is enough for the pins) and denies
// when a token falls outside root (ErrOutsideSessionRoot posture). Every
// call is recorded so a pin can prove the fence was consulted at all
// (non-vacuity).
type fencedGuardDouble struct {
	root string

	commands []string
	dirs     []string
}

func (f *fencedGuardDouble) CheckPath(_, _ string) error { return nil }

func (f *fencedGuardDouble) CheckCommand(command, workDir string) error {
	f.commands = append(f.commands, command)
	f.dirs = append(f.dirs, workDir)
	// Coarse posture of the real fence: any absolute path token outside the
	// root is refused. The pins' commands deliberately carry /etc/hosts.
	for _, tok := range strings.Fields(command) {
		if strings.HasPrefix(tok, "/") && !strings.HasPrefix(tok, f.root) {
			return errors.New("command path outside session root: " + tok)
		}
	}
	return nil
}

func fencePinCfg(id string) JobConfig {
	return JobConfig{
		ID: id, Name: id, Type: JobTypeShell, Schedule: "@yearly",
		ShellConfig: &ShellJobConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "cat /etc/hosts"}, // target OUTSIDE any session root
		},
	}
}

// Pin P1-1 (the finding's exact pin): a scheduled command targeting a path
// outside the session root is DENIED at fire time when the fence is armed.
// Failing revert: deleting the phase-3 block in ShellJob.Execute (or the
// jobFence threading in newShellJob) lets Execute fall through to spawn.
func TestShellJobFenceDeniesOutsideSessionRootAtFireTime(t *testing.T) {
	t.Setenv(config.EnvMeeptHome, t.TempDir())
	cfg := fencePinCfg("fence-pin-deny")

	fence := &fencedGuardDouble{root: "/tmp/session-root-pin"}
	deps := &JobDependencies{
		Fence:            fence,
		FenceSessionRoot: "/tmp/session-root-pin",
	}
	job, err := NewShellJobWithDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewShellJobWithDeps failed: %v", err)
	}

	err = job.Execute(context.Background())
	if err == nil {
		t.Fatal("out-of-root scheduled command must be DENIED at fire time; Execute returned nil")
	}
	if !strings.Contains(err.Error(), "session fence") {
		t.Errorf("error = %v; want a session-fence denial", err)
	}
	if len(fence.commands) != 1 {
		t.Fatalf("fence consulted %d times, want exactly 1 (guard source + workdir judged together)", len(fence.commands))
	}
	// The judged surface must be the REAL shell source (/bin/sh -c wrapper
	// unwrapped), matching the guard's classification source — never the
	// literal "/bin/sh".
	if fence.commands[0] != "cat /etc/hosts" {
		t.Errorf("fence judged %q; want the unwrapped shell source cat /etc/hosts", fence.commands[0])
	}
}

// Pin P1-2: the unset-work_dir job is judged against the SESSION ROOT the
// daemon armed (the fence's implicit-cwd posture), not skipped and not an
// implicit ".". Failing revert: effectiveWorkDir losing the fenceRoot
// fallback makes the judged dir empty.
func TestShellJobFenceJudgesUnsetWorkDirAgainstSessionRoot(t *testing.T) {
	t.Setenv(config.EnvMeeptHome, t.TempDir())
	cfg := fencePinCfg("fence-pin-implicit-cwd")
	cfg.ShellConfig.WorkDir = "" // the common schedule_create shape

	fence := &fencedGuardDouble{root: "/sroot"} // in-root tokens pass; /etc/hosts is denied
	deps := &JobDependencies{
		Fence:            fence,
		FenceSessionRoot: "/sroot/session-a",
	}
	job, err := NewShellJobWithDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewShellJobWithDeps failed: %v", err)
	}
	// The fence denies the out-of-root command at fire time (before spawn),
	// which is exactly the assertion below — the job must NOT reach exec.
	execErr := job.Execute(context.Background())
	if execErr == nil || !strings.Contains(execErr.Error(), "session fence") {
		t.Fatalf("unset-work_dir job with out-of-root command must still be fence-denied; got %v", execErr)
	}
	// And the judged workDir must be the SESSION ROOT (implicit-cwd posture),
	// never empty and never an unjudged daemon CWD.
	if len(fence.dirs) != 1 {
		t.Fatalf("fence consulted %d times, want 1", len(fence.dirs))
	}
	if fence.dirs[0] != "/sroot/session-a" {
		t.Errorf("fence judged workDir %q; want the armed session root", fence.dirs[0])
	}
}

// Pin P1-3: an explicit job work_dir is judged AS-IS (the fence owns the
// boundary semantics — the same value the child's cmd.Dir receives).
func TestShellJobFenceJudgesExplicitWorkDir(t *testing.T) {
	t.Setenv(config.EnvMeeptHome, t.TempDir())
	cfg := fencePinCfg("fence-pin-explicit-wd")
	cfg.ShellConfig.WorkDir = "/outside-jail/deep"

	fence := &fencedGuardDouble{root: "/tmp/session-root-pin"}
	deps := &JobDependencies{Fence: fence, FenceSessionRoot: "/tmp/session-root-pin"}
	job, err := NewShellJobWithDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewShellJobWithDeps failed: %v", err)
	}

	err = job.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session fence") {
		t.Fatalf("explicit out-of-root work_dir must be denied; got err=%v", err)
	}
	if fence.dirs[0] != "/outside-jail/deep" {
		t.Errorf("fence judged workDir %q; want the explicit job work_dir", fence.dirs[0])
	}
}

// Pin P1-4 (the nil=legacy pin): NO fence armed — a job that would be denied
// by the fence EXECUTES normally. The legacy posture is byte-identical: no
// fence input, no judgment, no error from the fence path.
// Failing revert: an unconditional fence construction (or a nil-fence
// deny-everything stub) breaks the legacy scheduler suite AND this pin.
func TestShellJobNilFenceLegacyBehaviorByteIdentical(t *testing.T) {
	t.Setenv(config.EnvMeeptHome, t.TempDir())
	tmp := t.TempDir()
	outFile := filepath.Join(tmp, "legacy-out.txt")

	cfg := JobConfig{
		ID: "fence-pin-nil-legacy", Name: "legacy", Type: JobTypeShell, Schedule: "@yearly",
		ShellConfig: &ShellJobConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "echo legacy-ran > " + outFile},
		},
	}
	// deps with Guard+Sink but NO Fence — the exact pre-fix daemon shape.
	deps := &JobDependencies{}
	job, err := NewShellJobWithDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewShellJobWithDeps failed: %v", err)
	}
	if err := job.Execute(context.Background()); err != nil {
		t.Fatalf("nil-fence legacy job must execute unchanged; Execute returned %v", err)
	}
	if _, statErr := os.Stat(outFile); statErr != nil {
		t.Fatalf("legacy output file missing after nil-fence run: %v", statErr)
	}
}

// Pin P1-5: nil JobDependencies — NewShellJobWithDeps must stay nil-safe
// (the standalone-construction legacy path) with the fence unset.
func TestShellJobNilDepsNilSafe(t *testing.T) {
	cfg := fencePinCfg("fence-pin-nil-deps")
	job, err := NewShellJobWithDeps(cfg, nil)
	if err != nil {
		t.Fatalf("NewShellJobWithDeps(nil deps) failed: %v", err)
	}
	_ = job // construction must not arm any fence; Execute exercised by P1-4's shape
}
