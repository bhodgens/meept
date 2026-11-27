package agent

// fence_stage.go — per-session fence injection (agent-groups branch-01
// leaf 01, docs/plans/20261030-agent-groups/01-permissions/01-fence-stage.md
// T2/T4). Replaces the shared-FenceChecker RootPath mutation: instead of
// retargeting ONE process-wide checker per turn (a cross-session race —
// session A's root applied to session B's checks), every session-scoped
// loop gets a per-session PermissionChecker clone carrying its OWN fence
// instance bound to the session's working directory.
//
// Bughunt H6/A2-1 fix (2026-11-25): the session fence and the group fence
// are now composed in ONE pass via composeLoopFence + UnionFence. The old
// `if fence != nil { ... } else if group` shape was dead — injectSessionFence
// returns base (non-nil) whenever it has nothing to add, so the group
// branch never ran on the chat/dispatch lanes. Per owner ruling D1 the
// group fs_roots are ADDITIVE: a member may write in the session root AND
// any listed fs_roots; the union enforces exactly that boundary.

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
		// Issue #157 re-home: the minting lives in NewSessionFenceStage
		// (the single production construction point the daemon closures
		// also call), so the #138 per-session jail semantics engage on
		// the central FenceStage instead of the retired per-tool
		// fenceChecker field. The AllowRead list below is the
		// pre-jail template posture kept for parity; under the jail an
		// out-of-session read is refused even when AllowRead admits it.
		return NewSessionFenceStage(rootPath, []string{"/usr", "/bin", "/sbin", "/etc"}, noFence, logger)
	}
}

// jailedFenceStage applies the #138 per-session path jail at the CENTRAL
// FenceStage seam (issue #157 re-home). The jail lives HERE, on the
// session-rooted stage the daemon mints per loop, because the per-tool
// fenceChecker wiring it originally shipped on was retired (agent-groups
// leaf 01 T3) and the jail never engaged in production: only the shell
// tool still receives a per-tool checker (internal/services/
// terminal_service.go), and the filesystem tools' checkPathJailed gate
// stayed nil for every daemon session.
//
// The jail narrows the fence in both directions (internal/security/
// fence.go CheckPathJailed contract, jail == the session worktree):
//   - a path inside the session root is allowed unconditionally,
//     short-circuiting the fence's own allowlist;
//   - a path outside it is refused even when AllowRead would admit it.
//
// Symlink resolution happens inside CheckPathJailed on both sides, so a
// link inside the session root that lands outside it is refused.
// CheckCommand keeps the plain checker posture (the command surface is
// the rig-wide fence contract, unchanged by #157).
type jailedFenceStage struct {
	fc       intsecurity.JailedFenceChecker
	jailRoot string
}

func (s jailedFenceStage) CheckPath(path, op string) error {
	return s.fc.CheckPathJailed(path, op, s.jailRoot)
}

func (s jailedFenceStage) CheckCommand(command, workDir string) error {
	return s.fc.CheckCommand(command, workDir)
}

// NewSessionFenceStage mints the production per-session fence stage: a
// FRESH FenceChecker rooted at the session's working directory, wrapped so
// the stage itself enforces the #138 jail narrowing (issue #157). Every
// session lane (daemon components.go factory closures, this package's
// sessionFenceFactory) must mint through this one function so the jail
// posture cannot drift between call sites.
//
// A nil stage (no fence) is returned for an empty rootPath or a
// --nofence session, matching the legacy posture byte-for-byte: unbound
// sessions and explicit no-fence overrides outrank the jail, exactly as
// FenceConfig.NoFence outranks it inside CheckPathJailed.
func NewSessionFenceStage(rootPath string, allowRead []string, noFence bool, logger *slog.Logger) security.FenceStage {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" || noFence {
		return nil
	}
	fc := intsecurity.NewFenceChecker(intsecurity.FenceConfig{
		Enabled:   true,
		RootPath:  rootPath,
		AllowRead: allowRead,
	}, logger)
	return security.NewFenceStageAdapter(jailedFenceStage{fc: fc, jailRoot: rootPath})
}

