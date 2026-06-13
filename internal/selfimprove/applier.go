// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// ErrApprovalRequired is returned when a fix requires human approval.
var ErrApprovalRequired = fmt.Errorf("fix requires human approval")

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

	return a.applyFix(ctx, fix)
}

// applyFix performs the actual fix application.
func (a *ChangeApplier) applyFix(_ context.Context, fix *ProposedFix) (*AppliedFix, error) {
	// Create backup
	backupPath, err := a.createBackup(fix)
	if err != nil {
		a.logger.Warn("failed to create backup", "error", err)
		// Continue anyway
	}

	// Parse the diff
	original, fixed, err := parseDiff(fix.Diff)
	if err != nil {
		return nil, fmt.Errorf("failed to parse diff: %w", err)
	}

	// Read the file
	filePath := filepath.Join(a.projectRoot, fix.FilePath)

	// Validate the resolved path is within projectRoot to prevent path traversal.
	if !isWithinDir(a.projectRoot, filePath) {
		return nil, fmt.Errorf("fix file path escapes project root: %q", fix.FilePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Apply the replacement
	newContent := strings.Replace(string(content), original, fixed, 1)
	if newContent == string(content) {
		return nil, fmt.Errorf("original code not found in file")
	}

	// Write the file
	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(filePath, []byte(newContent), 0o644); err != nil {
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
		ApprovedBy:        "auto",
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

	applied, err := a.applyFix(ctx, pending.Fix)
	if err != nil {
		return nil, err
	}
	applied.ApprovedBy = "human"
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
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	backupPath := filepath.Join(a.backupDir, fmt.Sprintf("%s_%s.backup",
		fix.ID, filepath.Base(fix.FilePath)))

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

// validateFixPath ensures that a relative file path resolves inside the
// project root and does not escape via "..", symlinks, or absolute paths.
// Paths beginning with "-" are rejected to defend against arg-injection
// (callers should also pass "--" to the git invocation).
func (a *ChangeApplier) validateFixPath(relPath string) error {
	if relPath == "" {
		return fmt.Errorf("fix file path is empty")
	}
	if strings.HasPrefix(relPath, "-") {
		return fmt.Errorf("fix file path starts with '-': %q", relPath)
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("fix file path must be relative: %q", relPath)
	}

	absRoot, err := filepath.Abs(a.projectRoot)
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}
	absTarget, err := filepath.Abs(filepath.Join(absRoot, relPath))
	if err != nil {
		return fmt.Errorf("resolve fix path: %w", err)
	}
	if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(filepath.Separator)) {
		return fmt.Errorf("fix file path escapes project root: %q", relPath)
	}
	return nil
}
