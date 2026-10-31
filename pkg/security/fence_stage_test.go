package security

// fence_stage_test.go — RED tests for the fence-stage port
// (docs/plans/20261030-agent-groups/01-permissions/01-fence-stage.md).
//
// RED reason: security.FenceStage and PermissionChecker.SetFence do not
// exist yet. Every failure below must be a missing-symbol compile error
// ("undefined: FenceStage" / "pc.SetFence undefined"), never a syntax
// error. These tests are written against the C1 contract verbatim:
//
//	FenceStage interface {
//	    CheckPath(path, op string) error
//	    CheckCommand(command, workDir string) error
//	}
//
// Stage order: after path globs, before confirmation gating. Nil fence =
// legacy behavior unchanged. Deny shape: Allowed=false, Reason=err.Error().

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeFence is a test double for the (not yet existing) FenceStage
// interface. It denies file paths under denyPathPrefix and optionally
// shell commands whose workdir is under denyWorkdirPrefix, and records
// every consultation so tests can pin stage order.
type fakeFence struct {
	mu sync.Mutex

	denyPathPrefix    string
	denyWorkdirPrefix string
	errText           string

	calls []string // recorded as "path:<path>:<op>" / "cmd:<command>:<workdir>"
}

func (f *fakeFence) CheckPath(path, op string) error {
	f.mu.Lock()
	f.calls = append(f.calls, "path:"+path+":"+op)
	f.mu.Unlock()
	if f.denyPathPrefix != "" && strings.HasPrefix(path, f.denyPathPrefix) {
		return errors.New(f.errText)
	}
	return nil
}

func (f *fakeFence) CheckCommand(command, workDir string) error {
	f.mu.Lock()
	f.calls = append(f.calls, "cmd:"+command+":"+workDir)
	f.mu.Unlock()
	if f.denyWorkdirPrefix != "" && strings.HasPrefix(workDir, f.denyWorkdirPrefix) {
		return errors.New(f.errText)
	}
	return nil
}

func (f *fakeFence) recordedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// Compile-time shape pin: the fake must satisfy FenceStage exactly as C1
// declares it. RED: FenceStage undefined.
var _ FenceStage = (*fakeFence)(nil)

