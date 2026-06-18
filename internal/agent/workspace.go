// Package agent provides the agent loop and related components.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// Validate taskID to prevent path traversal
	if strings.Contains(taskID, "..") || strings.ContainsAny(taskID, "/\\") {
		return "", fmt.Errorf("invalid task ID: must not contain path separators or parent references")
	}

	// Read baseDir under lock, then release for I/O.
	w.mu.RLock()
	baseDir := w.baseDir
	w.mu.RUnlock()

	workspace := filepath.Join(baseDir, taskID)
	if err := os.MkdirAll(workspace, 0o755); err != nil { //nolint:gosec // task workspace dirs are user-readable
		return "", fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Initialize git repo
	if ok, _ := w.gitCmd(ctx, workspace, "init"); !ok {
		w.logger.Warn("workspace: git init failed", "task_id", taskID)
	}

	// Write README with task description
	readme := filepath.Join(workspace, "README.md")
	content := fmt.Sprintf("# Task: %s\n\n%s\n\nCreated: %s\n",
		taskID, description, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(readme, []byte(content), 0o644); err != nil { //nolint:gosec // workspace plan/data files are user-readable
		return "", fmt.Errorf("failed to write README: %w", err)
	}

	// Register the workspace path under the lock.
	w.mu.Lock()
	// Check for duplicate registration (TOCTOU: another goroutine may have
	// created the same workspace concurrently).
	if existing, ok := w.workspaces[taskID]; ok {
		w.mu.Unlock()
		// Another goroutine already registered this workspace; return it.
		return existing, nil
	}
	w.workspaces[taskID] = workspace
	autoCommit := w.autoCommit
	w.mu.Unlock()

	if autoCommit {
		if err := w.commitInternal(ctx, taskID, "Initial workspace setup"); err != nil {
			w.logger.Warn("workspace: initial commit failed", "task_id", taskID, "error", err)
		}
	}

	w.logger.Info("workspace: created", "task_id", taskID, "path", workspace)
	return workspace, nil
}

// Commit stages and commits changes in the task workspace.
func (w *WorkspaceManager) Commit(ctx context.Context, taskID, message string, paths []string) error {
	return w.commitInternal(ctx, taskID, message, paths...)
}

func (w *WorkspaceManager) commitInternal(ctx context.Context, taskID, message string, paths ...string) error {
	// Snapshot workspace path under lock, then release for git I/O.
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
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

// AppendLog appends an entry to the workspace LOG.md.
func (w *WorkspaceManager) AppendLog(ctx context.Context, taskID, entry string) error {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return nil // Silently ignore unknown workspaces
	}

	logPath := filepath.Join(workspace, "LOG.md")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // workspace plan/data files are user-readable
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
	// Snapshot workspace path under lock, then release for I/O.
	w.mu.Lock()
	workspace, ok := w.workspaces[taskID]
	if !ok {
		w.mu.Unlock()
		return nil
	}
	delete(w.workspaces, taskID)
	w.mu.Unlock()

	if err := os.RemoveAll(workspace); err != nil {
		w.logger.Error("workspace: cleanup failed", "task_id", taskID, "error", err)
		return err
	}

	w.logger.Info("workspace: cleaned up", "task_id", taskID)
	return nil
}

// gitCmd runs a git command in the workspace and returns (success, output).
func (w *WorkspaceManager) gitCmd(ctx context.Context, workspace string, args ...string) (ok bool, result string) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, string(output)
	}
	return true, strings.TrimSpace(string(output))
}

// Checkpoint represents a workspace checkpoint for rollback.
type Checkpoint struct {
	TaskID    string    `json:"task_id"`
	Label     string    `json:"label"`
	GitRef    string    `json:"git_ref"`
	Timestamp time.Time `json:"timestamp"`
}

// CreateCheckpoint creates a checkpoint in the workspace using git tags.
// The tag format is: checkpoint-{taskID}-{label}-{timestamp}
func (w *WorkspaceManager) CreateCheckpoint(ctx context.Context, taskID, label string) (*Checkpoint, error) {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no workspace for task %s", taskID)
	}

	// Create checkpoints directory
	checkpointDir := filepath.Join(workspace, "checkpoints", label)
	if err := os.MkdirAll(checkpointDir, 0o755); err != nil { //nolint:gosec // task workspace dirs are user-readable
		w.logger.Warn("Failed to create checkpoint directory",
			"task_id", taskID,
			"label", label,
			"error", err,
		)
		// Continue anyway - git tag is the primary mechanism
	}

	// Generate tag name
	timestamp := time.Now().Unix()
	tagName := fmt.Sprintf("checkpoint-%s-%s-%d", taskID, label, timestamp)

	// Write checkpoint metadata file
	metadata := Checkpoint{
		TaskID:    taskID,
		Label:     label,
		GitRef:    tagName,
		Timestamp: time.Unix(timestamp, 0),
	}
	metadataPath := filepath.Join(checkpointDir, "checkpoint.json")
	data, _ := json.MarshalIndent(metadata, "", "  ")
	if err := os.WriteFile(metadataPath, data, 0o644); err != nil { //nolint:gosec // workspace plan/data files are user-readable
		w.logger.Warn("Failed to write checkpoint metadata", "error", err)
	}

	// Create git tag
	ok, output := w.gitCmd(ctx, workspace, "tag", tagName)
	if !ok {
		return nil, fmt.Errorf("failed to create checkpoint tag: %s", output)
	}

	w.logger.Info("Checkpoint created",
		"task_id", taskID,
		"label", label,
		"tag", tagName,
	)

	return &metadata, nil
}