// injectSessionFenceStage clones base and installs the given session fence
// stage. A nil stage returns base unchanged (byte-identical legacy path).
func injectSessionFenceStage(base *security.PermissionChecker, stage security.FenceStage) *security.PermissionChecker {
	if base == nil || stage == nil {
		return base
	}
	clone := security.NewPermissionChecker(base.ConfigSnapshot())
	clone.SetFence(stage)
	return clone
}

// factoryMint asks the factory for the session fence stage, nil-safe on
// every disabled posture (no factory, --nofence, empty root).
func factoryMint(factory sessionFenceFactory, rootPath string, noFence bool) security.FenceStage {
	if factory == nil || noFence || rootPath == "" {
		return nil
	}
	return factory(rootPath, noFence)
}

// composeLoopFence is the single arm point for BOTH fences on a
// session-scoped loop (bughunt H6/A2-1, owner ruling D1). It computes the
// session fence stage (via the factory) and the group fence stage (from
// the loop's GroupResolution() record), composes them with UnionFence,
// and installs the result on a clone of the loop's checker.
//
// Semantics (D1, additive fs_roots): a managed member may act inside the
// session root AND every listed fs_root; anything outside both is denied
// at the path boundary. Without group roots the call is byte-identical to
// injectSessionFence (Managed=false / nil record contribute nothing).
func composeLoopFence(base *security.PermissionChecker, factory sessionFenceFactory, rootPath string, noFence bool, res *groups.Resolved) *security.PermissionChecker {
	if base == nil {
		return nil
	}
	var sessionStage security.FenceStage
	if !noFence && rootPath != "" {
		sessionStage = factoryMint(factory, rootPath, noFence)
	}
	groupStage := groupFenceStageFor(res, rootPath)
	if groupStage == nil {
		// No group fence to arm: exact legacy posture.
		return injectSessionFenceStage(base, sessionStage)
	}
	clone := security.NewPermissionChecker(base.ConfigSnapshot())
	clone.SetFence(UnionFence(sessionStage, groupStage))
	return clone
}

// GroupFenceStage is a FenceStage that checks a path/command against EVERY
// root in its list and denies when NO root accepts it (agent-groups leaf
// 04, F6): the group policy's fs_roots are ADDITIVE fence roots — the
// subject may write inside any of them, nothing outside. It is consulted
// IN ADDITION to the session fence (see UnionFence for the exact
// composition; per owner ruling D1 the path boundary is the union of
// both root sets).
//
// Bughunt A2-6/M12 fix: comparisons resolve symlinks before the prefix
// test (same fail-hard shape as internal/security/fence.go
// resolveSymlinks), so a link inside a root pointing outside it is refused
// instead of admitted lexically.
type GroupFenceStage struct {
	roots []string
	// sessionRoot is the loop's own working directory folded into the
	// ADDITIVE union per D1 (a member never loses its session dir). When
	// a real session fence stage exists it enforces its own root too;
	// this field matters for the no-session-fence postures (fencing off
	// or --nofence), where the group stage is the only boundary and must
	// still admit the session dir.
	sessionRoot string
}

// NewGroupFenceStage builds the additive group fence over the (non-empty)
// root list. Nil/empty input yields nil (no group fence to enforce).
func NewGroupFenceStage(roots []string) *GroupFenceStage {
	cleaned := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		cleaned = append(cleaned, r)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return &GroupFenceStage{roots: cleaned}
}

// groupFenceStageFor derives the group stage from a resolution record:
// nil/unmanaged records (or records without fs_roots) contribute nothing;
// ~ expansion is applied to the roots (fs_roots documents "~/..."
// shapes, see internal/groups resolve_test) and the session root joins
// the additive union per D1.
func groupFenceStageFor(res *groups.Resolved, sessionRoot string) security.FenceStage {
	if res == nil || !res.Managed || len(res.FSRoots) == 0 {
		return nil
	}
	expanded := make([]string, 0, len(res.FSRoots))
	for _, r := range res.FSRoots {
		if e := expandRootTilde(r); e != "" {
			expanded = append(expanded, e)
		}
	}
	stage := NewGroupFenceStage(expanded)
	if stage == nil {
		return nil
	}
	stage.sessionRoot = sessionRoot
	return stage
}