// TestFenceStage_NilFenceLegacyUnchanged: a checker with no fence set
// (never SetFence'd) must produce byte-identical legacy results.
func TestFenceStage_NilFenceLegacyUnchanged(t *testing.T) {
	pc := NewPermissionChecker(Config{
		AllowedPaths: []string{"/tmp"},
		BlockedPaths: []string{"/etc"},
	})

	tests := []struct {
		name        string
		action      string
		details     map[string]string
		wantAllowed bool
		wantReason  string
	}{
		{
			name:        "write inside allowed path",
			action:      "file_write",
			details:     map[string]string{"path": "/tmp/x"},
			wantAllowed: true,
			wantReason:  "Permitted",
		},
		{
			name:        "write into blocked path still denied by path glob",
			action:      "file_write",
			details:     map[string]string{"path": "/etc/passwd"},
			wantAllowed: false,
			wantReason:  "Path matches blocked pattern: /etc",
		},
		{
			name:        "shell command allowed",
			action:      "shell_execute",
			details:     map[string]string{"command": "ls -la"},
			wantAllowed: true,
			wantReason:  "Permitted",
		},
		{
			name:        "unknown action still denied",
			action:      "not_a_real_action",
			details:     map[string]string{},
			wantAllowed: false,
			wantReason:  "Unknown action: not_a_real_action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pc.CheckPermissionForAgent(tt.action, tt.details, "")
			if got.Allowed != tt.wantAllowed {
				t.Fatalf("Allowed = %v, want %v (reason %q)", got.Allowed, tt.wantAllowed, got.Reason)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

// TestFenceStage_SetFenceTypedNilGuard: SetFence must guard against typed
// nil interface values — storing (*fakeFence)(nil) must behave exactly like
// no fence (no panic, legacy results).
func TestFenceStage_SetFenceTypedNilGuard(t *testing.T) {
	pc := NewPermissionChecker(Config{
		AllowedPaths: []string{"/tmp"},
	})
	var typedNil *fakeFence
	pc.SetFence(typedNil) // must not store a non-nil interface holding nil

	got := pc.CheckPermissionForAgent("file_write", map[string]string{"path": "/tmp/x"}, "")
	if !got.Allowed || got.Reason != "Permitted" {
		t.Fatalf("typed-nil fence not guarded: Allowed=%v Reason=%q, want legacy allow", got.Allowed, got.Reason)
	}
}

// TestFenceStage_Deny: a fence that denies the /tmp/x write must turn the
// legacy allow into Allowed=false with Reason equal to the fence error.
func TestFenceStage_Deny(t *testing.T) {
	fence := &fakeFence{
		denyPathPrefix: "/tmp/x",
		errText:        "fence: path outside project root",
	}
	pc := NewPermissionChecker(Config{
		AllowedPaths: []string{"/tmp"},
	})
	pc.SetFence(fence)

	got := pc.CheckPermissionForAgent("file_write", map[string]string{"path": "/tmp/x/secret.txt"}, "")
	if got.Allowed {
		t.Fatalf("fence deny not enforced: Allowed=true reason=%q", got.Reason)
	}
	if got.Reason != "fence: path outside project root" {
		t.Fatalf("Reason = %q, want the fence error text %q", got.Reason, "fence: path outside project root")
	}
}

// TestFenceStage_StageOrder pins C1 ordering:
//
//   - fence runs AFTER the path-glob stage: a path-glob deny short-circuits
//     and the fence is never consulted;
//   - fence runs BEFORE confirmation gating: a fence deny wins over the
//     confirmation result, and a fence pass does not bypass confirmation.
func TestFenceStage_StageOrder(t *testing.T) {
	t.Run("path glob deny wins; fence not consulted", func(t *testing.T) {
		fence := &fakeFence{errText: "should not be called"}
		pc := NewPermissionChecker(Config{
			AllowedPaths: []string{"/tmp"},
			BlockedPaths: []string{"/etc"},
		})
		pc.SetFence(fence)

		got := pc.CheckPermissionForAgent("file_write", map[string]string{"path": "/etc/passwd"}, "")
		if got.Allowed {
			t.Fatalf("blocked path allowed: reason=%q", got.Reason)
		}
		if got.Reason != "Path matches blocked pattern: /etc" {
			t.Fatalf("Reason = %q, want the path-glob deny reason", got.Reason)
		}
		if calls := fence.recordedCalls(); len(calls) != 0 {
			t.Fatalf("fence consulted on path-glob deny: %v", calls)
		}
	})

	t.Run("fence deny wins over confirmation gate", func(t *testing.T) {
		fence := &fakeFence{denyPathPrefix: "/tmp/gated", errText: "fence: denied"}
		pc := NewPermissionChecker(Config{
			AllowedPaths:                []string{"/tmp"},
			RequireConfirmationHigh:     true,
			RequireConfirmationCritical: true,
		})
		pc.SetFence(fence)

		// file_delete is RiskHigh; with RequireConfirmationHigh the legacy
		// result would be the confirmation deny. The fence stage runs
		// BEFORE confirmation, so the fence reason must win.
		got := pc.CheckPermissionForAgent("file_delete", map[string]string{"path": "/tmp/gated/old.txt"}, "")
		if got.Allowed {
			t.Fatalf("fence deny not enforced on file_delete")
		}
		if got.Reason != "fence: denied" {
			t.Fatalf("Reason = %q, want fence reason (fence stage must run before confirmation)", got.Reason)
		}
	})

	t.Run("fence pass does not bypass confirmation", func(t *testing.T) {
		fence := &fakeFence{} // allows everything
		pc := NewPermissionChecker(Config{
			AllowedPaths:            []string{"/tmp"},
			RequireConfirmationHigh: true,
		})
		pc.SetFence(fence)

		got := pc.CheckPermissionForAgent("file_delete", map[string]string{"path": "/tmp/ok.txt"}, "")
		if got.Allowed || !got.NeedsConfirm {
			t.Fatalf("fence pass must not bypass confirmation: Allowed=%v NeedsConfirm=%v", got.Allowed, got.NeedsConfirm)
		}
	})
}

// TestFenceStage_OpMapping pins the engine-port action→op mapping:
// file_read→"read", file_write→"write", file_delete→"delete", and shell
// execution routes through CheckCommand with the workdir defaulting to ".".
func TestFenceStage_OpMapping(t *testing.T) {
	fence := &fakeFence{denyPathPrefix: "/tmp/denied", errText: "fence: denied"}
	pc := NewPermissionChecker(Config{
		AllowedPaths: []string{"/tmp"},
	})
	pc.SetFence(fence)

	tests := []struct {
		name     string
		action   string
		details  map[string]string
		wantCall string
	}{
		{
			name:     "file_read maps to read op",
			action:   "file_read",
			details:  map[string]string{"path": "/tmp/f.txt"},
			wantCall: "path:/tmp/f.txt:read",
		},
		{
			name:     "file_write maps to write op",
			action:   "file_write",
			details:  map[string]string{"path": "/tmp/f.txt"},
			wantCall: "path:/tmp/f.txt:write",
		},
		{
			name:     "file_delete maps to delete op",
			action:   "file_delete",
			details:  map[string]string{"path": "/tmp/f.txt"},
			wantCall: "path:/tmp/f.txt:delete",
		},
		{
			name:     "shell_execute routes to CheckCommand with explicit workdir",
			action:   "shell_execute",
			details:  map[string]string{"command": "make build", "workdir": "/tmp/proj"},
			wantCall: "cmd:make build:/tmp/proj",
		},
		{
			name:     "shell_execute workdir defaults to dot",
			action:   "shell_execute",
			details:  map[string]string{"command": "ls"},
			wantCall: "cmd:ls:.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pc.CheckPermissionForAgent(tt.action, tt.details, "")
			_ = got // result itself not under test here; only the fence routing is
			calls := fence.recordedCalls()
			found := false
			for _, c := range calls {
				if c == tt.wantCall {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("fence call %q not recorded; calls = %v", tt.wantCall, calls)
			}
		})
	}

	t.Run("fence deny on shell workdir", func(t *testing.T) {
		cmdFence := &fakeFence{denyWorkdirPrefix: "/tmp/outside", errText: "fence: command outside root"}
		cmdPC := NewPermissionChecker(Config{})
		cmdPC.SetFence(cmdFence)

		got := cmdPC.CheckPermissionForAgent("shell_execute", map[string]string{
			"command": "make build",
			"workdir": "/tmp/outside/repo",
		}, "")
		if got.Allowed {
			t.Fatalf("shell fence deny not enforced")
		}
		if got.Reason != "fence: command outside root" {
			t.Fatalf("Reason = %q, want fence reason", got.Reason)
		}
	})
}
