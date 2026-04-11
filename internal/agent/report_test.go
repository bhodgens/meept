package agent

import (
	"testing"
)

func TestExtractReport(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		wantStatus string
		wantNil    bool
	}{
		{
			name: "completed report",
			response: `I've fixed the bug in user.go by adding a nil check.

` + "```json" + `
{
  "status": "completed",
  "accomplished": ["Fixed null pointer exception", "Added test coverage"],
  "user_decision_needed": false
}
` + "```",
			wantStatus: "completed",
		},
		{
			name: "partial report with routing",
			response: `I've identified the issue but need help implementing.

` + "```json" + `
{
  "status": "partial",
  "accomplished": ["Analyzed codebase", "Found root cause"],
  "not_done": ["Implement fix"],
  "suggested_next_agent": "coder",
  "user_decision_needed": false
}
` + "```",
			wantStatus: "partial",
		},
		{
			name: "needs input",
			response: `I found two possible approaches.

` + "```json" + `
{
  "status": "needs_input",
  "accomplished": ["Analyzed options"],
  "user_decision_needed": true,
  "decision_context": "Choose option A or B"
}
` + "```",
			wantStatus: "needs_input",
		},
		{
			name: "failed report",
			response: `Unable to complete the task.

` + "```json" + `
{
  "status": "failed",
  "accomplished": [],
  "issues": ["API rate limited", "Credentials expired"],
  "user_decision_needed": false
}
` + "```",
			wantStatus: "failed",
		},
		{
			name:     "no report in response",
			response: "Just a regular response without any JSON report.",
			wantNil:  true,
		},
		{
			name: "invalid status",
			response: "```json\n" + `{"status": "unknown", "accomplished": []}` + "\n```",
			wantNil:  true,
		},
		{
			name:       "report without code fence",
			response:   `Done! {"status": "completed", "accomplished": ["task"], "user_decision_needed": false}`,
			wantStatus: "completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := ExtractReport(tt.response)

			if tt.wantNil {
				if report != nil {
					t.Errorf("expected nil, got %+v", report)
				}
				return
			}

			if report == nil {
				t.Fatal("expected report, got nil")
			}

			if report.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", report.Status, tt.wantStatus)
			}
		})
	}
}

func TestExtractReport_Fields(t *testing.T) {
	response := "```json\n" + `{
  "status": "partial",
  "accomplished": ["Task A", "Task B"],
  "not_done": ["Task C"],
  "issues": ["Issue 1"],
  "observations": ["Observation 1", "Observation 2"],
  "suggested_next_agent": "coder",
  "user_decision_needed": true,
  "decision_context": "Need to decide X"
}` + "\n```"

	report := ExtractReport(response)
	if report == nil {
		t.Fatal("expected report, got nil")
	}

	if len(report.Accomplished) != 2 {
		t.Errorf("Accomplished len = %d, want 2", len(report.Accomplished))
	}
	if len(report.NotDone) != 1 {
		t.Errorf("NotDone len = %d, want 1", len(report.NotDone))
	}
	if len(report.Issues) != 1 {
		t.Errorf("Issues len = %d, want 1", len(report.Issues))
	}
	if len(report.Observations) != 2 {
		t.Errorf("Observations len = %d, want 2", len(report.Observations))
	}
	if report.SuggestedNextAgent != "coder" {
		t.Errorf("SuggestedNextAgent = %q, want %q", report.SuggestedNextAgent, "coder")
	}
	if !report.UserDecisionNeeded {
		t.Error("UserDecisionNeeded should be true")
	}
	if report.DecisionContext != "Need to decide X" {
		t.Errorf("DecisionContext = %q", report.DecisionContext)
	}
}

func TestStripReport(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			name: "strip json block",
			response: `I completed the task.

` + "```json" + `
{"status": "completed", "accomplished": ["done"], "user_decision_needed": false}
` + "```",
			want: "I completed the task.",
		},
		{
			name:     "no report to strip",
			response: "Just a regular response.",
			want:     "Just a regular response.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripReport(tt.response)
			if got != tt.want {
				t.Errorf("StripReport() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetermineRouteAction(t *testing.T) {
	tests := []struct {
		name   string
		report *AgentReport
		want   RouteAction
	}{
		{
			name:   "nil report",
			report: nil,
			want:   RouteActionClose,
		},
		{
			name:   "completed no routing",
			report: &AgentReport{Status: "completed"},
			want:   RouteActionClose,
		},
		{
			name: "completed with routing",
			report: &AgentReport{
				Status:             "completed",
				SuggestedNextAgent: "coder",
			},
			want: RouteActionRoute,
		},
		{
			name: "completed but needs user",
			report: &AgentReport{
				Status:             "completed",
				SuggestedNextAgent: "coder",
				UserDecisionNeeded: true,
			},
			want: RouteActionClose,
		},
		{
			name: "partial with routing",
			report: &AgentReport{
				Status:             "partial",
				SuggestedNextAgent: "debugger",
			},
			want: RouteActionRoute,
		},
		{
			name: "partial needs user",
			report: &AgentReport{
				Status:             "partial",
				UserDecisionNeeded: true,
			},
			want: RouteActionNotifyUser,
		},
		{
			name:   "needs_input",
			report: &AgentReport{Status: "needs_input"},
			want:   RouteActionNotifyUser,
		},
		{
			name:   "failed",
			report: &AgentReport{Status: "failed"},
			want:   RouteActionNotifyError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineRouteAction(tt.report)
			if got != tt.want {
				t.Errorf("DetermineRouteAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentReport_Methods(t *testing.T) {
	t.Run("IsComplete", func(t *testing.T) {
		r := &AgentReport{Status: "completed"}
		if !r.IsComplete() {
			t.Error("expected IsComplete() = true")
		}

		r.Status = "partial"
		if r.IsComplete() {
			t.Error("expected IsComplete() = false")
		}
	})

	t.Run("NeedsRouting", func(t *testing.T) {
		r := &AgentReport{SuggestedNextAgent: "coder"}
		if !r.NeedsRouting() {
			t.Error("expected NeedsRouting() = true")
		}

		r.UserDecisionNeeded = true
		if r.NeedsRouting() {
			t.Error("expected NeedsRouting() = false when user decision needed")
		}
	})

	t.Run("HasIssues", func(t *testing.T) {
		r := &AgentReport{}
		if r.HasIssues() {
			t.Error("expected HasIssues() = false")
		}

		r.Issues = []string{"error"}
		if !r.HasIssues() {
			t.Error("expected HasIssues() = true")
		}
	})
}
