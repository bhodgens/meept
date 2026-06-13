// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FixValidator validates proposed fixes in a sandbox.
type FixValidator struct {
	config      SandboxConfig
	safety      SafetyConfig
	projectRoot string
	logger      *slog.Logger

	// Active sandboxes
	mu        sync.Mutex
	sandboxes map[string]string // fix_id -> sandbox_path
}

// NewFixValidator creates a new FixValidator.
func NewFixValidator(sandboxCfg SandboxConfig, safetyCfg SafetyConfig, projectRoot string, logger *slog.Logger) *FixValidator {
	if logger == nil {
		logger = slog.Default()
	}
	return &FixValidator{
		config:      sandboxCfg,
		safety:      safetyCfg,
		projectRoot: projectRoot,
		logger:      logger,
		sandboxes:   make(map[string]string),
	}
}

// Validate validates a proposed fix.
func (v *FixValidator) Validate(ctx context.Context, fix *ProposedFix) (*ValidationResult, error) {
	result := &ValidationResult{
		FixID:       fix.ID,
		Status:      ValidationPending,
		ValidatedAt: time.Now(),
	}

	start := time.Now()
	defer func() {
		result.Duration = time.Since(start)
	}()

	// Create sandbox
	sandboxPath, err := v.createSandbox(fix.ID)
	if err != nil {
		result.Status = ValidationFailed
		result.Errors = append(result.Errors, fmt.Sprintf("failed to create sandbox: %v", err))
		return result, nil
	}
	defer v.cleanupSandbox(fix.ID)

	// Copy project to sandbox
	if err := v.copyProject(sandboxPath); err != nil {
		result.Status = ValidationFailed
		result.Errors = append(result.Errors, fmt.Sprintf("failed to copy project: %v", err))
		return result, nil
	}

	// Apply the fix
	if err := v.applyFix(sandboxPath, fix); err != nil {
		result.Status = ValidationFailed
		result.Errors = append(result.Errors, fmt.Sprintf("failed to apply fix: %v", err))
		return result, nil
	}

	// Run build if required
	if v.safety.RequireBuildSuccess {
		if buildErr := v.runBuild(ctx, sandboxPath); buildErr != nil {
			result.Status = ValidationFailed
			result.BuildSuccess = false
			result.Errors = append(result.Errors, fmt.Sprintf("build failed: %v", buildErr))
			return result, nil
		}
		result.BuildSuccess = true
	}

	// Run tests if required
	if v.safety.RequireTestsPass {
		passed, failed, testErr := v.runTests(ctx, sandboxPath)
		result.TestsPassed = passed
		result.TestsFailed = failed
		if testErr != nil || failed > 0 {
			result.Status = ValidationFailed
			if testErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("tests failed: %v", testErr))
			}
			return result, nil
		}
	}

	result.Status = ValidationPassed
	result.Success = true
	return result, nil
}

// ValidateBatch validates multiple fixes.
func (v *FixValidator) ValidateBatch(ctx context.Context, fixes []*ProposedFix) ([]*ValidationResult, error) {
	results := make([]*ValidationResult, 0, len(fixes))

	for _, fix := range fixes {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result, err := v.Validate(ctx, fix)
		if err != nil {
			v.logger.Warn("validation error", "fix_id", fix.ID, "error", err)
			continue
		}
		results = append(results, result)
	}

	return results, nil
}

// createSandbox creates a sandbox directory.
func (v *FixValidator) createSandbox(fixID string) (string, error) {
	sandboxPath := filepath.Join(v.config.WorkDirTemplate, fixID)
	if err := os.MkdirAll(sandboxPath, 0o755); err != nil { //nolint:gosec // task workspace dirs are user-readable
		return "", err
	}
	v.mu.Lock()
	v.sandboxes[fixID] = sandboxPath
	v.mu.Unlock()
	return sandboxPath, nil
}

// copyProject copies the project to the sandbox.
func (v *FixValidator) copyProject(sandboxPath string) error {
	// Use rsync for efficient copying
	cmd := exec.Command("rsync", "-a", "--exclude", ".git", "--exclude", "vendor", //nolint:gosec // path is constructed from known config values
		v.projectRoot+"/", sandboxPath+"/")
	return cmd.Run()
}

// applyFix applies a fix to the sandbox.
func (v *FixValidator) applyFix(sandboxPath string, fix *ProposedFix) error {
	// Parse the diff to extract original and fixed code
	original, fixed, err := parseDiff(fix.Diff)
	if err != nil {
		return err
	}

	// Validate the file path resolves inside the sandbox to prevent path traversal.
	filePath := filepath.Join(sandboxPath, fix.FilePath)
	absTarget, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolve fix path: %w", err)
	}
	absSandbox, err := filepath.Abs(sandboxPath)
	if err != nil {
		return fmt.Errorf("resolve sandbox path: %w", err)
	}
	if absTarget != absSandbox && !strings.HasPrefix(absTarget, absSandbox+string(os.PathSeparator)) {
		return fmt.Errorf("fix file path escapes sandbox: %q", fix.FilePath)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// Apply the replacement
	newContent := strings.Replace(string(content), original, fixed, 1)
	if newContent == string(content) {
		return fmt.Errorf("original code not found in file")
	}

	return os.WriteFile(filePath, []byte(newContent), 0o644) //nolint:gosec // workspace plan/data files are user-readable
}

// runBuild runs the build in the sandbox.
func (v *FixValidator) runBuild(ctx context.Context, sandboxPath string) error {
	ctx, cancel := context.WithTimeout(ctx, v.config.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "./...") //nolint:gosec // path is constructed from known config values
	cmd.Dir = sandboxPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

// runTests runs tests in the sandbox.
func (v *FixValidator) runTests(ctx context.Context, sandboxPath string) (passed, failed int, err error) {
	ctx, cancel := context.WithTimeout(ctx, v.config.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "-v", "-json", "./...") //nolint:gosec // path is constructed from known config values
	cmd.Dir = sandboxPath
	output, err := cmd.CombinedOutput()

	// Parse test output (simplified)
	outputStr := string(output)
	passed = strings.Count(outputStr, `"Action":"pass"`)
	failed = strings.Count(outputStr, `"Action":"fail"`)

	if err != nil {
		return passed, failed, fmt.Errorf("%w: %s", err, outputStr)
	}

	return passed, failed, nil
}

// cleanupSandbox removes the sandbox.
func (v *FixValidator) cleanupSandbox(fixID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if path, ok := v.sandboxes[fixID]; ok {
		os.RemoveAll(path)
		delete(v.sandboxes, fixID)
	}
}

// Cleanup cleans up all sandboxes.
func (v *FixValidator) Cleanup() error {
	v.mu.Lock()
	ids := make([]string, 0, len(v.sandboxes))
	for id := range v.sandboxes {
		ids = append(ids, id)
	}
	v.mu.Unlock()

	for _, id := range ids {
		v.cleanupSandbox(id)
	}
	return nil
}
