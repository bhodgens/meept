package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/plan"
)

// StepHandoff is the structured handoff document produced after a step
// completes. Replaces the legacy 500-char truncation in
// propagateContextToNextStepsLegacy.
type StepHandoff struct {
	StepID          string          `json:"step_id"`
	StepDescription string          `json:"step_description"`
	Summary         string          `json:"summary"`
	FilesModified   []FileChange    `json:"files_modified"`
	Decisions       []Decision      `json:"decisions"`
	Artifacts       []plan.Artifact `json:"artifacts"`
	FollowUpHints   []string        `json:"follow_up_hints"`
	ToolHighlights  []ToolHighlight `json:"tool_highlights"`
	ErrorCode       string          `json:"error_code,omitempty"`
}

// FileChange describes a single file mutation declared by a step.
type FileChange struct {
	Path    string `json:"path"`
	Change  string `json:"change"` // created|modified|deleted
	Summary string `json:"summary"`
}

// Decision captures a notable design or tooling choice the step made.
type Decision struct {
	Name      string `json:"name"`
	Rationale string `json:"rationale"`
}

// ToolHighlight summarizes a notable tool invocation from the step.
type ToolHighlight struct {
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
}

// Failed returns true if the step reported an error.
func (h *StepHandoff) Failed() bool {
	return h.ErrorCode != ""
}

// Truncate enforces per-field length limits to keep the handoff compact.
// Paths are preserved full length; summaries capped at 200 chars; descriptions at 300.
func (h *StepHandoff) Truncate() {
	h.StepDescription = truncStr(h.StepDescription, 300)
	h.Summary = truncStr(h.Summary, 200)
	for i := range h.FilesModified {
		h.FilesModified[i].Summary = truncStr(h.FilesModified[i].Summary, 200)
	}
	for i := range h.Decisions {
		h.Decisions[i].Rationale = truncStr(h.Decisions[i].Rationale, 300)
	}
	for i := range h.Artifacts {
		h.Artifacts[i].Description = truncStr(h.Artifacts[i].Description, 300)
	}
	for i := range h.ToolHighlights {
		h.ToolHighlights[i].Summary = truncStr(h.ToolHighlights[i].Summary, 200)
	}
	for i := range h.FollowUpHints {
		h.FollowUpHints[i] = truncStr(h.FollowUpHints[i], 200)
	}
	if len(h.FilesModified) > 10 {
		h.FilesModified = h.FilesModified[:10]
	}
	if len(h.Decisions) > 5 {
		h.Decisions = h.Decisions[:5]
	}
	if len(h.Artifacts) > 5 {
		h.Artifacts = h.Artifacts[:5]
	}
	if len(h.FollowUpHints) > 5 {
		h.FollowUpHints = h.FollowUpHints[:5]
	}
	if len(h.ToolHighlights) > 10 {
		h.ToolHighlights = h.ToolHighlights[:10]
	}
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return "..."
	}
	// Use rune-aware slicing to avoid splitting multi-byte UTF-8 sequences.
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}

// RenderMarkdown renders the handoff as markdown for injection into
// dependent steps' AccumulatedContext.
func (h *StepHandoff) RenderMarkdown() string {
	if h.Failed() {
		return fmt.Sprintf("## Prior step FAILED: %s\n**Error:** %s\n", h.StepDescription, h.ErrorCode)
	}
	var sb strings.Builder
	sb.WriteString("## Prior step completed: " + h.StepDescription + "\n\n")
	sb.WriteString(h.Summary + "\n\n")
	if len(h.FilesModified) > 0 {
		sb.WriteString("**Files:**\n")
		for _, f := range h.FilesModified {
			sb.WriteString(fmt.Sprintf("- `%s` (%s): %s\n", f.Path, f.Change, f.Summary))
		}
		sb.WriteString("\n")
	}
	if len(h.Decisions) > 0 {
		sb.WriteString("**Decisions:**\n")
		for _, d := range h.Decisions {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", d.Name, d.Rationale))
		}
		sb.WriteString("\n")
	}
	if len(h.Artifacts) > 0 {
		sb.WriteString("**Artifacts:**\n")
		for _, a := range h.Artifacts {
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", a.Name, a.Kind, a.Description))
		}
		sb.WriteString("\n")
	}
	if len(h.FollowUpHints) > 0 {
		sb.WriteString("**Watch out:**\n")
		for _, hint := range h.FollowUpHints {
			sb.WriteString("- " + hint + "\n")
		}
	}
	return sb.String()
}

// parseHandoffJSON parses LLM output into a StepHandoff.
func parseHandoffJSON(raw string) (*StepHandoff, error) {
	jsonStr := ExtractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON in handoff output")
	}
	var h StepHandoff
	if err := json.Unmarshal([]byte(jsonStr), &h); err != nil {
		return nil, fmt.Errorf("parse handoff JSON: %w", err)
	}
	h.Truncate()
	return &h, nil
}
