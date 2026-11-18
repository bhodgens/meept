// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// ErrApprovalRequired is returned when a fix requires human approval.
var ErrApprovalRequired = fmt.Errorf("fix requires human approval")

// ErrTrustedRoot is returned when a proposed fix targets a trusted-root path:
// the security/eval/auth packages, auth handlers, the self-improvement
// machinery itself, build and CI files, or gate-bearing configuration. The
// component that approves updates must never be writable by those updates.
// Callers should test with errors.Is.
var ErrTrustedRoot = errors.New("trusted root path denied")

// trustedRootPrefixes are path prefixes (slash-separated, relative to the
// project root) the applier refuses to modify. Gate configuration lives in
// meept.json5 (employees.defaults.gate.enabled) and config/employees/*.json5;
// the `meept agents set-gate` CLI remains the only gate mutator. Skills
// (config/skills/, .meept/skills/) intentionally stay writable.
//
// Build and CI files, and auth handlers, are refused here too: an LLM patch
// must not rewrite the build, the module graph, CI, or credential checks.
var trustedRootPrefixes = []string{
	"internal/security/",
	"pkg/security/",
	"internal/eval/",
	"internal/selfimprove/applier.go",
	"internal/selfimprove/validator.go",
	"config/meept.json5",
	"config/employees/",
	// Eval oracle fixtures (harness-eval C5): the runs that judge skill
	// quality must not be editable by the thing being judged.
	"testdata/eval/",
	"Makefile",
	"go.mod",
	"go.sum",
	"Dockerfile",
	".github",
	// Auth package plus the handlers that accept credentials. A patch that
	// hardcodes a credential or disables a check lands in one of these.
	"internal/auth/",
	"internal/comm/http/auth.go",
	"internal/comm/http/identity.go",
	"internal/comm/http/pairing.go",
	"internal/comm/web/auth.go",
	"internal/employee/handler_auth.go",
}

// deniedBuildFiles are build-graph filenames refused at any depth, not only
// at the project root. A nested Dockerfile is still a Dockerfile.
var deniedBuildFiles = []string{"Makefile", "go.mod", "go.sum", "Dockerfile"}

// gateFrontmatterDeny marks the YAML frontmatter block of roster AGENT.md
// files. C5 (harness-eval): `meept agents set-gate` is the only gate
// mutator, and roster agents carry `gate:` in their AGENT.md frontmatter —
// so the applier may edit an AGENT.md prompt body but never its frontmatter
// (role/capabilities/gate live there). A whole-file deny would block the
// prompt-body writes C5 explicitly allows, hence the section-level guard.
const gateFrontmatterDeny = "config/agents/"

// isGateFrontmatterPath reports whether relPath is a roster AGENT.md whose
// frontmatter block must stay applier-untouchable.
func isGateFrontmatterPath(relPath string) bool {
	cleaned := filepath.Clean(strings.ReplaceAll(relPath, "\\", "/"))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return false // odd shapes fall through to denyTrustedRoot's own checks
	}
	return strings.HasPrefix(cleaned, gateFrontmatterDeny) && strings.HasSuffix(cleaned, "/AGENT.md")
}

// denyGateFrontmatter rejects diffs that touch the YAML frontmatter block
// (the `---`-delimited head) of a roster AGENT.md. Instruction bodies stay
// writable per C5.
func denyGateFrontmatter(relPath, diff string) error {
	if !isGateFrontmatterPath(relPath) {
		return nil
	}
	if strings.Contains(diff, "\n---\n") {
		return fmt.Errorf("%w: %s frontmatter", ErrTrustedRoot, relPath)
	}
	return nil
}

// ChangeApplier applies validated fixes to the codebase.
type ChangeApplier struct {
	mu sync.RWMutex

	config      SafetyConfig
	projectRoot string
	bus         *bus.MessageBus
	logger      *slog.Logger

	// Pending approvals
	pendingApprovals map[string]*pendingFix

	// Backup directory
	backupDir string

	// verify runs in the temporary worktree after the patch is written there
	// and before any live-tree write. Nil fails closed: the live file is not
	// written. The production caller installs verifyTouchedPackage; tests
	// inject their own.
	verify PatchVerifier
}

type pendingFix struct {
	Fix         *ProposedFix
	Validation  *ValidationResult
	RequestedAt time.Time
}

