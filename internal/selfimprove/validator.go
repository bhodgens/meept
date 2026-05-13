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
	"time"
)

// FixValidator validates proposed fixes in a sandbox.
type FixValidator struct {
	config      SandboxConfig
	safety      SafetyConfig
	projectRoot string
	logger      *slog.Logger

	// Active sandboxes
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
	if err := os.MkdirAll(sandboxPath, 0755); err != nil { //nolint:gosec // task workspace dirs are user-readable
		return "", err
	}
	v.sandboxes[fixID] = sandboxPath
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
	original, fixed, err := v.parseDiff(fix.Diff)
	if err != nil {
		return err
	}

	// Read the file
	filePath := filepath.Join(sandboxPath, fix.FilePath)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// Apply the replacement
	newContent := strings.Replace(string(content), original, fixed, 1)
	if newContent == string(content) {
		return fmt.Errorf("original code not found in file")
	}

	return os.WriteFile(filePath, []byte(newContent), 0644) //nolint:gosec // workspace plan/data files are user-readable
}

// parseDiff parses a conflict-style diff.
func (v *FixValidator) parseDiff(diff string) (original, fixed string, err error) {
	lines := strings.Split(diff, "\n")
	inOriginal := false
	inFixed := false
	var origLines, fixedLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "<<<<<<< ORIGINAL") {
			inOriginal = true
			continue
		}
		if line == "=======" {
			inOriginal = false
			inFixed = true
			continue
		}
		if strings.HasPrefix(line, ">>>>>>> FIXED") {
			inFixed = false
			continue
		}

		if inOriginal {
			origLines = append(origLines, line)
		}
		if inFixed {
			fixedLines = append(fixedLines, line)
		}
	}

	if len(origLines) == 0 && len(fixedLines) == 0 {
		return "", "", fmt.Errorf("could not parse diff")
	}

	return strings.Join(origLines, "\n"), strings.Join(fixedLines, "\n"), nil
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
	if path, ok := v.sandboxes[fixID]; ok {
		os.RemoveAll(path)
		delete(v.sandboxes, fixID)
	}
}

// Cleanup cleans up all sandboxes.
func (v *FixValidator) Cleanup() error {
	for fixID := range v.sandboxes {
		v.cleanupSandbox(fixID)
	}
	return nil
}
