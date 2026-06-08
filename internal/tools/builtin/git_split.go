package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// GitSplitTool suggests atomic commit groups from working tree changes.
type GitSplitTool struct {
	workingDir string
}

// NewGitSplitTool creates a new git split tool.
func NewGitSplitTool(workingDir string) *GitSplitTool {
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	return &GitSplitTool{workingDir: workingDir}
}

func (t *GitSplitTool) Name() string { return "git_split" }

func (t *GitSplitTool) Category() string { return "git" }

func (t *GitSplitTool) Description() string {
	return "Analyze working tree changes and suggest logical atomic commit groups. Groups changes by dependency order: source code first, then tests, then documentation, then configuration. Returns grouped suggestions with commit message hints for each group."
}

func (t *GitSplitTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"working_dir": {
				Type:        schemaTypeString,
				Description: "Working directory for git command (optional, defaults to current dir).",
			},
			"group_by": {
				Type:        schemaTypeString,
				Description: "Grouping strategy: 'dependency' (default), 'feature', 'file_type'.",
				Enum:        []string{"dependency", "feature", "file_type"},
			},
		},
	}
}

// GitSplitResult contains suggested commit groups.
type GitSplitResult struct {
	Groups       []CommitGroup `json:"groups"`
	TotalChanges int           `json:"total_changes"`
	Strategy     string        `json:"strategy"`
}

// CommitGroup represents a suggested atomic commit.
type CommitGroup struct {
	Order       int      `json:"order"`
	Name        string   `json:"name"`
	Files       []string `json:"files"`
	Priority    int      `json:"priority"`
	MessageHint string   `json:"message_hint,omitempty"`
	Rationale   string   `json:"rationale"`
}

func (t *GitSplitTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	workingDir := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		workingDir = wd
	}

	groupBy, ok := args["group_by"].(string)
	if !ok || groupBy == "" {
		groupBy = "dependency"
	}

	changedFiles, err := t.getAllChangedFiles(ctx, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get changed files: %w", err)
	}

	if len(changedFiles) == 0 {
		return GitSplitResult{
			Groups:       []CommitGroup{},
			TotalChanges: 0,
			Strategy:     groupBy,
		}, nil
	}

	var groups []CommitGroup
	switch groupBy {
	case "dependency":
		groups = t.groupByDependency(changedFiles)
	case "feature":
		groups = t.groupByFeature(changedFiles)
	case "file_type":
		groups = t.groupByFileType(changedFiles)
	default:
		groups = t.groupByDependency(changedFiles)
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Priority < groups[j].Priority
	})

	for i := range groups {
		groups[i].Order = i + 1
	}

	return GitSplitResult{
		Groups:       groups,
		TotalChanges: len(changedFiles),
		Strategy:     groupBy,
	}, nil
}

func (t *GitSplitTool) getAllChangedFiles(ctx context.Context, dir string) ([]FileChangeInfo, error) {
	var allChanges []FileChangeInfo

	stagedOutput, err := t.runGitCmd(ctx, dir, "diff", "--cached", "--name-status")
	if err == nil && strings.TrimSpace(stagedOutput) != "" {
		changes := t.parseSimpleStatus(stagedOutput)
		allChanges = append(allChanges, changes...)
	}

	unstagedOutput, err := t.runGitCmd(ctx, dir, "diff", "--name-status")
	if err == nil && strings.TrimSpace(unstagedOutput) != "" {
		changes := t.parseSimpleStatus(unstagedOutput)
		allChanges = append(allChanges, changes...)
	}

	return allChanges, nil
}

func (t *GitSplitTool) parseSimpleStatus(output string) []FileChangeInfo {
	var changes []FileChangeInfo
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		filePath := parts[1]

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
			FilePath: filePath,
			Status:   changeType,
		})
	}
	return changes
}