// NewChangeApplier creates a new ChangeApplier.
func NewChangeApplier(cfg SafetyConfig, projectRoot string, msgBus *bus.MessageBus, logger *slog.Logger) *ChangeApplier {
	if logger == nil {
		logger = slog.Default()
	}

	homeDir, _ := os.UserHomeDir()
	backupDir := filepath.Join(homeDir, ".meept", "selfimprove", "backups")
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		logger.Warn("failed to create backup directory", "error", err)
	}

	return &ChangeApplier{
		config:           cfg,
		projectRoot:      projectRoot,
		bus:              msgBus,
		logger:           logger,
		pendingApprovals: make(map[string]*pendingFix),
		backupDir:        backupDir,
	}
}

// PatchVerifier checks a patched worktree before the live tree is updated.
// worktreeRoot is the temporary git worktree. relPath is the patched file,
// slash-separated, relative to that root. A non-nil error discards the
// worktree and leaves the live file unchanged.
type PatchVerifier func(ctx context.Context, worktreeRoot, relPath string) error

// SetVerifier installs the worktree verifier. A nil verifier fails closed:
// Apply refuses to write the live tree. The receiver nil guard matches the
// other setters; a nil function value is a valid clear.
func (a *ChangeApplier) SetVerifier(v PatchVerifier) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.verify = v
	a.mu.Unlock()
}

// Apply applies a validated fix.
func (a *ChangeApplier) Apply(ctx context.Context, fix *ProposedFix, validation *ValidationResult, approvedBy string) (*AppliedFix, error) {
	// Check if approval is required
	if a.config.RequireHumanApproval && approvedBy != "human" {
		if !a.config.AutoApplyLowRisk || fix.Risk != "low" {
			// Add to pending approvals
			a.mu.Lock()
			a.pendingApprovals[fix.ID] = &pendingFix{
				Fix:         fix,
				Validation:  validation,
				RequestedAt: time.Now(),
			}
			a.mu.Unlock()

			return nil, ErrApprovalRequired
		}
	}

	return a.applyFix(ctx, fix, approvedBy)
}

