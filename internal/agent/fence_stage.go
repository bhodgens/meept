package agent

// fence_stage.go — per-session fence injection (agent-groups branch-01
// leaf 01, docs/plans/20261030-agent-groups/01-permissions/01-fence-stage.md
// T2/T4). Replaces the shared-FenceChecker RootPath mutation: instead of
// retargeting ONE process-wide checker per turn (a cross-session race —
// session A's root applied to session B's checks), every session-scoped
// loop gets a per-session PermissionChecker clone carrying its OWN fence
// instance bound to the session's working directory.

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/groups"
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

// GroupFenceStage is a FenceStage that checks a path/command against EVERY
// root in its list and denies when NO root accepts it (agent-groups leaf
// 04, F6): the group policy's fs_roots are ADDITIVE fence roots — the
// subject may write inside any of them, nothing outside. It is consulted
// IN ADDITION to the session fence (whichever denies first wins).
type GroupFenceStage struct {
	roots []string
}

// NewGroupFenceStage builds the additive group fence over the (non-empty)
// root list. Nil/empty input yields nil (no group fence to enforce).
func NewGroupFenceStage(roots []string) *GroupFenceStage {
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		if r != "" {
			cleaned = append(cleaned, r)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return &GroupFenceStage{roots: cleaned}
}

// CheckPath allows the path when it sits inside ANY group root.
func (g *GroupFenceStage) CheckPath(path, op string) error {
	for _, root := range g.roots {
		if pathInRoot(path, root) {
			return nil
		}
	}
	return fmt.Errorf("fence: %s access denied for %q (outside group roots %v)", op, path, g.roots)
}

// CheckCommand validates the working directory against the group roots
// (the command surface is owned by the session fence; the group gate's
// fs_roots contract is path-boundary scoped).
func (g *GroupFenceStage) CheckCommand(command, workDir string) error {
	if workDir == "" || workDir == "." || pathInRoot(workDir, g.roots[0]) {
		return nil
	}
	for _, root := range g.roots {
		if pathInRoot(workDir, root) {
			return nil
		}
	}
	return fmt.Errorf("fence: command workdir %q denied (outside group roots %v)", workDir, g.roots)
}

// pathInRoot reports whether path resolves inside root (prefix match on
// cleaned absolute forms, same shape as the FenceChecker's own check).
func pathInRoot(path, root string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	return strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) || abs == rootAbs
}

// InjectGroupFence is the exported arm point for daemon-side fence arming
// (agent-groups leaf 04 F6): derives a permission checker carrying the
// record's additive fs_roots fence. Returns base unchanged when there is
// nothing to arm (nil base/record, unmanaged, no roots).
func InjectGroupFence(base *security.PermissionChecker, res *groups.Resolved) *security.PermissionChecker {
	return injectGroupFence(base, res)
}

// injectGroupFence clones the checker and ADDS the group fence stage from
// the loop's resolution record (leaf 04 F6). A record with no FSRoots, an
// unmanaged record, or a nil base returns base unchanged — the session
// fence posture is untouched. When a session fence is ALSO present, both
// stages run: whichever denies first wins (the group roots are additive,
// never a widening of the session root — an allow from the group fence
// still has to pass the session fence, and vice versa).
func injectGroupFence(base *security.PermissionChecker, res *groups.Resolved) *security.PermissionChecker {
	if base == nil || res == nil || !res.Managed {
		return base
	}
	stage := NewGroupFenceStage(res.FSRoots)
	if stage == nil {
		return base
	}
	clone := security.NewPermissionChecker(base.ConfigSnapshot())
	// Carry an existing session fence (if any was already installed on
	// base via a prior SetFence) is not needed here: base is the loop's
	// UN-fenced checker — the session fence is injected by the caller
	// AFTER this when both apply. UnionFence composes both stages.
	clone.SetFence(stage)
	return clone
}

// UnionFence composes two FenceStages into one: every check runs against
// BOTH stages and the first denial wins. Either side may be nil.
func UnionFence(a, b security.FenceStage) security.FenceStage {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return b
	case b == nil:
		return a
	}
	return unionFence{a: a, b: b}
}

type unionFence struct {
	a, b security.FenceStage
}

func (u unionFence) CheckPath(path, op string) error {
	if err := u.a.CheckPath(path, op); err != nil {
		return err
	}
	return u.b.CheckPath(path, op)
}

func (u unionFence) CheckCommand(command, workDir string) error {
	if err := u.a.CheckCommand(command, workDir); err != nil {
		return err
	}
	return u.b.CheckCommand(command, workDir)
}
