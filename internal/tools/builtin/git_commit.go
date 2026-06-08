package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// GitCommitTool creates git commits with validation and multi-commit support.
type GitCommitTool struct {
	workingDir string
}

// NewGitCommitTool creates a new git commit tool.
func NewGitCommitTool(workingDir string) *GitCommitTool {
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	return &GitCommitTool{workingDir: workingDir}
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
				Description: "Commit message (required for single commit).",
			},
			"files": {
				Type:        schemaTypeArray,
				Description: "List of files to commit (optional, commits all staged if empty).",
				Items: &llm.ParameterProperty{
					Type:        schemaTypeString,
					Description: "File path to commit",
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

	validate, _ := args["validate"].(bool)
	if !validate {
		validate = true // Default to validation enabled
	}

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

func (t *GitCommitTool) createCommit(ctx context.Context, dir, message string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
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