func (t *GitSplitTool) groupByDependency(files []FileChangeInfo) []CommitGroup {
	categories := map[string]*struct {
		files     []string
		priority  int
		name      string
		rationale string
	}{
		"source": {
			files:     []string{},
			priority:  1,
			name:      "Source code changes",
			rationale: "Source code should be committed first to maintain buildable state",
		},
		"test": {
			files:     []string{},
			priority:  2,
			name:      "Test changes",
			rationale: "Tests depend on source code, commit after implementation",
		},
		"docs": {
			files:     []string{},
			priority:  3,
			name:      "Documentation updates",
			rationale: "Documentation can be committed separately from code",
		},
		"config": {
			files:     []string{},
			priority:  4,
			name:      "Configuration changes",
			rationale: "Config changes are independent and should be atomic",
		},
		"other": {
			files:     []string{},
			priority:  5,
			name:      "Other changes",
			rationale: "Miscellaneous changes grouped together",
		},
	}

	for _, file := range files {
		category := t.categorizeFile(file.FilePath)
		categories[category].files = append(categories[category].files, file.FilePath)
	}

	var groups []CommitGroup
	for _, cat := range categories {
		if len(cat.files) > 0 {
			groups = append(groups, CommitGroup{
				Name:      cat.name,
				Files:     cat.files,
				Priority:  cat.priority,
				Rationale: cat.rationale,
			})
		}
	}

	return groups
}

func (t *GitSplitTool) groupByFeature(files []FileChangeInfo) []CommitGroup {
	featureGroups := make(map[string][]string)

	for _, file := range files {
		parts := strings.Split(file.FilePath, "/")
		feature := "root"
		if len(parts) > 1 {
			feature = parts[0]
		}
		featureGroups[feature] = append(featureGroups[feature], file.FilePath)
	}

	var groups []CommitGroup
	priority := 1
	for feature, featureFiles := range featureGroups {
		groups = append(groups, CommitGroup{
			Name:      fmt.Sprintf("%s/ changes", feature),
			Files:     featureFiles,
			Priority:  priority,
			Rationale: fmt.Sprintf("Files in %s/ directory grouped as logical unit", feature),
		})
		priority++
	}

	return groups
}

func (t *GitSplitTool) groupByFileType(files []FileChangeInfo) []CommitGroup {
	typeGroups := make(map[string][]string)

	for _, file := range files {
		ext := getFileExtension(file.FilePath)
		if ext == "" {
			ext = "no-extension"
		}
		typeGroups[ext] = append(typeGroups[ext], file.FilePath)
	}

	var groups []CommitGroup
	priority := 1
	for ext, extFiles := range typeGroups {
		groups = append(groups, CommitGroup{
			Name:      fmt.Sprintf("%s files", ext),
			Files:     extFiles,
			Priority:  priority,
			Rationale: fmt.Sprintf("All %s files grouped by file type", ext),
		})
		priority++
	}

	return groups
}

func (t *GitSplitTool) categorizeFile(filePath string) string {
	sourceExts := []string{".go", ".py", ".js", ".ts", ".rs", ".c", ".cpp", ".h", ".java", ".rb", ".swift", ".kt", ".scala"}

	if strings.Contains(filePath, "_test.") ||
		strings.Contains(filePath, ".test.") ||
		strings.Contains(filePath, "/test/") ||
		strings.Contains(filePath, "/tests/") ||
		strings.Contains(filePath, "__test__") ||
		strings.Contains(filePath, "__tests__") {
		return "test"
	}

	if strings.HasSuffix(filePath, ".md") ||
		strings.HasSuffix(filePath, ".rst") ||
		strings.Contains(filePath, "/doc/") ||
		strings.Contains(filePath, "/docs/") {
		return "docs"
	}

	configPatterns := []string{".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".config", "Dockerfile", ".env"}
	for _, pattern := range configPatterns {
		if strings.HasSuffix(filePath, pattern) || strings.Contains(filePath, pattern) {
			return "config"
		}
	}

	for _, ext := range sourceExts {
		if strings.HasSuffix(filePath, ext) {
			return "source"
		}
	}

	return "other"
}

func getFileExtension(filePath string) string {
	ext := ""
	parts := strings.Split(filePath, ".")
	if len(parts) > 1 {
		ext = "." + parts[len(parts)-1]
	}
	return ext
}

func (t *GitSplitTool) runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// Ensure GitSplitTool implements the Tool interface
var _ tools.Tool = (*GitSplitTool)(nil)
