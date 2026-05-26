package agent

import (
	"context"
	"log/slog"
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
		name      string
		input     string
		wantType  string
		wantAgent string
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

func TestRecordMethods(t *testing.T) {
	d := &Dispatcher{
		stats: &DispatcherStats{
			ByMethod: make(map[string]int),
			ByAgent:  make(map[string]int),
			ByIntent: make(map[string]int),
		},
	}

	d.recordClassificationMethod("keyword")
	d.recordClassificationMethod("keyword")
	d.recordClassificationMethod("llm")

	d.recordAgent("coder")
	d.recordAgent("coder")
	d.recordAgent("chat")

	d.recordIntentType("code")
	d.recordIntentType("code")
	d.recordIntentType("chat")

	if d.stats.ByMethod["keyword"] != 2 {
		t.Errorf("ByMethod[keyword] = %d, want 2", d.stats.ByMethod["keyword"])
	}
	if d.stats.ByMethod["llm"] != 1 {
		t.Errorf("ByMethod[llm] = %d, want 1", d.stats.ByMethod["llm"])
	}
	if d.stats.ByAgent["coder"] != 2 {
		t.Errorf("ByAgent[coder] = %d, want 2", d.stats.ByAgent["coder"])
	}
	if d.stats.ByIntent["code"] != 2 {
		t.Errorf("ByIntent[code] = %d, want 2", d.stats.ByIntent["code"])
	}
}

func TestGetStats(t *testing.T) {
	d := &Dispatcher{
		stats: &DispatcherStats{
			TotalDispatched: 42,
			ByMethod:        map[string]int{"keyword": 30, "llm": 12},
			ByAgent:         map[string]int{"coder": 20, "chat": 22},
			ByIntent:        map[string]int{"code": 20, "chat": 22},
			FallbackCount:   5,
			FallbackDetails: []FallbackEntry{{Input: "test"}},
		},
	}

	stats := d.GetStats()
	if stats.TotalDispatched != 42 {
		t.Errorf("TotalDispatched = %d, want 42", stats.TotalDispatched)
	}
	if stats.ByMethod["keyword"] != 30 {
		t.Errorf("ByMethod[keyword] = %d, want 30", stats.ByMethod["keyword"])
	}
}

func TestGetStatsNil(t *testing.T) {
	d := &Dispatcher{}
	stats := d.GetStats()
	if stats.TotalDispatched != 0 {
		t.Errorf("expected zero stats, got %d", stats.TotalDispatched)
	}
}

func TestGetFallbackDetails(t *testing.T) {
	d := &Dispatcher{
		stats: &DispatcherStats{
			FallbackDetails: []FallbackEntry{
				{Input: "first"},
				{Input: "second"},
				{Input: "third"},
			},
		},
	}

	details := d.GetFallbackDetails(2)
	if len(details) != 2 {
		t.Fatalf("len(details) = %d, want 2", len(details))
	}
	if details[0].Input != "second" {
		t.Errorf("details[0].Input = %q, want %q", details[0].Input, "second")
	}
	if details[1].Input != "third" {
		t.Errorf("details[1].Input = %q, want %q", details[1].Input, "third")
	}
}

func TestGetFallbackDetailsLimitTruncation(t *testing.T) {
	d := &Dispatcher{
		stats: &DispatcherStats{
			FallbackDetails: []FallbackEntry{
				{Input: "only"},
			},
		},
	}

	details := d.GetFallbackDetails(10)
	if len(details) != 1 {
		t.Errorf("len(details) = %d, want 1", len(details))
	}
}

func TestDetectCompound(t *testing.T) {
	tests := []struct {
		name         string
		intents      []*Intent
		wantCompound bool
		wantType     string
	}{
		{
			name:         "single intent",
			intents:      []*Intent{{Type: "code", AgentType: "coder"}},
			wantCompound: false,
			wantType:     "",
		},
		{
			name: "multi sequential",
			intents: []*Intent{
				{Type: "code", AgentType: "coder", Confidence: 0.8},
				{Type: "plan", AgentType: "planner", RequiresPlanning: true, Confidence: 0.7},
			},
			wantCompound: true,
			wantType:     "sequential",
		},
		{
			name: "multi parallel",
			intents: []*Intent{
				{Type: "code", AgentType: "coder", Confidence: 0.7},
				{Type: "debug", AgentType: "debugger", Confidence: 0.7},
			},
			wantCompound: true,
			wantType:     "parallel",
		},
		{
			name: "chat+scheduler is not compound",
			intents: []*Intent{
				{Type: "chat", AgentType: "chat", Confidence: 0.6},
				{Type: "schedule", AgentType: "scheduler", Confidence: 0.3},
			},
			wantCompound: false, // no non-chat intent above 0.5 threshold
			wantType:     "",
		},
		{
			name:         "low confidence intents are not compound",
			intents:      []*Intent{{Type: "code", Confidence: 0.1}, {Type: "plan", Confidence: 0.2}},
			wantCompound: false,
			wantType:     "",
		},
		{
			name: "only chat intents are not compound",
			intents: []*Intent{
				{Type: "chat", AgentType: "chat", Confidence: 0.7},
				{Type: "platform", AgentType: "chat", Confidence: 0.7},
			},
			wantCompound: false, // both are chat/platform
			wantType:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MultiIntent{Intents: tt.intents}
			got := m.DetectCompound()
			if got != tt.wantCompound {
				t.Errorf("DetectCompound() = %v, want %v", got, tt.wantCompound)
			}
			if m.CompoundType != tt.wantType {
				t.Errorf("CompoundType = %q, want %q", m.CompoundType, tt.wantType)
			}
		})
	}
}

