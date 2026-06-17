package builtin

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// GitValidateTool validates commit messages and git state.
type GitValidateTool struct{}

// NewGitValidateTool creates a new git validate tool.
func NewGitValidateTool() *GitValidateTool {
	return &GitValidateTool{}
}

func (t *GitValidateTool) Name() string { return "git_validate" }

func (t *GitValidateTool) Category() string { return "git" }

func (t *GitValidateTool) Description() string {
	return "Validate commit messages against conventional commit format and check git state before committing. Returns validation results with suggestions for improvement."
}

func (t *GitValidateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"message": {
				Type:        schemaTypeString,
				Description: "Commit message to validate.",
			},
			"messages": {
				Type:        schemaTypeArray,
				Description: "Multiple commit messages to validate (batch validation).",
				Items: &llm.ParameterProperty{
					Type:        schemaTypeString,
					Description: "Commit message to validate",
				},
			},
			"check_state": {
				Type:        schemaTypeBoolean,
				Description: "Check git state (staged changes, branch status) before committing (default: true).",
			},
			"working_dir": {
				Type:        schemaTypeString,
				Description: "Working directory for git commands (optional, defaults to current dir).",
			},
		},
	}
}

// GitValidateResult contains validation results.
type GitValidateResult struct {
	Valid       bool               `json:"valid"`
	Message     string             `json:"message"`
	Results     []ValidationResult `json:"results,omitempty"`
	GitState    *GitStateInfo      `json:"git_state,omitempty"`
	Suggestions []string           `json:"suggestions,omitempty"`
}

// ValidationResult describes validation of a single message.
type ValidationResult struct {
	Message     string   `json:"message"`
	Valid       bool     `json:"valid"`
	MessageType string   `json:"type,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	HasBreaking bool     `json:"has_breaking,omitempty"`
	Description string   `json:"description,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

// GitStateInfo describes current git state.
type GitStateInfo struct {
	Branch          string `json:"branch"`
	StagedChanges   int    `json:"staged_changes"`
	UnstagedChanges int    `json:"unstaged_changes"`
	UntrackedFiles  int    `json:"untracked_files"`
	Ahead           int    `json:"ahead,omitempty"`
	Behind          int    `json:"behind,omitempty"`
	ReadyToCommit   bool   `json:"ready_to_commit"`
}

var validCommitTypes = map[string]bool{
	"feat": true, "fix": true, "docs": true, "style": true,
	"refactor": true, "perf": true, "test": true, "build": true,
	"ci": true, "chore": true, "revert": true,
}

var validateCommitRegex = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\(([a-zA-Z0-9-]+)\))?!?:\s+(.+)$`)

func (t *GitValidateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	result := GitValidateResult{
		Results:     make([]ValidationResult, 0),
		Suggestions: make([]string, 0),
		Valid:       true,
	}

	checkState := true
	if v, ok := args["check_state"].(bool); ok {
		checkState = v
	}

	workingDir, _ := args["working_dir"].(string)

	if checkState {
		state, err := t.getGitState(ctx, workingDir)
		if err != nil {
			result.Suggestions = append(result.Suggestions, fmt.Sprintf("Warning: Could not check git state: %v", err))
		} else {
			result.GitState = state
			if !state.ReadyToCommit {
				result.Suggestions = append(result.Suggestions, "No staged changes. Use git add to stage files before committing.")
			}
		}
	}

	message, hasMessage := args["message"].(string)
	if hasMessage && message != "" {
		vr := t.validateMessage(message)
		result.Results = append(result.Results, vr)
		if !vr.Valid {
			result.Valid = false
		}
		result.Message = fmt.Sprintf("Message validation: %s", statusText(vr.Valid))
		return result, nil
	}

	messagesRaw, hasMessages := args["messages"].([]any)
	if hasMessages && len(messagesRaw) > 0 {
		allValid := true
		for _, msgRaw := range messagesRaw {
			msg, ok := msgRaw.(string)
			if !ok {
				continue
			}
			vr := t.validateMessage(msg)
			result.Results = append(result.Results, vr)
			if !vr.Valid {
				allValid = false
			}
		}
		result.Valid = allValid
		result.Message = fmt.Sprintf("Batch validation: %d/%d valid", countValid(result.Results), len(result.Results))
		return result, nil
	}

	result.Message = "No commit messages provided for validation"
	result.Valid = true
	return result, nil
}

func (t *GitValidateTool) validateMessage(message string) ValidationResult {
	vr := ValidationResult{
		Message: message,
		Valid:   true,
		Errors:  make([]string, 0),
	}

	matches := validateCommitRegex.FindStringSubmatch(message)
	if matches == nil {
		vr.Valid = false
		vr.Errors = append(vr.Errors,
			"Message does not follow conventional commit format",
			"Expected: type(scope)!: description",
			"Example: fix(api): resolve null pointer in user endpoint",
		)
		return vr
	}

	vr.MessageType = matches[1]
	if matches[3] != "" {
		vr.Scope = matches[3]
	}
	vr.HasBreaking = strings.Contains(message, "!:")
	vr.Description = matches[len(matches)-1]

	if !validCommitTypes[vr.MessageType] {
		vr.Valid = false
		vr.Errors = append(vr.Errors, fmt.Sprintf("Invalid commit type '%s'. Valid types: %v", vr.MessageType, getValidTypes()))
	}

	if len(vr.Description) < 5 {
		vr.Valid = false
		vr.Errors = append(vr.Errors, "Description too short (minimum 5 characters)")
	}

	if strings.HasSuffix(vr.Description, ".") {
		vr.Errors = append(vr.Errors, "Description should not end with a period")
	}

	if len(vr.Description) > 0 && strings.ToUpper(string(vr.Description[0])) == string(vr.Description[0]) {
		vr.Errors = append(vr.Errors, "Description should start with lowercase letter")
	}

	return vr
}

func (t *GitValidateTool) getGitState(ctx context.Context, dir string) (*GitStateInfo, error) {
	state := &GitStateInfo{}

	branch, err := t.runGitCmd(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to get branch: %w", err)
	}
	state.Branch = strings.TrimSpace(branch)

	staged, _ := t.runGitCmd(ctx, dir, "diff", "--cached", "--name-only")
	state.StagedChanges = countLines(staged)

	unstaged, _ := t.runGitCmd(ctx, dir, "diff", "--name-only")
	state.UnstagedChanges = countLines(unstaged)

	untracked, _ := t.runGitCmd(ctx, dir, "ls-files", "--others", "--exclude-standard")
	state.UntrackedFiles = countLines(untracked)

	upstream, err := t.runGitCmd(ctx, dir, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err == nil {
		counts, _ := t.runGitCmd(ctx, dir, "rev-list", "--left-right", "--count", fmt.Sprintf("HEAD...%s", strings.TrimSpace(upstream)))
		parts := strings.Fields(strings.TrimSpace(counts))
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &state.Ahead)
			fmt.Sscanf(parts[1], "%d", &state.Behind)
		}
	}

	state.ReadyToCommit = state.StagedChanges > 0
	return state, nil
}

func (t *GitValidateTool) runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

func statusText(valid bool) string {
	if valid {
		return "valid"
	}
	return "invalid"
}

func countValid(results []ValidationResult) int {
	count := 0
	for _, r := range results {
		if r.Valid {
			count++
		}
	}
	return count
}

func getValidTypes() []string {
	types := make([]string, 0, len(validCommitTypes))
	for t := range validCommitTypes {
		types = append(types, t)
	}
	return types
}

func countLines(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// Ensure GitValidateTool implements the Tool interface
var _ tools.Tool = (*GitValidateTool)(nil)
