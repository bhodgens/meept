package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// GitOverviewTool provides a summary of working tree changes.
type GitOverviewTool struct {
	workingDir   string
	fenceChecker FenceChecker
}

// NewGitOverviewTool creates a new git overview tool.
func NewGitOverviewTool(workingDir string) *GitOverviewTool {
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	return &GitOverviewTool{workingDir: workingDir}
}

// SetFenceChecker installs a path fence so working_dir arguments are
// validated before git is invoked. Passing nil clears the fence.
func (t *GitOverviewTool) SetFenceChecker(fc FenceChecker) {
	if fc != nil {
		t.fenceChecker = fc
	}
}

func (t *GitOverviewTool) Name() string { return "git_overview" }

func (t *GitOverviewTool) Category() string { return "git" }

func (t *GitOverviewTool) Description() string {
	return "Summarize working tree changes including staged and unstaged modifications. Returns a structured overview of all changed files with diff stats and change types (added, modified, deleted). Use before committing to understand the scope of changes."
}

func (t *GitOverviewTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"include_untracked": {
				Type:        schemaTypeBoolean,
				Description: "Include untracked files in the overview (default: false).",
			},
			"working_dir": {
				Type:        schemaTypeString,
				Description: "Working directory for git command (optional, defaults to current dir).",
			},
		},
	}
}

// GitOverviewResult contains the working tree summary.
type GitOverviewResult struct {
	Branch          string           `json:"branch"`
	AheadBehind     string           `json:"ahead_behind,omitempty"`
	StagedChanges   []FileChangeInfo `json:"staged_changes,omitempty"`
	UnstagedChanges []FileChangeInfo `json:"unstaged_changes,omitempty"`
	UntrackedFiles  []string         `json:"untracked_files,omitempty"`
	Summary         SummaryStats     `json:"summary"`
}