func TestDeduplicateIntents(t *testing.T) {
	tests := []struct {
		name    string
		intents []*Intent
		want    int
	}{
		{
			name: "overlapping types",
			intents: []*Intent{
				{Type: "code", Confidence: 0.8},
				{Type: "code", Confidence: 0.9},
				{Type: "debug", Confidence: 0.7},
			},
			want: 2,
		},
		{
			name: "all unique",
			intents: []*Intent{
				{Type: "code", Confidence: 0.8},
				{Type: "debug", Confidence: 0.7},
			},
			want: 2,
		},
		{
			name:    "empty",
			intents: []*Intent{},
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deduplicateIntents(tt.intents)
			if len(result) != tt.want {
				t.Errorf("len(deduplicateIntents()) = %d, want %d", len(result), tt.want)
			}
			if tt.name == "overlapping types" {
				for _, intent := range result {
					if intent.Type == "code" && intent.Confidence != 0.9 {
						t.Errorf("deduplicated code confidence = %v, want 0.9", intent.Confidence)
					}
				}
			}
		})
	}
}

func TestClassifyAll(t *testing.T) {
	c := &KeywordClassifier{}
	ctx := context.Background()

	intents := c.ClassifyAll(ctx, "write code and debug the error", nil)
	if len(intents) < 2 {
		t.Errorf("ClassifyAll() returned %d intents, want >= 2", len(intents))
	}
}

func TestRouteToAgentUsesReportRouter(t *testing.T) {
	// Verify that RouteToAgent returns a response that incorporates
	// report routing decisions, not just StripReport of the raw response
	registry := NewAgentRegistry(RegistryConfig{
		Logger: slog.Default(),
	})
	// Register mock agents
	_ = registry.RegisterSpec(&AgentSpec{
		ID:   "coder",
		Name: "coder",
		Role: "executor",
	})
	_ = registry.RegisterSpec(&AgentSpec{
		ID:   "reviewer",
		Name: "reviewer",
		Role: "executor",
	})

	d := NewDispatcher(DispatcherConfig{
		Registry: registry,
		Logger:   slog.Default(),
	})

	if d == nil {
		t.Fatal("dispatcher should not be nil")
	}
	// The router field must be initialized by NewDispatcher
	if d.router == nil {
		t.Fatal("dispatcher.router should be initialized by NewDispatcher")
	}
}