// expandRootTilde expands a leading "~/" (and bare "~") in a configured
// root to the current user's home, matching the config tilde posture for
// fs_roots (resolve_test seeds "~/a" shapes). Best-effort: os.UserHomeDir
// errors leave the root verbatim.
func expandRootTilde(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if root == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return root
	}
	if strings.HasPrefix(root, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, root[2:])
		}
	}
	return root
}

// allRoots returns the configured roots plus the additive session root
// (deduped).
func (g *GroupFenceStage) allRoots() []string {
	if g.sessionRoot == "" {
		return g.roots
	}
	for _, r := range g.roots {
		if r == g.sessionRoot {
			return g.roots
		}
	}
	return append(g.roots, g.sessionRoot)
}

// isEffectiveWorkDir reports whether dir denotes "the loop's own working
// directory" rather than an explicit path: the executor injects the real
// (absolute) directory before the tool runs, and the central pipeline
// maps an empty shell/job workdir to "." before consulting the fence
// (pkg/security/permissions.go shell_execute/job_start arms). Both forms
// mean the effective cwd, which is inside the boundary by construction
// (bughunt A2-6/M12: the old CheckCommand special-cased "" and "." and
// the roots[0] prefix quirk — the "" / "." posture is kept here
// DELIBERATELY and documented; the roots[0] short-circuit is gone).
func isEffectiveWorkDir(dir string) bool {
	return dir == "" || dir == "."
}

// CheckPath allows the path when it sits inside ANY group root (plus the
// additive session root, D1). Symlinks are resolved on both sides before
// the containment test (A2-6/M12). An empty/"." path denotes the
// effective workdir and is admitted (see isEffectiveWorkDir).
func (g *GroupFenceStage) CheckPath(path, op string) error {
	if isEffectiveWorkDir(path) {
		return nil
	}
	for _, root := range g.allRoots() {
		if pathInRoot(path, root) {
			return nil
		}
	}
	return fmt.Errorf("fence: %s access denied for %q (outside group roots %v)", op, path, g.allRoots())
}

// CheckCommand validates the working directory against the group roots
// (the command surface is owned by the session fence; the group gate's
// fs_roots contract is path-boundary scoped).
//
// workDir == "" or "." is treated as INSIDE: the executor injects the
// loop's real working directory before the tool runs (isEffectiveWorkDir).
// When the command embeds shell substitutions the path checks cannot see
// through ($(), backticks, <() / >()) — the same patterns the session
// FenceChecker refuses outright — the group lane refuses instead of
// admitting the whole command on the strength of its workdir.
func (g *GroupFenceStage) CheckCommand(command, workDir string) error {
	if !isEffectiveWorkDir(workDir) {
		inside := false
		for _, root := range g.allRoots() {
			if pathInRoot(workDir, root) {
				inside = true
				break
			}
		}
		if !inside {
			return fmt.Errorf("fence: command workdir %q denied (outside group roots %v)", workDir, g.allRoots())
		}
	}
	if commandHidesSubstitution(command) {
		return fmt.Errorf("fence: command substitution denied in a group-fenced command (path checks cannot see through it): %q", command)
	}
	return nil
}

// commandHidesSubstitution reports whether a command string carries shell
// substitutions the path checks cannot inspect (same patterns the session
// FenceChecker refuses outright in CheckCommand).
func commandHidesSubstitution(command string) bool {
	return strings.Contains(command, "$(") ||
		strings.Contains(command, "`") ||
		strings.Contains(command, "<(") ||
		strings.Contains(command, ">(")
}

