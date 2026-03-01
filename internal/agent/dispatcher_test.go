package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestShouldDispatchAsync(t *testing.T) {
	d := &Dispatcher{}

	tests := []struct {
		name   string
		result *DispatchResult
		want   bool
	}{
		{
			name:   "nil result",
			result: nil,
			want:   false,
		},
		{
			name:   "nil intent",
			result: &DispatchResult{},
			want:   false,
		},
		{
			name: "skill response (always sync)",
			result: &DispatchResult{
				Intent:   &Intent{Type: "code"},
				Response: "skill handled this",
			},
			want: false,
		},
		{
			name: "code intent",
			result: &DispatchResult{
				Intent: &Intent{Type: "code"},
			},
			want: true,
		},
		{
			name: "debug intent",
			result: &DispatchResult{
				Intent: &Intent{Type: "debug"},
			},
			want: true,
		},
		{
			name: "plan intent",
			result: &DispatchResult{
				Intent: &Intent{Type: "plan"},
			},
			want: true,
		},
		{
			name: "schedule intent (simple, sync)",
			result: &DispatchResult{
				Intent: &Intent{Type: "schedule"},
			},
			want: false, // Simple schedule intents are sync
		},
		{
			name: "schedule intent (complex, async)",
			result: &DispatchResult{
				Intent: &Intent{Type: "schedule", RequiresPlanning: true},
			},
			want: true, // Complex schedule intents that require planning are async
		},
		{
			name: "chat intent (sync)",
			result: &DispatchResult{
				Intent: &Intent{Type: "chat"},
			},
			want: false,
		},
		{
			name: "report intent (sync)",
			result: &DispatchResult{
				Intent: &Intent{Type: "report"},
			},
			want: false,
		},
		{
			name: "recall intent (sync)",
			result: &DispatchResult{
				Intent: &Intent{Type: "recall"},
			},
			want: false,
		},
		{
			name: "analyze intent (sync)",
			result: &DispatchResult{
				Intent: &Intent{Type: "analyze"},
			},
			want: false,
		},
		{
			name: "search intent (sync)",
			result: &DispatchResult{
				Intent: &Intent{Type: "search"},
			},
			want: false,
		},
		{
			name: "git intent (async)",
			result: &DispatchResult{
				Intent: &Intent{Type: "git"},
			},
			want: true,
		},
		{
			name: "requires planning flag",
			result: &DispatchResult{
				Intent: &Intent{Type: "unknown", RequiresPlanning: true},
			},
			want: true,
		},
		{
			name: "code intent with task",
			result: &DispatchResult{
				Intent: &Intent{Type: "code"},
				Task:   task.NewTask("test", "test"),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.ShouldDispatchAsync(tt.result)
			if got != tt.want {
				t.Errorf("ShouldDispatchAsync() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKeywordClassifier_ReportIntents(t *testing.T) {
	c := &KeywordClassifier{}
	ctx := context.Background()

	tests := []struct {
		name       string
		input      string
		wantType   string
		wantAgent  string
	}{
		{
			name:      "give me a report",
			input:     "give me a report on what you did",
			wantType:  "report",
			wantAgent: "chat",
		},
		{
			name:      "what did you do",
			input:     "what did you do today?",
			wantType:  "report",
			wantAgent: "chat",
		},
		{
			name:      "status report",
			input:     "status report please",
			wantType:  "report",
			wantAgent: "chat",
		},
		{
			name:      "what have you done",
			input:     "what have you done so far?",
			wantType:  "report",
			wantAgent: "chat",
		},
		{
			name:      "recall memory",
			input:     "do you remember what we talked about?",
			wantType:  "recall",
			wantAgent: "chat",
		},
		{
			name:      "summarize document (analyze, not report)",
			input:     "summarize this code file",
			wantType:  "analyze",
			wantAgent: "analyst",
		},
		{
			name:      "summarize what you did (report)",
			input:     "summarize what you worked on",
			wantType:  "report",
			wantAgent: "chat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := c.Classify(ctx, tt.input, nil)
			if err != nil {
				t.Fatalf("Classify() error = %v", err)
			}
			if intent == nil {
				t.Fatal("Classify() returned nil intent")
			}
			if intent.Type != tt.wantType {
				t.Errorf("Classify(%q).Type = %q, want %q", tt.input, intent.Type, tt.wantType)
			}
			if intent.AgentType != tt.wantAgent {
				t.Errorf("Classify(%q).AgentType = %q, want %q", tt.input, intent.AgentType, tt.wantAgent)
			}
		})
	}
}

func TestShouldCreateTask(t *testing.T) {
	d := &Dispatcher{}

	tests := []struct {
		name   string
		intent *Intent
		want   bool
	}{
		{name: "chat", intent: &Intent{Type: "chat"}, want: false},
		{name: "report", intent: &Intent{Type: "report"}, want: false},
		{name: "recall", intent: &Intent{Type: "recall"}, want: false},
		{name: "platform", intent: &Intent{Type: "platform"}, want: false},
		{name: "code", intent: &Intent{Type: "code"}, want: true},
		{name: "debug", intent: &Intent{Type: "debug"}, want: true},
		{name: "plan", intent: &Intent{Type: "plan"}, want: true},
		{name: "schedule", intent: &Intent{Type: "schedule"}, want: true},
		{name: "git", intent: &Intent{Type: "git"}, want: true},
		{name: "analyze (no planning)", intent: &Intent{Type: "analyze"}, want: false},
		{name: "unknown with planning", intent: &Intent{Type: "unknown", RequiresPlanning: true}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.shouldCreateTask(tt.intent)
			if got != tt.want {
				t.Errorf("shouldCreateTask(%q) = %v, want %v", tt.intent.Type, got, tt.want)
			}
		})
	}
}

func TestShouldDecompose(t *testing.T) {
	sp := &StrategicPlanner{}

	tests := []struct {
		name string
		req  PlanRequest
		want bool
	}{
		{
			name: "chat intent never decomposes",
			req:  PlanRequest{Intent: "chat", Input: "hello there"},
			want: false,
		},
		{
			name: "report intent never decomposes",
			req:  PlanRequest{Intent: "report", Input: "give me a report"},
			want: false,
		},
		{
			name: "recall intent never decomposes",
			req:  PlanRequest{Intent: "recall", Input: "what do you remember"},
			want: false,
		},
		{
			name: "search intent never decomposes",
			req:  PlanRequest{Intent: "search", Input: "find something for me"},
			want: false,
		},
		{
			name: "short code request without complexity",
			req:  PlanRequest{Intent: "code", Input: "fix the login bug"},
			want: false,
		},
		{
			name: "short code request with complexity indicator",
			req:  PlanRequest{Intent: "code", Input: "fix the login bug and then update the tests"},
			want: true,
		},
		{
			name: "long code request decomposes",
			req: PlanRequest{
				Intent: "code",
				Input:  "I need you to refactor the authentication module to use JWT tokens instead of session cookies. This involves updating the login handler, creating a token generation service, modifying the middleware to validate tokens, and updating all API endpoints that currently check session state.",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sp.shouldDecompose(tt.req)
			if got != tt.want {
				t.Errorf("shouldDecompose() = %v, want %v", got, tt.want)
			}
		})
	}
}