// FileChangeInfo describes a single file's changes.
type FileChangeInfo struct {
	FilePath  string `json:"file_path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	PrevPath  string `json:"prev_path,omitempty"`
}

// SummaryStats provides aggregate statistics.
type SummaryStats struct {
	TotalFiles     int `json:"total_files"`
	StagedCount    int `json:"staged_count"`
	UnstagedCount  int `json:"unstaged_count"`
	UntrackedCount int `json:"untracked_count"`
	TotalAdditions int `json:"total_additions"`
	TotalDeletions int `json:"total_deletions"`
}

func (t *GitOverviewTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	includeUntracked, _ := args["include_untracked"].(bool)
	workingDir := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		workingDir = wd
	}

	if t.fenceChecker != nil {
		if err := t.fenceChecker.CheckPath(workingDir, "write"); err != nil {
			return nil, fmt.Errorf("git overview: working_dir fence: %w", err)
		}
	}

	result := GitOverviewResult{
		StagedChanges:   make([]FileChangeInfo, 0),
		UnstagedChanges: make([]FileChangeInfo, 0),
		UntrackedFiles:  make([]string, 0),
	}

	// Get current branch
	branch, err := t.runGitCmd(ctx, workingDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to get current branch: %w", err)
	}
	result.Branch = strings.TrimSpace(branch)

	// Get ahead/behind status
	aheadBehind, _ := t.getAheadBehind(ctx, workingDir)
	result.AheadBehind = aheadBehind

	// Get staged changes
	stagedChanges, err := t.getStagedChanges(ctx, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get staged changes: %w", err)
	}
	result.StagedChanges = stagedChanges

	// Get unstaged changes
	unstagedChanges, err := t.getUnstagedChanges(ctx, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get unstaged changes: %w", err)
	}
	result.UnstagedChanges = unstagedChanges

	// Get untracked files if requested
	if includeUntracked {
		untracked, err := t.getUntrackedFiles(ctx, workingDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get untracked files: %w", err)
		}
		result.UntrackedFiles = untracked
	}

	// Calculate summary
	result.Summary = SummaryStats{
		TotalFiles:     len(stagedChanges) + len(unstagedChanges) + len(result.UntrackedFiles),
		StagedCount:    len(stagedChanges),
		UnstagedCount:  len(unstagedChanges),
		UntrackedCount: len(result.UntrackedFiles),
	}
	for _, c := range stagedChanges {
		result.Summary.TotalAdditions += c.Additions
		result.Summary.TotalDeletions += c.Deletions
	}
	for _, c := range unstagedChanges {
		result.Summary.TotalAdditions += c.Additions
		result.Summary.TotalDeletions += c.Deletions
	}

	return result, nil
}

func (t *GitOverviewTool) getAheadBehind(ctx context.Context, dir string) (string, error) {
	upstream, err := t.runGitCmd(ctx, dir, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err != nil {
		return "", nil
	}

	output, err := t.runGitCmd(ctx, dir, "rev-list", "--left-right", "--count", fmt.Sprintf("HEAD...%s", strings.TrimSpace(upstream)))
	if err != nil {
		return "", nil
	}

	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) == 2 {
		return fmt.Sprintf("ahead %s, behind %s", parts[0], parts[1]), nil
	}
	return "", nil
}

func (t *GitOverviewTool) getStagedChanges(ctx context.Context, dir string) ([]FileChangeInfo, error) {
	output, err := t.runGitCmd(ctx, dir, "diff", "--cached", "--name-status")
	if err != nil || strings.TrimSpace(output) == "" {
		return []FileChangeInfo{}, nil
	}
	return t.parseFileStatus(ctx, dir, output, true)
}

func (t *GitOverviewTool) getUnstagedChanges(ctx context.Context, dir string) ([]FileChangeInfo, error) {
	output, err := t.runGitCmd(ctx, dir, "diff", "--name-status")
	if err != nil || strings.TrimSpace(output) == "" {
		return []FileChangeInfo{}, nil
	}
	return t.parseFileStatus(ctx, dir, output, false)
}

func (t *GitOverviewTool) parseFileStatus(ctx context.Context, dir, output string, staged bool) ([]FileChangeInfo, error) {
	var changes []FileChangeInfo

	for _, line := range strings.Split(output, "\n") {
		// Note: do NOT TrimSpace the whole line. The leading character in
		// porcelain v1 format is the X status, which may legitimately be a
		// space (worktree-only changes). Trailing whitespace is safe to strip.
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			continue
		}
		// Git --porcelain=v1 format: "XY PATH" where XY is a 2-char status
		// starting at column 0 and PATH starts at column 3. Use fixed-width
		// extraction instead of strings.Fields so paths with spaces survive.
		if len(line) < 4 {
			continue
		}

		status := strings.TrimSpace(line[:2])
		rest := line[3:]

		var filePath, prevPath string
		// For renames (R status), the format is "R  ORIG_PATH\tNEW_PATH".
		if idx := strings.IndexByte(rest, '\t'); idx >= 0 && strings.HasPrefix(status, "R") {
			prevPath = strings.TrimSpace(rest[:idx])
			filePath = strings.TrimSpace(rest[idx+1:])
		} else {
			filePath = strings.TrimSpace(rest)
		}
		if filePath == "" {
			continue
		}

		additions, deletions := t.getFileStats(ctx, dir, filePath, staged)

		changeType := "modified"
		switch {
		case strings.HasPrefix(status, "A"):
			changeType = "added"
		case strings.HasPrefix(status, "D"):
			changeType = "deleted"
		case strings.HasPrefix(status, "R"):
			changeType = "renamed"
		}

		changes = append(changes, FileChangeInfo{
			FilePath:  filePath,
			Status:    changeType,
			Additions: additions,
			Deletions: deletions,
			PrevPath:  prevPath,
		})
	}

	return changes, nil
}

func (t *GitOverviewTool) getFileStats(ctx context.Context, dir, file string, staged bool) (int, int) {
	var args []string
	if staged {
		args = []string{"diff", "--cached", "--numstat", "--", file}
	} else {
		args = []string{"diff", "--numstat", "--", file}
	}

	output, err := t.runGitCmd(ctx, dir, args...)
	if err != nil || strings.TrimSpace(output) == "" {
		return 0, 0
	}

	parts := strings.Fields(output)
	if len(parts) >= 2 {
		additions, deletions := 0, 0
		fmt.Sscanf(parts[0], "%d", &additions)
		fmt.Sscanf(parts[1], "%d", &deletions)
		return additions, deletions
	}

	return 0, 0
}

func (t *GitOverviewTool) getUntrackedFiles(ctx context.Context, dir string) ([]string, error) {
	output, err := t.runGitCmd(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return []string{}, nil
	}

	var files []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func (t *GitOverviewTool) runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// Ensure GitOverviewTool implements the Tool interface
var _ tools.Tool = (*GitOverviewTool)(nil)
