package builtin

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// noVerifyKey is the context key for disabling git hooks.
type noVerifyKey struct{}

// WithGitNoVerify returns a context that disables git hooks for git operations.
// This prevents execution of potentially malicious .git/hooks/* scripts.
func WithGitNoVerify(ctx context.Context) context.Context {
	return context.WithValue(ctx, noVerifyKey{}, true)
}

// GitCommitTool creates git commits with validation and multi-commit support.
type GitCommitTool struct {
	tools.ToolDefaults
	workingDir   string
	fenceChecker FenceChecker
}

// BatchCommitEntry represents a single commit in a batch operation.
type BatchCommitEntry struct {
	Message string   `json:"message"`
	Files   []string `json:"files,omitempty"`
}

// BatchCommitResult contains results from a multi-commit batch operation.
type BatchCommitResult struct {
	Success bool              `json:"success"`
	Commits []GitCommitResult `json:"commits,omitempty"`
	Message string            `json:"message"`
}

// NewGitCommitTool creates a new git commit tool.
func NewGitCommitTool(workingDir string) *GitCommitTool {
	// workingDir may be empty; set per-session when a project is bound.
	// Do NOT fall back to os.Getwd() — the daemon's CWD is not the user's project.
	return &GitCommitTool{workingDir: workingDir}
}

// SetFenceChecker installs a path fence so all working_dir and file arguments
// are validated before being passed to git. Passing nil clears the fence.
func (t *GitCommitTool) SetFenceChecker(fc FenceChecker) {
	if fc != nil {
		t.fenceChecker = fc
	}
}

func (t *GitCommitTool) Name() string { return "git_commit" }

func (t *GitCommitTool) Category() string { return "git" }

func (t *GitCommitTool) Description() string {
	return "Create git commits with optional conventional commit format validation. Supports single or multiple commits with file grouping. Use after git_split to create atomic commits in dependency order."
}

func (t *GitCommitTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"message": {
				Type:        schemaTypeString,
				Description: "Commit message (required for single commit). Ignored when 'commits' is provided.",
			},
			"files": {
				Type:        schemaTypeArray,
				Description: "List of files to commit (optional, commits all staged if empty).",
				Items: &llm.ParameterProperty{
					Type:        schemaTypeString,
					Description: "File path to commit",
				},
			},
			"commits": {
				Type:        schemaTypeArray,
				Description: "List of ordered commits for batch atomic commit creation. Each entry is an object with 'message' (string, required) and 'files' (array of strings, optional). Commits are created in order by dependency (source first, then tests, then docs, then config). Use with git_split to create atomic commits.",
				Items: &llm.ParameterProperty{
					Type:        schemaTypeObject,
					Description: "A single commit entry with 'message' and optional 'files'.",
				},
			},
			"validate": {
				Type:        schemaTypeBoolean,
				Description: "Validate commit message against conventional commit format (default: true).",
			},
			"working_dir": {
				Type:        schemaTypeString,
				Description: "Working directory for git command (optional, defaults to current dir).",
			},
		},
		Required: []string{},
	}
}

// GitCommitResult contains the result of commit operation.
type GitCommitResult struct {
	CommitHash string `json:"commit_hash,omitempty"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
}

// Conventional commit type pattern (shared with git_validate)
var gitCommitTypeRegex = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9-]+\))?!?:\s+.+.`)

func (t *GitCommitTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	workingDir := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		workingDir = wd
	}

	// Fence check: validate working directory is inside the sandbox before
	// invoking any git commands. This prevents the LLM from targeting
	// directories outside the project root.
	if t.fenceChecker != nil {
		if err := t.fenceChecker.CheckPath(workingDir, "write"); err != nil {
			return nil, fmt.Errorf("git commit: working_dir fence: %w", err)
		}
	}

	// Distinguish "validate" key absent (default to true) from explicitly
	// set to false (caller wants to bypass validation).
	validate, validateSpecified := args["validate"].(bool)
	if !validateSpecified {
		validate = true // Default to validation enabled
	}

	// Check for batch commit mode
	commitsRaw, hasCommits := args["commits"].([]any)
	if hasCommits && len(commitsRaw) > 0 {
		return t.executeBatchCommits(ctx, workingDir, commitsRaw, validate)
	}

	// Single commit mode
	// Get message
	message, _ := args["message"].(string)
	if message == "" {
		return nil, fmt.Errorf("message required")
	}

	// Validate message if requested
	if validate {
		if err := t.validateCommitMessage(message); err != nil {
			return nil, fmt.Errorf("invalid commit message: %w", err)
		}
	}

	// Get files to commit
	var files []string
	if filesRaw, ok := args["files"].([]any); ok {
		for _, f := range filesRaw {
			if s, ok := f.(string); ok {
				files = append(files, s)
			}
		}
	}

	// Stage files if specified
	if len(files) > 0 {
		for _, file := range files {
			if t.fenceChecker != nil {
				absFile := file
				if !filepath.IsAbs(file) {
					absFile = filepath.Join(workingDir, file)
				}
				if err := t.fenceChecker.CheckPath(absFile, "write"); err != nil {
					return nil, fmt.Errorf("git commit: file %q fence: %w", file, err)
				}
			}
			if _, err := t.runGitCmd(ctx, workingDir, "add", file); err != nil {
				return nil, fmt.Errorf("failed to stage %s: %w", file, err)
			}
		}
	} else {
		// Stage all changes
		if _, err := t.runGitCmd(ctx, workingDir, "add", "-A"); err != nil {
			return nil, fmt.Errorf("failed to stage all changes: %w", err)
		}
	}

	// Create commit
	hash, err := t.createCommit(ctx, workingDir, message)
	if err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	return GitCommitResult{
		CommitHash: hash,
		Success:    true,
		Message:    fmt.Sprintf("Successfully created commit %s", hash[:7]),
	}, nil
}

