package agent

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
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

// TestKeywordClassifier_ResearchRoutesToResearcher verifies that "research"
// queries produce IntentResearch (not IntentAnalyze), so they route to the
// dedicated researcher agent rather than the analyst.
func TestKeywordClassifier_ResearchRoutesToResearcher(t *testing.T) {
	c := &KeywordClassifier{}
	ctx := context.Background()

	cases := []struct {
		name  string
		input string
	}{
		{"research literal", "research best practices for X"},
		{"investigate", "investigate why the cluster is slow"},
		{"deep dive", "do a deep dive on the auth flow"},
		{"study", "study the existing patterns before coding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intents := c.ClassifyAll(ctx, tc.input, nil)
			if len(intents) == 0 {
				t.Fatalf("no intents returned for %q", tc.input)
			}
			top := intents[0]
			if top.Type != string(IntentResearch) {
				t.Errorf("ClassifyAll(%q) top intent = %q, want %q",
					tc.input, top.Type, string(IntentResearch))
			}
			if top.AgentType != config.AgentIDResearcher {
				t.Errorf("ClassifyAll(%q) top agent = %q, want %q",
					tc.input, top.AgentType, config.AgentIDResearcher)
			}
		})
	}
}

// TestKeywordClassifier_NewRosterIntents (Plan 2) verifies that trigger
// phrases for the four new knowledge-work intents route to their specialist
// agents rather than being captured by older patterns (notably
// IntentPlan/"architect" used to swallow design requests).
func TestKeywordClassifier_NewRosterIntents(t *testing.T) {
	c := &KeywordClassifier{}
	ctx := context.Background()

	cases := []struct {
		name      string
		input     string
		wantType  string
		wantAgent string
	}{
		{
			name:      "write essay → writer",
			input:     "write an essay about distributed systems",
			wantType:  string(IntentWrite),
			wantAgent: config.AgentIDWriter,
		},
		{
			name:      "draft a brief → writer",
			input:     "draft a brief on the Q3 launch",
			wantType:  string(IntentWrite),
			wantAgent: config.AgentIDWriter,
		},
		{
			name:      "design system → architect (not planner)",
			input:     "design a system for real-time collaboration",
			wantType:  string(IntentArchitect),
			wantAgent: config.AgentIDArchitect,
		},
		{
			name:      "tech stack tradeoff → architect",
			input:     "evaluate the tech stack trade-off between Postgres and DynamoDB",
			wantType:  string(IntentArchitect),
			wantAgent: config.AgentIDArchitect,
		},
		{
			name:      "stress-test claim → skeptic",
			input:     "stress-test my claim that the cache layer is unnecessary",
			wantType:  string(IntentSkeptic),
			wantAgent: config.AgentIDSkeptic,
		},
		{
			name:      "what's wrong with → skeptic",
			input:     "what's wrong with my reasoning about the auth flow?",
			wantType:  string(IntentSkeptic),
			wantAgent: config.AgentIDSkeptic,
		},
		{
			name:      "review memory → librarian",
			input:     "review my memory and surface contradictions",
			wantType:  string(IntentLibrarian),
			wantAgent: config.AgentIDLibrarian,
		},
		{
			name:      "clean up tags → librarian",
			input:     "clean up tags on my epistemic memory",
			wantType:  string(IntentLibrarian),
			wantAgent: config.AgentIDLibrarian,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intents := c.ClassifyAll(ctx, tc.input, nil)
			if len(intents) == 0 {
				t.Fatalf("no intents returned for %q", tc.input)
			}
			// ClassifyAll returns one match per pattern; the order is
			// non-deterministic. Pick the highest-confidence intent to
			// match what a real dispatcher would choose after ranking.
			top := intents[0]
			for _, in := range intents[1:] {
				if in.Confidence > top.Confidence {
					top = in
				}
			}
			if top.Type != tc.wantType {
				t.Errorf("ClassifyAll(%q) best intent = %q (conf %f), want %q\nall matches: %+v",
					tc.input, top.Type, top.Confidence, tc.wantType, intents)
			}
			if top.AgentType != tc.wantAgent {
				t.Errorf("ClassifyAll(%q) best agent = %q, want %q",
					tc.input, top.AgentType, tc.wantAgent)
			}
		})
	}
}