// RestoreCheckpoint restores a workspace to a previously created checkpoint.
// Returns error if checkpoint does not exist.
func (w *WorkspaceManager) RestoreCheckpoint(ctx context.Context, taskID, label string) error {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no workspace for task %s", taskID)
	}

	// Find the most recent checkpoint tag matching the label
	tagName := fmt.Sprintf("checkpoint-%s-%s", taskID, label)
	_, output := w.gitCmd(ctx, workspace, "tag", "-l", tagName+"-*")
	if output == "" {
		return fmt.Errorf("no checkpoint found with label '%s' for task %s", label, taskID)
	}

	// Get the most recent tag (last in the list, since tags include timestamp)
	tags := strings.Split(strings.TrimSpace(output), "\n")
	if len(tags) == 0 || tags[0] == "" {
		return fmt.Errorf("no checkpoint found with label '%s' for task %s", label, taskID)
	}
	latestTag := tags[len(tags)-1]

	// Checkout the checkpoint tag
	// Note: This puts the repo in detached HEAD state
	ok, output = w.gitCmd(ctx, workspace, "checkout", latestTag)
	if !ok {
		return fmt.Errorf("failed to restore checkpoint '%s': %s", label, output)
	}

	w.logger.Info("Checkpoint restored",
		"task_id", taskID,
		"label", label,
		"tag", latestTag,
	)

	return nil
}

// ListCheckpoints returns all checkpoints for a task.
// AGENT-17 FIX: Tag parsing extracts timestamp from the end (last numeric segment
// after the final dash) and joins everything between taskID and timestamp as the
// label, correctly handling labels containing dashes (e.g. "fix-a-bug-1712345678").
func (w *WorkspaceManager) ListCheckpoints(ctx context.Context, taskID string) ([]Checkpoint, error) {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no workspace for task %s", taskID)
	}

	// List all checkpoint tags for this task
	tagPrefix := fmt.Sprintf("checkpoint-%s-", taskID)
	_, output := w.gitCmd(ctx, workspace, "tag", "-l", tagPrefix+"*")
	if output == "" {
		return []Checkpoint{}, nil
	}

	tags := strings.Split(strings.TrimSpace(output), "\n")
	var checkpoints []Checkpoint

	for _, tag := range tags {
		if tag == "" {
			continue
		}
		// Parse tag: checkpoint-{taskID}-{label}-{timestamp}
		// Use SplitN with limit 4 to handle labels with dashes (e.g., "fix-my-bug")
		// Format: checkpoint-{taskID}-{label-with-dashes}-{timestamp}
		parts := strings.SplitN(tag, "-", 4)
		if len(parts) < 4 {
			continue
		}
		// parts[0] = "checkpoint", parts[1] = taskID, parts[2] = label (may contain dashes), parts[3] = timestamp
		// However, since label can have dashes and timestamp is at the end, we need to split from the end
		// Actually, the tag format is: checkpoint-{taskID}-{label}-{timestamp}
		// where timestamp is the last numeric part
		// Let's find the last hyphen before a pure numeric segment
		lastDash := strings.LastIndex(tag, "-")
		if lastDash == -1 {
			continue
		}
		timestampStr := tag[lastDash+1:]
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			continue
		}

		// Extract prefix (everything before the timestamp)
		prefix := tag[:lastDash]
		// prefix is: checkpoint-{taskID}-{label}
		// We need to extract label by removing "checkpoint-{taskID}-"
		expectedPrefix := fmt.Sprintf("checkpoint-%s-", taskID)
		if !strings.HasPrefix(prefix, expectedPrefix) {
			continue
		}
		label := strings.TrimPrefix(prefix, expectedPrefix)

		checkpoints = append(checkpoints, Checkpoint{
			TaskID:    taskID,
			Label:     label,
			GitRef:    tag,
			Timestamp: time.Unix(timestamp, 0),
		})
	}

	return checkpoints, nil
}

// DeleteCheckpoint removes a checkpoint by deleting the git tag.
func (w *WorkspaceManager) DeleteCheckpoint(ctx context.Context, taskID, label string) error {
	w.mu.RLock()
	workspace, ok := w.workspaces[taskID]
	w.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no workspace for task %s", taskID)
	}

	// Find the checkpoint tag
	tagPrefix := fmt.Sprintf("checkpoint-%s-%s", taskID, label)
	_, output := w.gitCmd(ctx, workspace, "tag", "-l", tagPrefix+"-*")
	if output == "" {
		return fmt.Errorf("checkpoint not found: %s", label)
	}

	tags := strings.SplitSeq(strings.TrimSpace(output), "\n")
	for tag := range tags {
		if tag == "" {
			continue
		}
		// Delete the tag
		ok, delOutput := w.gitCmd(ctx, workspace, "tag", "-d", tag)
		if !ok {
			w.logger.Warn("Failed to delete checkpoint tag",
				"tag", tag,
				"error", delOutput,
			)
		}
	}

	// Remove checkpoint directory if it exists
	checkpointDir := filepath.Join(workspace, "checkpoints", label)
	if err := os.RemoveAll(checkpointDir); err != nil {
		w.logger.Warn("Failed to remove checkpoint directory", "error", err)
	}

	w.logger.Info("Checkpoint deleted",
		"task_id", taskID,
		"label", label,
	)

	return nil
}