// applyFix performs the actual fix application.
func (a *ChangeApplier) applyFix(ctx context.Context, fix *ProposedFix, approvedBy string) (*AppliedFix, error) {
	// Deny trusted-root targets BEFORE any backup or write: the approver
	// must not be updatable by the updates it approves, and rejecting here
	// also prevents traversal targets from being copied into backups.
	if err := a.denyTrustedRoot(fix.FilePath); err != nil {
		a.logger.Warn("fix denied: trusted root", "fix_id", fix.ID, "path", fix.FilePath)
		return nil, err
	}

	// Deny AGENT.md frontmatter edits (harness-eval C5): gate commands are
	// CLI-only, and roster AGENT.md frontmatter carries the gate block.
	// Checked before backup for the same reason as above.
	if err := denyGateFrontmatter(fix.FilePath, fix.Diff); err != nil {
		a.logger.Warn("fix denied: gate frontmatter", "fix_id", fix.ID, "path", fix.FilePath)
		return nil, err
	}

	// Parse the diff and compute the replacement in memory. The live file
	// is not written until a worktree verify passes.
	original, fixed, err := parseDiff(fix.Diff)
	if err != nil {
		return nil, fmt.Errorf("failed to parse diff: %w", err)
	}

	filePath := filepath.Join(a.projectRoot, fix.FilePath)

	// Validate the resolved path is within projectRoot to prevent path traversal.
	if !isWithinDir(a.projectRoot, filePath) {
		return nil, fmt.Errorf("fix file path escapes project root: %q", fix.FilePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Apply the replacement.
	// Fail closed when the snippet is ambiguous rather than silently patching
	// the wrong site (S2-10).
	if n := strings.Count(string(content), original); n > 1 {
		return nil, fmt.Errorf("ambiguous fix: original snippet appears %d times in file; refusing to patch without explicit location", n)
	}
	newContent := strings.Replace(string(content), original, fixed, 1)
	if newContent == string(content) {
		return nil, fmt.Errorf("original code not found in file")
	}

	verified, err := a.stageInWorktree(ctx, fix.FilePath, []byte(newContent))
	if err != nil {
		return nil, err
	}

	// Backup the still-original live file, then copy the verified bytes back.
	backupPath, err := a.createBackup(fix)
	if err != nil {
		a.logger.Warn("failed to create backup", "error", err)
		// Continue anyway
	}

	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(filePath, verified, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Optionally create a git commit
	commitHash := ""
	if a.hasGit() {
		hash, err := a.createCommit(fix)
		if err != nil {
			a.logger.Warn("failed to create commit", "error", err)
		} else {
			commitHash = hash
		}
	}

	applied := &AppliedFix{
		FixID:             fix.ID,
		AppliedAt:         time.Now(),
		ApprovedBy:        approvedBy,
		CommitHash:        commitHash,
		RollbackAvailable: backupPath != "",
		BackupPath:        backupPath,
		OriginalPath:      fix.FilePath,
	}

	a.logger.Info("fix applied", "fix_id", fix.ID, "file", fix.FilePath)
	return applied, nil
}

// Approve approves a pending fix.
func (a *ChangeApplier) Approve(ctx context.Context, fixID string) (*AppliedFix, error) {
	a.mu.Lock()
	pending, ok := a.pendingApprovals[fixID]
	if !ok {
		a.mu.Unlock()
		return nil, fmt.Errorf("no pending approval for fix %s", fixID)
	}
	delete(a.pendingApprovals, fixID)
	a.mu.Unlock()

	applied, err := a.applyFix(ctx, pending.Fix, "human")
	if err != nil {
		return nil, err
	}
	return applied, nil
}

// Reject rejects a pending fix.
func (a *ChangeApplier) Reject(fixID, reason string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.pendingApprovals[fixID]; !ok {
		return fmt.Errorf("no pending approval for fix %s", fixID)
	}

	delete(a.pendingApprovals, fixID)
	a.logger.Info("fix rejected", "fix_id", fixID, "reason", reason)
	return nil
}

// Rollback rolls back an applied fix.
func (a *ChangeApplier) Rollback(applied *AppliedFix) error {
	if !applied.RollbackAvailable || applied.BackupPath == "" {
		return fmt.Errorf("rollback not available for fix %s", applied.FixID)
	}

	// Read backup
	backupContent, err := os.ReadFile(applied.BackupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	// Restore file to its original location. Prefer the explicitly recorded
	// OriginalPath; fall back to the legacy convention for older AppliedFix
	// records that pre-date the field.
	var originalPath string
	if applied.OriginalPath != "" {
		originalPath = filepath.Join(a.projectRoot, applied.OriginalPath)
	} else {
		legacy := strings.TrimSuffix(applied.BackupPath, ".backup")
		originalPath = filepath.Join(a.projectRoot, filepath.Base(legacy))
	}

	// Validate the resolved path is within projectRoot to prevent path
	// traversal via a malicious OriginalPath.
	if !isWithinDir(a.projectRoot, originalPath) {
		return fmt.Errorf("rollback target path escapes project root: %q", originalPath)
	}

	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(originalPath, backupContent, 0o644); err != nil {
		return fmt.Errorf("failed to restore file: %w", err)
	}

	a.logger.Info("fix rolled back", "fix_id", applied.FixID)
	return nil
}

// PendingApprovals returns the map of pending approvals.
func (a *ChangeApplier) PendingApprovals() map[string]*pendingFix { //nolint:revive // diagnostic method
	a.mu.RLock()
	defer a.mu.RUnlock()
	// Return a copy
	result := make(map[string]*pendingFix)
	maps.Copy(result, a.pendingApprovals)
	return result
}

// isWithinDir reports whether target resolves inside dir.
func isWithinDir(dir, target string) bool {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) || absTarget == absDir
}

// createBackup creates a backup of the file being modified.
func (a *ChangeApplier) createBackup(fix *ProposedFix) (string, error) {
	filePath := filepath.Join(a.projectRoot, fix.FilePath)

	// Validate the resolved path is within projectRoot before reading to
	// prevent path traversal from exfiltrating sensitive file contents.
	if !isWithinDir(a.projectRoot, filePath) {
		return "", fmt.Errorf("fix file path escapes project root: %q", fix.FilePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	// Never join raw fix data into the destination: both parts are reduced
	// to their base name and stripped of separators so the backup always
	// lands directly inside backupDir, regardless of what ID or path the
	// proposed fix carries.
	safeID := sanitizeBackupComponent(fix.ID)
	backupPath := filepath.Join(a.backupDir, fmt.Sprintf("%s_%s.backup",
		safeID, filepath.Base(fix.FilePath)))

	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(backupPath, content, 0o644); err != nil {
		return "", err
	}

	return backupPath, nil
}

// hasGit checks if the project has a git repository.
func (a *ChangeApplier) hasGit() bool {
	gitDir := filepath.Join(a.projectRoot, ".git")
	info, err := os.Stat(gitDir)
	return err == nil && info.IsDir()
}

// createCommit creates a git commit for the fix.
func (a *ChangeApplier) createCommit(fix *ProposedFix) (string, error) {
	// Validate fix.FilePath is within projectRoot to prevent path traversal
	// and arg-injection via leading "-" characters.
	if err := a.validateFixPath(fix.FilePath); err != nil {
		return "", err
	}

	// Stage the file. The `--` separator prevents git from interpreting
	// a path beginning with `-` as an option flag.
	//nolint:gosec // validated input
	cmd := exec.Command("git", "add", "--", fix.FilePath)
	cmd.Dir = a.projectRoot
	if err := cmd.Run(); err != nil {
		return "", err
	}

	// Create commit
	message := fmt.Sprintf("fix(selfimprove): %s\n\nFix ID: %s\nRisk: %s",
		fix.Description, fix.ID, fix.Risk)

	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = a.projectRoot
	if err := cmd.Run(); err != nil {
		return "", err
	}

	// Get commit hash
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = a.projectRoot
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// sanitizeBackupComponent reduces s to a safe single path component: only
// [A-Za-z0-9._-] survive and every other rune becomes "_", so no separator
// and therefore no traversal can survive. (".." is harmless once separators
// are gone.) Used for attacker-influenced strings (fix IDs) that are
// formatted into file names.
func sanitizeBackupComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." {
		return "unnamed"
	}
	return out
}

// denyTrustedRoot reports whether relPath targets a trusted-root location
// and must never be modified by the applier. Denied when the path is
// absolute, contains any ".." segment, escapes projectRoot via symlink
// resolution (only the deepest existing ancestor is resolved, so not-yet-
// created files are fine), or matches a trusted prefix. Malformed paths
// (empty, dash-prefixed) return a plain validation error WITHOUT the
// ErrTrustedRoot sentinel; trusted-root and traversal denials wrap
// ErrTrustedRoot with %w so callers can test with errors.Is.
func (a *ChangeApplier) denyTrustedRoot(relPath string) error {
	// Malformed input: plain validation errors, not trusted-root denials.
	if relPath == "" {
		return fmt.Errorf("fix file path is empty")
	}
	if strings.HasPrefix(relPath, "-") {
		return fmt.Errorf("fix file path starts with '-': %q", relPath)
	}

	cleaned := filepath.Clean(strings.ReplaceAll(relPath, "\\", "/"))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "/") {
		return fmt.Errorf("%w: %s", ErrTrustedRoot, relPath)
	}
	if slices.Contains(strings.Split(cleaned, "/"), "..") {
		return fmt.Errorf("%w: %s", ErrTrustedRoot, relPath)
	}

	for _, prefix := range trustedRootPrefixes {
		if matchesTrustedPrefix(cleaned, prefix) {
			return fmt.Errorf("%w: %s", ErrTrustedRoot, relPath)
		}
	}
	if isDeniedBuildFile(cleaned) {
		return fmt.Errorf("%w: %s", ErrTrustedRoot, relPath)
	}

	// Symlink containment: resolve the deepest EXISTING ancestor so paths
	// whose final components do not exist yet are still checked against
	// the root they would be created under. The root itself is resolved
	// too: it may sit behind symlinks (e.g. /var -> /private/var on macOS),
	// and both sides must be canonical for the prefix check to hold.
	absRoot, err := filepath.Abs(a.projectRoot)
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}
	if r, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = r
	} else {
		current := absRoot
		for {
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			if r, err := filepath.EvalSymlinks(parent); err == nil {
				absRoot = filepath.Join(r, filepath.Base(current))
				break
			}
			current = parent
		}
	}
	joined := filepath.Join(absRoot, filepath.FromSlash(cleaned))
	resolved := joined
	if current := joined; current != "" {
		for {
			r, err := filepath.EvalSymlinks(current)
			if err == nil {
				if rest, relErr := filepath.Rel(current, joined); relErr == nil && rest != "." {
					resolved = filepath.Join(r, rest)
				} else {
					resolved = r
				}
				break
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	if resolved != absRoot && !strings.HasPrefix(resolved, absRoot+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s", ErrTrustedRoot, relPath)
	}
	return nil
}

// validateFixPath ensures that a relative file path resolves inside the
// project root and does not escape via "..", symlinks, or absolute paths.
// Paths beginning with "-" are rejected to defend against arg-injection
// (callers should also pass "--" to the git invocation). It also enforces
// the trusted-root deny list.
func (a *ChangeApplier) validateFixPath(relPath string) error {
	if err := a.denyTrustedRoot(relPath); err != nil {
		return err
	}
	return nil
}

// matchesTrustedPrefix reports whether cleaned matches a deny-list entry.
// Directory prefixes (trailing slash) match any continuation. File prefixes
// match the exact path or a boundary continuation (".bak", a child segment)
// so "go.mod" does not deny "go.modules".
func matchesTrustedPrefix(cleaned, prefix string) bool {
	if cleaned == prefix {
		return true
	}
	if !strings.HasPrefix(cleaned, prefix) {
		return false
	}
	if strings.HasSuffix(prefix, "/") {
		return true
	}
	rest := cleaned[len(prefix):]
	if rest == "" {
		return true
	}
	switch rest[0] {
	case '/', '.':
		return true
	default:
		return false
	}
}

// isDeniedBuildFile reports whether cleaned's final component is a build
// file, including extension variants such as Dockerfile.dev.
func isDeniedBuildFile(cleaned string) bool {
	base := cleaned
	if _, after, ok := strings.CutLast(cleaned, "/"); ok {
		base = after
	}
	for _, name := range deniedBuildFiles {
		if base == name || strings.HasPrefix(base, name+".") {
			return true
		}
	}
	return false
}

// stageInWorktree writes content into a temporary git worktree, runs the
// caller-supplied verifier, and returns the verified bytes. The worktree is
// discarded on every path. The live tree is not modified.
func (a *ChangeApplier) stageInWorktree(ctx context.Context, relPath string, content []byte) ([]byte, error) {
	a.mu.RLock()
	verify := a.verify
	a.mu.RUnlock()
	if verify == nil {
		return nil, fmt.Errorf("worktree verify is not configured")
	}

	parent, err := os.MkdirTemp("", "meept-selfimprove-verify-")
	if err != nil {
		return nil, fmt.Errorf("create verify dir: %w", err)
	}
	wt := filepath.Join(parent, "wt")
	defer a.discardWorktree(parent, wt)

	add := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", wt, "HEAD")
	add.Dir = a.projectRoot
	add.Env = gitEnv()
	out, err := add.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("create verify worktree: %w: %s", err, strings.TrimSpace(string(out)))
	}

	cleaned := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(relPath, "\\", "/")))
	dest := filepath.Join(wt, filepath.FromSlash(cleaned))
	if !isWithinDir(wt, dest) {
		return nil, fmt.Errorf("%w: %s", ErrTrustedRoot, relPath)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, fmt.Errorf("create worktree parent: %w", err)
	}
	//nolint:gosec // worktree file, discarded after verify
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return nil, fmt.Errorf("write worktree file: %w", err)
	}
	if err := verify(ctx, wt, cleaned); err != nil {
		return nil, fmt.Errorf("worktree verify failed: %w", err)
	}
	verified, err := os.ReadFile(dest)
	if err != nil {
		return nil, fmt.Errorf("read verified file: %w", err)
	}
	return verified, nil
}