// executeBatchCommits creates multiple ordered atomic commits.
// Each commit in the list is staged and committed separately in order.
func (t *GitCommitTool) executeBatchCommits(ctx context.Context, workingDir string, commitsRaw []any, validate bool) (any, error) {
	var results []GitCommitResult
	allSuccess := true

	for i, cRaw := range commitsRaw {
		cMap, ok := cRaw.(map[string]any)
		if !ok {
			return BatchCommitResult{
				Success: false,
				Commits: results,
				Message: fmt.Sprintf("commit %d: expected object, got %T", i, cRaw),
			}, fmt.Errorf("commit %d: expected object", i)
		}

		message, _ := cMap["message"].(string)
		if message == "" {
			return BatchCommitResult{
				Success: false,
				Commits: results,
				Message: fmt.Sprintf("commit %d: message required", i),
			}, fmt.Errorf("commit %d: message required", i)
		}

		if validate {
			if err := t.validateCommitMessage(message); err != nil {
				allSuccess = false
				results = append(results, GitCommitResult{
					Success: false,
					Message: fmt.Sprintf("commit %d: invalid message: %v", i, err),
				})
				continue
			}
		}

		// Reset staging area before each commit to ensure clean state
		if _, err := t.runGitCmd(ctx, workingDir, "reset", "HEAD", "--"); err != nil {
			// Not a fatal error — may have no previous state to reset
		}

		var files []string
		if filesRaw, ok := cMap["files"].([]any); ok {
			for _, f := range filesRaw {
				if s, ok := f.(string); ok {
					files = append(files, s)
				}
			}
		}

		if len(files) > 0 {
			for _, file := range files {
				if t.fenceChecker != nil {
					absFile := file
					if !filepath.IsAbs(file) {
						absFile = filepath.Join(workingDir, file)
					}
					if err := t.fenceChecker.CheckPath(absFile, "write"); err != nil {
						allSuccess = false
						results = append(results, GitCommitResult{
							Success: false,
							Message: fmt.Sprintf("commit %d: file %q fence: %v", i, file, err),
						})
						continue
					}
				}
				if _, err := t.runGitCmd(ctx, workingDir, "add", file); err != nil {
					allSuccess = false
					results = append(results, GitCommitResult{
						Success: false,
						Message: fmt.Sprintf("commit %d: failed to stage %s: %v", i, file, err),
					})
					continue
				}
			}
		} else {
			// If no files specified for this commit, stage all remaining changes
			if _, err := t.runGitCmd(ctx, workingDir, "add", "-A"); err != nil {
				allSuccess = false
				results = append(results, GitCommitResult{
					Success: false,
					Message: fmt.Sprintf("commit %d: failed to stage all: %v", i, err),
				})
				continue
			}
		}

		hash, err := t.createCommit(ctx, workingDir, message)
		if err != nil {
			allSuccess = false
			results = append(results, GitCommitResult{
				Success: false,
				Message: fmt.Sprintf("commit %d: %v", i, err),
			})
			continue
		}

		results = append(results, GitCommitResult{
			CommitHash: hash,
			Success:    true,
			Message:    fmt.Sprintf("Successfully created commit %d/%d: %s", i+1, len(commitsRaw), hash[:7]),
		})
	}

	if allSuccess {
		return BatchCommitResult{
			Success: true,
			Commits: results,
			Message: fmt.Sprintf("Successfully created %d atomic commits", len(results)),
		}, nil
	}

	return BatchCommitResult{
		Success: false,
		Commits: results,
		Message: fmt.Sprintf("Created %d/%d commits with errors", countSuccessfulCommits(results), len(commitsRaw)),
	}, nil
}

func countSuccessfulCommits(results []GitCommitResult) int {
	count := 0
	for _, r := range results {
		if r.Success {
			count++
		}
	}
	return count
}

func (t *GitCommitTool) createCommit(ctx context.Context, dir, message string) (string, error) {
	// SECURITY: Add --no-verify to skip git hooks which could execute arbitrary code
	// Git hooks (.git/hooks/*) run automatically and could:
	//   - Exfiltrate data via network
	//   - Modify files outside workspace
	//   - Execute malicious payloads
	// Mitigation: Use --no-verify for untrusted repositories
	args := []string{"commit", "-m", message}

	// Check if no-verify is requested via context value
	if noVerify, ok := ctx.Value(noVerifyKey{}).(bool); ok && noVerify {
		args = append(args, "--no-verify")
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git commit failed: %w, output: %s", err, string(output))
	}

	// Get commit hash from HEAD
	hash, err := t.runGitCmd(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %w", err)
	}

	return strings.TrimSpace(hash), nil
}

func (t *GitCommitTool) validateCommitMessage(message string) error {
	if gitCommitTypeRegex.MatchString(message) {
		return nil
	}

	// Also accept simple non-empty messages as fallback
	if len(strings.TrimSpace(message)) >= 10 {
		return nil
	}

	return fmt.Errorf("message should follow conventional commit format: type(scope): description (e.g., 'feat(api): add user endpoint')")
}

func (t *GitCommitTool) runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// Ensure GitCommitTool implements the Tool interface
var _ tools.Tool = (*GitCommitTool)(nil)
