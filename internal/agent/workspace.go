// Package agent provides the agent loop and related components.
package agent

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

// WorkspaceManager manages per-task workspace directories with git tracking.
// It creates isolated directories under a configurable base path (default
// ~/.meept/workspaces/), initialises each as a git repository, and provides
// methods for committing plans, reviews, and artifacts at every lifecycle stage.
type WorkspaceManager struct {
	mu sync.RWMutex

	baseDir    string
	autoCommit bool
	workspaces map[string]string // task_id -> workspace path
	logger     *slog.Logger
}

// WorkspaceConfig holds configuration for the WorkspaceManager.
type WorkspaceConfig struct {
	BaseDir    string // Root directory for workspaces (default: ~/.meept/workspaces)
	AutoCommit bool   // Automatically commit after writes (default: true)
}

// DefaultWorkspaceConfig returns sensible defaults.
func DefaultWorkspaceConfig() WorkspaceConfig {
	homeDir, _ := os.UserHomeDir()
	return WorkspaceConfig{
		BaseDir:    filepath.Join(homeDir, ".meept", "workspaces"),
		AutoCommit: true,
	}
}

// NewWorkspaceManager creates a new workspace manager.
func NewWorkspaceManager(cfg WorkspaceConfig, logger *slog.Logger) *WorkspaceManager {
	if cfg.BaseDir == "" {
		cfg = DefaultWorkspaceConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkspaceManager{
		baseDir:    cfg.BaseDir,
		autoCommit: cfg.AutoCommit,
		workspaces: make(map[string]string),
		logger:     logger,
	}
}

// Create creates a new workspace directory and initializes it as a git repo.
func (w *WorkspaceManager) Create(ctx context.Context, taskID, description string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	workspace := filepath.Join(w.baseDir, taskID)
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return "", fmt.Errorf("failed to create workspace directory: %w", err)
	}

	w.workspaces[taskID] = workspace

	// Initialize git repo
	if ok, _ := w.gitCmd(ctx, workspace, "init"); !ok {
		w.logger.Warn("workspace: git init failed", "task_id", taskID)
	}

	// Write README with task description
	readme := filepath.Join(workspace, "README.md")
	content := fmt.Sprintf("# Task: %s\n\n%s\n\nCreated: %s\n",
		taskID, description, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(readme, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write README: %w", err)
	}

	if w.autoCommit {
		if err := w.commitInternal(ctx, taskID, "Initial workspace setup"); err != nil {
			w.logger.Warn("workspace: initial commit failed", "task_id", taskID, "error", err)
		}
	}

	w.logger.Info("workspace: created", "task_id", taskID, "path", workspace)
	return workspace, nil
}

// Commit stages and commits changes in the task workspace.
func (w *WorkspaceManager) Commit(ctx context.Context, taskID, message string, paths []string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.commitInternal(ctx, taskID, message, paths...)
}

func (w *WorkspaceManager) commitInternal(ctx context.Context, taskID, message string, paths ...string) error {
	workspace, ok := w.workspaces[taskID]
	if !ok {
		return fmt.Errorf("unknown workspace: %s", taskID)
	}

	// Stage files
	if len(paths) > 0 {
		for _, p := range paths {
			w.gitCmd(ctx, workspace, "add", p)
		}
	} else {
		w.gitCmd(ctx, workspace, "add", "-A")
	}

	// Commit
	ok, output := w.gitCmd(ctx, workspace, "commit", "-m", message, "--allow-empty")
	if !ok {
		if strings.Contains(output, "nothing to commit") {
			return nil // Not a real failure
		}
		return fmt.Errorf("git commit failed: %s", output)
	}

	return nil
}

// TaskPlanInfo represents a task plan for workspace writing.
type TaskPlanInfo struct {
	ID          string
	Description string
	Steps       []TaskStepInfo
}

// TaskStepInfo represents a step in a task plan.
type TaskStepInfo struct {
	ID          string
	Description string
	DependsOn   []string
	ToolHint    string
}

// WritePlan writes PLAN.md into the workspace.
func (w *WorkspaceManager) WritePlan(ctx context.Context, taskID string, plan TaskPlanInfo) (string, error) {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("no workspace for task %s", taskID)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Plan: %s\n\n", plan.Description))
	for i, step := range plan.Steps {
		deps := ""
		if len(step.DependsOn) > 0 {
			deps = fmt.Sprintf(" (depends on: %s)", strings.Join(step.DependsOn, ", "))
		}
		sb.WriteString(fmt.Sprintf("%d. **%s**: %s%s\n", i+1, step.ID, step.Description, deps))
	}
	sb.WriteString("\n")

	planPath := filepath.Join(workspace, "PLAN.md")
	if err := os.WriteFile(planPath, []byte(sb.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write PLAN.md: %w", err)
	}

	if w.autoCommit {
		if err := w.commitInternal(ctx, taskID, "Add task plan"); err != nil {
			w.logger.Warn("workspace: plan commit failed", "task_id", taskID, "error", err)
		}
	}

	return planPath, nil
}

// WriteReview writes REVIEW.md with the LLM analysis.
func (w *WorkspaceManager) WriteReview(ctx context.Context, taskID, analysis string) (string, error) {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("no workspace for task %s", taskID)
	}

	content := fmt.Sprintf("# Plan Review\n\n%s\n\nReviewed: %s\n",
		analysis, time.Now().UTC().Format(time.RFC3339))

	reviewPath := filepath.Join(workspace, "REVIEW.md")
	if err := os.WriteFile(reviewPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write REVIEW.md: %w", err)
	}

	if w.autoCommit {
		if err := w.commitInternal(ctx, taskID, "Add plan review"); err != nil {
			w.logger.Warn("workspace: review commit failed", "task_id", taskID, "error", err)
		}
	}

	return reviewPath, nil
}

// AppendLog appends an entry to the workspace LOG.md.
func (w *WorkspaceManager) AppendLog(ctx context.Context, taskID, entry string) error {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return nil // Silently ignore unknown workspaces
	}

	logPath := filepath.Join(workspace, "LOG.md")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	timestamp := time.Now().UTC().Format(time.RFC3339)
	_, err = fmt.Fprintf(f, "- [%s] %s\n", timestamp, entry)
	return err
}