// discardWorktree removes a temporary verify worktree and its parent dir.
// Cleanup errors are logged; they are not the caller's apply error.
func (a *ChangeApplier) discardWorktree(parent, wt string) {
	if wt != "" {
		if info, statErr := os.Stat(wt); statErr == nil && info.IsDir() {
			cmd := exec.Command("git", "worktree", "remove", "--force", wt)
			cmd.Dir = a.projectRoot
			cmd.Env = gitEnv()
			if err := cmd.Run(); err != nil {
				a.logger.Warn("worktree remove failed", "path", wt, "error", err)
			}
		}
		prune := exec.Command("git", "worktree", "prune")
		prune.Dir = a.projectRoot
		prune.Env = gitEnv()
		if err := prune.Run(); err != nil {
			a.logger.Warn("worktree prune failed", "error", err)
		}
	}
	if parent != "" {
		if err := os.RemoveAll(parent); err != nil {
			a.logger.Warn("verify dir cleanup failed", "path", parent, "error", err)
		}
	}
}

// gitEnv returns the process environment without git-dir overrides, so a
// command rooted at projectRoot does not follow the caller's GIT_DIR.
func gitEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_DIR=") ||
			strings.HasPrefix(e, "GIT_WORK_TREE=") ||
			strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// verifyTouchedPackage is the production verifier. It builds the Go package
// that contains relPath inside the worktree. Non-Go files have no package
// to build and pass. The unit-test pin is a caller-supplied failing
// verifier, not this function.
func verifyTouchedPackage(ctx context.Context, worktreeRoot, relPath string) error {
	cleaned := filepath.ToSlash(filepath.Clean(relPath))
	if !strings.HasSuffix(cleaned, ".go") {
		return nil
	}
	dir := cleaned
	if before, _, ok := strings.CutLast(cleaned, "/"); ok {
		dir = before
	} else {
		dir = "."
	}
	pkg := "."
	if dir != "." && dir != "" {
		pkg = "./" + dir
	}
	cmd := exec.CommandContext(ctx, "go", "build", pkg)
	cmd.Dir = worktreeRoot
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build %s: %w: %s", pkg, err, strings.TrimSpace(string(out)))
	}
	return nil
}
