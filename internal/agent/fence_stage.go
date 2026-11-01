package agent

// fence_stage.go — per-session fence injection (agent-groups branch-01
// leaf 01, docs/plans/20261030-agent-groups/01-permissions/01-fence-stage.md
// T2/T4). Replaces the shared-FenceChecker RootPath mutation: instead of
// retargeting ONE process-wide checker per turn (a cross-session race —
// session A's root applied to session B's checks), every session-scoped
// loop gets a per-session PermissionChecker clone carrying its OWN fence
// instance bound to the session's working directory.

import (
	"log/slog"

	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/pkg/security"
)

// sessionFenceFactory is the seam the daemon wires to construct a FRESH,
// session-owned fence instance. internal/agent cannot import the daemon's
// concrete *intsecurity.FenceChecker construction policy beyond this
// factory shape (the daemon closes over its config: FenceEnabled,
// AllowRead, logger). A nil factory on the handler/dispatcher means
// fencing is disabled daemon-wide and no fence stage is injected.
type sessionFenceFactory func(rootPath string, noFence bool) security.FenceStage

// newSessionFenceFactory adapts the daemon's shared-fence CONSTRUCTION
// policy (template config) into a factory that mints an independent
// FenceChecker per session. The returned checker is never shared: each
// call constructs a new instance rooted at rootPath (empty root is
// intentionally invalid — a session with no working directory must not
// silently get a permissive fence; the factory then yields a nil stage,
// i.e. no fence, matching the legacy unrooted posture).
func newSessionFenceFactory(logger *slog.Logger) sessionFenceFactory {
	return func(rootPath string, noFence bool) security.FenceStage {
		if rootPath == "" {
			return nil
		}
		if noFence {
			return nil
		}
		fc := intsecurity.NewFenceChecker(intsecurity.FenceConfig{
			Enabled:  true,
			RootPath: rootPath,
			// System reads outside the root stay allowed, matching the
			// daemon template's posture (allow-read-only-outsiders).
			AllowRead: []string{"/usr", "/bin", "/sbin", "/etc"},
		}, logger)
		return security.NewFenceStageAdapter(fc)
	}
}

// injectSessionFence returns a per-session PermissionChecker for the loop:
// a lightweight clone of base that carries the session's own fence stage.
// When no factory is wired (fencing disabled), the session has no working
// directory, the session carries --nofence, or the factory yields no
// fence, base is returned unchanged — the shared checker without any
// fence, byte-identical to the legacy path.
//
// Per-session isolation is the POINT: two concurrent sessions each get a
// checker fenced to ITS OWN root (regression pin:
// TestSessionFence_ConcurrentSessionsIsolated), replacing the old
// shared-RootPath mutation race.
func injectSessionFence(base *security.PermissionChecker, factory sessionFenceFactory, rootPath string, noFence bool) *security.PermissionChecker {
	if base == nil {
		return nil
	}
	if factory == nil || noFence || rootPath == "" {
		return base
	}
	stage := factory(rootPath, noFence)
	if stage == nil {
		return base
	}
	clone := security.NewPermissionChecker(base.ConfigSnapshot())
	clone.SetFence(stage)
	return clone
}