// GetPath returns the workspace path for a task, or empty string if not found.
func (w *WorkspaceManager) GetPath(taskID string) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.workspaces[taskID]
}

// Status returns git status --short output for the workspace.
func (w *WorkspaceManager) Status(ctx context.Context, taskID string) (string, error) {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return "", nil
	}

	_, output := w.gitCmd(ctx, workspace, "status", "--short")
	return output, nil
}

// Log returns recent git log --oneline output for the workspace.
func (w *WorkspaceManager) Log(ctx context.Context, taskID string, maxEntries int) (string, error) {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return "", nil
	}

	_, output := w.gitCmd(ctx, workspace, "log", "--oneline", fmt.Sprintf("-%d", maxEntries))
	return output, nil
}

// Cleanup removes the workspace directory entirely.
func (w *WorkspaceManager) Cleanup(taskID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	workspace, ok := w.workspaces[taskID]
	if !ok {
		return nil
	}

	delete(w.workspaces, taskID)
	if err := os.RemoveAll(workspace); err != nil {
		w.logger.Error("workspace: cleanup failed", "task_id", taskID, "error", err)
		return err
	}

	w.logger.Info("workspace: cleaned up", "task_id", taskID)
	return nil
}

// gitCmd runs a git command in the workspace and returns (success, output).
func (w *WorkspaceManager) gitCmd(ctx context.Context, workspace string, args ...string) (bool, string) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, string(output)
	}
	return true, strings.TrimSpace(string(output))
}