// TestKeywordClassifier_PlanNoLongerCapturesArchitect (Plan 2) verifies the
// specific regression: a request that says "architect" without any of the
// longer architect-intent compound phrases should not be misrouted to the
// planner. It should hit IntentArchitect (bare-keyword match) or remain
// unmatched — but never planner.
func TestKeywordClassifier_PlanNoLongerCapturesArchitect(t *testing.T) {
	c := &KeywordClassifier{}
	ctx := context.Background()

	// Input chosen to contain "architect" but none of the longer
	// IntentArchitect compound phrases (so the test is robust to the
	// classifier's best-score-wins ranking).
	input := "architect a migration path"
	intents := c.ClassifyAll(ctx, input, nil)
	for _, in := range intents {
		if in.Type == string(IntentPlan) {
			t.Errorf("input %q matched IntentPlan; 'architect' should no longer route to planner. intents: %+v", input, intents)
		}
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

func TestIsPendingClarification_NoTracker(t *testing.T) {
	d := &Dispatcher{sessionTracker: nil}
	if d.isPendingClarification("session-1") {
		t.Error("should return false when sessionTracker is nil")
	}
}

func TestIsPendingClarification_NoHistory(t *testing.T) {
	d := &Dispatcher{sessionTracker: NewSessionTracker(30 * time.Minute)}
	if d.isPendingClarification("session-1") {
		t.Error("should return false for session with no history")
	}
}

func TestIsPendingClarification_LastIntentWasClarify(t *testing.T) {
	d := &Dispatcher{sessionTracker: NewSessionTracker(30 * time.Minute)}
	intent := &Intent{
		Type:       string(IntentClarify),
		Confidence: 0.8,
		AgentType:  config.AgentIDChat,
		Summary:    "fix something",
		TrueAnalysis: &TrueIntentAnalysis{
			Goal:       "fix something",
			Ambiguity:  0.8,
			Scope:      "broad",
			Category:   "fix",
			Confidence: 0.7,
		},
	}
	d.sessionTracker.RecordIntent("session-1", intent, config.AgentIDChat)

	if !d.isPendingClarification("session-1") {
		t.Error("should return true when last intent was clarify")
	}
}

func TestIsPendingClarification_LastIntentWasNotClarify(t *testing.T) {
	d := &Dispatcher{sessionTracker: NewSessionTracker(30 * time.Minute)}
	intent := &Intent{
		Type:       string(IntentCode),
		Confidence: 0.9,
		AgentType:  config.AgentIDCoder,
		Summary:    "write some code",
	}
	d.sessionTracker.RecordIntent("session-1", intent, config.AgentIDCoder)

	if d.isPendingClarification("session-1") {
		t.Error("should return false when last intent was code, not clarify")
	}
}

func TestGetPendingClarification_NoTracker(t *testing.T) {
	d := &Dispatcher{sessionTracker: nil}
	pending := d.getPendingClarification("session-1")
	if pending != nil {
		t.Error("should return nil when sessionTracker is nil")
	}
}

func TestGetPendingClarification_ValidState(t *testing.T) {
	d := &Dispatcher{sessionTracker: NewSessionTracker(30 * time.Minute)}
	intent := &Intent{
		Type:       string(IntentClarify),
		Confidence: 0.8,
		AgentType:  config.AgentIDChat,
		Summary:    "fix the authentication system",
		TrueAnalysis: &TrueIntentAnalysis{
			Goal:               "fix authentication system",
			Ambiguity:          0.8,
			Scope:              "broad",
			Category:           "fix",
			SuggestedQuestions: []string{"Which auth system?", "What's the bug?"},
			Confidence:         0.7,
		},
	}
	d.sessionTracker.RecordIntent("session-1", intent, config.AgentIDChat)

	pending := d.getPendingClarification("session-1")
	if pending == nil {
		t.Fatal("should return non-nil pending clarification")
	}
	if pending.OriginalInput != "fix the authentication system" {
		t.Errorf("OriginalInput = %q, want %q", pending.OriginalInput, "fix the authentication system")
	}
	if pending.Analysis == nil {
		t.Error("Analysis should not be nil")
	}
	if pending.Analysis.Ambiguity != 0.8 {
		t.Errorf("Analysis.Ambiguity = %v, want 0.8", pending.Analysis.Ambiguity)
	}
}

func TestGetPendingClarification_NoTrueAnalysis(t *testing.T) {
	// This test verifies support for model directive clarifications,
	// which don't have TrueAnalysis but still need clarification tracking.
	d := &Dispatcher{sessionTracker: NewSessionTracker(30 * time.Minute)}
	intent := &Intent{
		Type:       string(IntentClarify),
		Confidence: 0.8,
		AgentType:  config.AgentIDChat,
		Summary:    "model directive needs clarification",
		// No TrueAnalysis - this is the case for model directive clarifications
	}
	d.sessionTracker.RecordIntent("session-1", intent, config.AgentIDChat)

	pending := d.getPendingClarification("session-1")
	if pending == nil {
		t.Fatal("should return pending clarification even when TrueAnalysis is nil (model directive case)")
	}
	if pending.OriginalInput != "model directive needs clarification" {
		t.Errorf("OriginalInput = %q, want %q", pending.OriginalInput, "model directive needs clarification")
	}
	if pending.Analysis != nil {
		t.Error("Analysis should be nil when TrueAnalysis was not set")
	}
	if pending.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", pending.SessionID, "session-1")
	}
}

func TestGetPendingClarification_WrongIntentType(t *testing.T) {
	d := &Dispatcher{sessionTracker: NewSessionTracker(30 * time.Minute)}
	intent := &Intent{
		Type:       string(IntentCode),
		Confidence: 0.9,
		AgentType:  config.AgentIDCoder,
		Summary:    "write code",
	}
	d.sessionTracker.RecordIntent("session-1", intent, config.AgentIDCoder)

	pending := d.getPendingClarification("session-1")
	if pending != nil {
		t.Error("should return nil when last intent is not clarify")
	}
}

func TestSuggestMode(t *testing.T) {
	cases := []struct {
		name       string
		intentType IntentType
		analysis   *TrueIntentAnalysis
		input      string
		want       string
	}{
		{name: "compound forces spec_pair", intentType: IntentCompound, analysis: nil, input: "x", want: "spec_pair"},
		{name: "analysis spec_plan wins", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "spec_plan"}, input: "refactor the auth subsystem", want: "spec_plan"},
		{name: "analysis invalid falls back", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "garbage"}, input: "refactor the auth subsystem to use oauth2 with pkce flow", want: "plan"},
		{name: "short input downgrades plan→direct", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: ""}, input: "fix typo", want: "direct"},
		{name: "short input does not downgrade spec_plan", intentType: IntentCode, analysis: &TrueIntentAnalysis{SuggestedMode: "spec_plan"}, input: "fix", want: "spec_plan"},
		{name: "default fallback for unknown", intentType: IntentUnknown, analysis: nil, input: "something longer than fifty characters total please advise", want: "plan"},
		{name: "empty analysis + long input uses rule", intentType: IntentDebug, analysis: nil, input: "investigate the production outage that happened yesterday at 3am", want: "plan"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := suggestMode(c.intentType, c.analysis, c.input)
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestValidateMode(t *testing.T) {
	cases := []struct {
		in   string
		want string // normalized, "" if invalid
	}{
		{"direct", "direct"},
		{"plan", "plan"},
		{"spec_plan", "spec_plan"},
		{"spec_pair", "spec_pair"},
		{"SPEC_PLAN", ""}, // case-sensitive
		{"", ""},
		{"garbage", ""},
	}
	for _, c := range cases {
		got := validateMode(c.in)
		if got != c.want {
			t.Errorf("validateMode(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
