package agent

import (
	"fmt"
	"strings"
)

// BuildRevisionContext constructs the AccumulatedContext string for a revision
// step. It combines the reviewer's feedback and issue list with the original
// spec's acceptance criteria, giving the coder agent full context of what went
// wrong and what "done" looks like.
func BuildRevisionContext(result *ReviewResult, spec *TaskSpec) string {
	if result == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("PREVIOUS REVIEW FEEDBACK (address these issues):\n\n")
	if result.Feedback != "" {
		sb.WriteString(result.Feedback)
		sb.WriteString("\n\n")
	}
	if len(result.Issues) > 0 {
		sb.WriteString("Specific issues to fix:\n")
		for i, issue := range result.Issues {
			fmt.Fprintf(&sb, "  %d. %s\n", i+1, issue)
		}
		sb.WriteString("\n")
	}

	if spec != nil && len(spec.Criteria) > 0 {
		sb.WriteString("ORIGINAL ACCEPTANCE CRITERIA (these must still be met):\n\n")
		for _, c := range spec.Criteria {
			fmt.Fprintf(&sb, "- %s\n", c.AcceptanceCriteria)
		}
	}

	return sb.String()
}