// pathInRoot reports whether path resolves inside root: absolute form,
// symlinks resolved on BOTH sides before the prefix match (A2-6/M12 —
// a link inside the root that escapes it is refused). Fail-hard: a path
// whose symlink resolution cannot be established is NOT inside.
func pathInRoot(path, root string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs, ok := fenceResolveSymlinks(abs)
	if !ok {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rootAbs, ok = fenceResolveSymlinks(rootAbs)
	if !ok {
		return false
	}
	return fencePathWithin(abs, rootAbs)
}

// fencePathWithin reports whether the already-absolute, already-resolved
// candidate sits at or under root (pure lexical containment — both
// arguments must be pre-resolved, same contract as
// internal/security.pathWithin).
func fencePathWithin(abs, root string) bool {
	return strings.HasPrefix(abs, root+string(os.PathSeparator)) || abs == root
}

// fenceSplitPath splits an absolute path into its components (mirror of
// internal/security.splitPath — kept local because pkg/security must not
// import internal and the internal helper is unexported; the shared
// helper lives HERE, in internal/agent, per the fix ruling).
func fenceSplitPath(path string) []string {
	path = filepath.Clean(path)
	if path == "/" {
		return nil
	}
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	return strings.Split(path, "/")
}

// fenceResolveSymlinks resolves symlinks in an absolute path, handling
// non-existent final components (common for write operations) by walking
// the longest-existing prefix (pattern copy of internal/security/fence.go
// resolveSymlinks, A2-6/M12). FAIL-HARD: returns ("", false) when even
// "/" cannot be resolved.
func fenceResolveSymlinks(path string) (string, bool) {
	path = filepath.Clean(path)
	if evaled, err := filepath.EvalSymlinks(path); err == nil {
		return evaled, true
	}
	parts := fenceSplitPath(path)
	resolvedSoFar := "/"
	for i, part := range parts {
		candidate := filepath.Join(resolvedSoFar, part)
		if evaledCandidate, err := filepath.EvalSymlinks(candidate); err == nil {
			resolvedSoFar = evaledCandidate
		} else {
			remaining := filepath.Join(parts[i:]...)
			if resolvedSoFar == "/" {
				return "/" + remaining, true
			}
			return resolvedSoFar + "/" + remaining, true
		}
	}
	return resolvedSoFar, true
}

// InjectGroupFence is the exported arm point for daemon-side fence arming
// (agent-groups leaf 04 F6): derives a permission checker carrying the
// record's additive fs_roots fence. Returns base unchanged when there is
// nothing to arm (nil base/record, unmanaged, no roots).
func InjectGroupFence(base *security.PermissionChecker, res *groups.Resolved) *security.PermissionChecker {
	return injectGroupFence(base, res)
}

// injectGroupFence clones the checker and installs the group fence stage
// from the resolution record (leaf 04 F6). A record with no FSRoots, an
// unmanaged record, or a nil base returns base unchanged — the session
// fence posture is untouched. On the daemon session lanes the session
// fence and the group fence are composed by composeLoopFence (UnionFence,
// H6/A2-1); this single-stage form stays the step-job lane's posture
// (no session-scoped fence instance exists there).
func injectGroupFence(base *security.PermissionChecker, res *groups.Resolved) *security.PermissionChecker {
	if base == nil {
		return base
	}
	stage := groupFenceStageFor(res, "")
	if stage == nil {
		return base
	}
	clone := security.NewPermissionChecker(base.ConfigSnapshot())
	clone.SetFence(stage)
	return clone
}

// UnionFence composes the session fence stage with the additive group
// fence stage (owner ruling D1, bughunt H6/A2-1):
//
//   - CheckPath — the ADDITIVE boundary: a path passes when EITHER stage
//     admits it (fs_roots widen the writable set with the listed roots;
//     the session root and the session stage's system AllowRead lanes
//     stay admitted; a denial requires BOTH stages to refuse).
//   - CheckCommand — first denial wins (the strict surface stays strict):
//     the session FenceChecker's substitution refusal and command token
//     scan can never be overridden by the group stage's more permissive
//     workdir answer, and a workdir the group fence refuses (outside
//     every root including the session dir) is denied even when the
//     session stage would allow it.
//
// Either side may be nil (the other stage's verdict applies verbatim —
// byte-identical to the single-fence postures).
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
	errA := u.a.CheckPath(path, op)
	if errA == nil {
		return nil
	}
	errB := u.b.CheckPath(path, op)
	if errB == nil {
		return nil
	}
	// Both deny: report the session stage's verdict when present (it
	// names the project root the operator configured), else the group's.
	return errA
}

func (u unionFence) CheckCommand(command, workDir string) error {
	if err := u.a.CheckCommand(command, workDir); err != nil {
		return err
	}
	return u.b.CheckCommand(command, workDir)
}
