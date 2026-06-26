package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// --- RoutingTable tests ---

func TestRoutingTable_Defaults(t *testing.T) {
	rt := NewDefaultRoutingTable()

	tests := []struct {
		name              string
		intent            string
		wantActor         string
		wantReviewer      string
	}{
		{
			name:         "code intent routes to coder/planner",
			intent:       string(IntentCode),
			wantActor:    config.AgentIDCoder,
			wantReviewer: config.AgentIDPlanner,
		},
		{
			name:         "compound intent routes to coder/planner",
			intent:       string(IntentCompound),
			wantActor:    config.AgentIDCoder,
			wantReviewer: config.AgentIDPlanner,
		},
		{
			name:         "debug intent routes to debugger/analyst",
			intent:       string(IntentDebug),
			wantActor:    config.AgentIDDebugger,
			wantReviewer: config.AgentIDAnalyst,
		},
		{
			name:         "unknown intent falls back to coder/planner",
			intent:       "some-unknown-intent",
			wantActor:    config.AgentIDCoder,
			wantReviewer: config.AgentIDPlanner,
		},
		{
			name:         "empty intent falls back to coder/planner",
			intent:       "",
			wantActor:    config.AgentIDCoder,
			wantReviewer: config.AgentIDPlanner,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rt.ActorFor(tc.intent); got != tc.wantActor {
				t.Errorf("ActorFor(%q) = %q, want %q", tc.intent, got, tc.wantActor)
			}
			if got := rt.ReviewerFor(tc.intent); got != tc.wantReviewer {
				t.Errorf("ReviewerFor(%q) = %q, want %q", tc.intent, got, tc.wantReviewer)
			}
		})
	}
}

func TestRoutingTable_SetRoute(t *testing.T) {
	rt := NewDefaultRoutingTable()

	// Override the code intent to route to the debugger as actor.
	rt.SetRoute(string(IntentCode), config.AgentIDDebugger, config.AgentIDAnalyst)

	if got := rt.ActorFor(string(IntentCode)); got != config.AgentIDDebugger {
		t.Errorf("after SetRoute, ActorFor(%q) = %q, want %q",
			string(IntentCode), got, config.AgentIDDebugger)
	}
	if got := rt.ReviewerFor(string(IntentCode)); got != config.AgentIDAnalyst {
		t.Errorf("after SetRoute, ReviewerFor(%q) = %q, want %q",
			string(IntentCode), got, config.AgentIDAnalyst)
	}

	// Verify compound routing is unchanged.
	if got := rt.ActorFor(string(IntentCompound)); got != config.AgentIDCoder {
		t.Errorf("compound ActorFor should be unchanged; got %q, want %q",
			got, config.AgentIDCoder)
	}
}

func TestRoutingTable_SetRoute_PartialOverride(t *testing.T) {
	rt := NewDefaultRoutingTable()

	// Only override the actor, leave reviewer unchanged.
	rt.SetRoute(string(IntentCode), config.AgentIDDebugger, "")

	if got := rt.ActorFor(string(IntentCode)); got != config.AgentIDDebugger {
		t.Errorf("ActorFor after partial override = %q, want %q", got, config.AgentIDDebugger)
	}
	// Reviewer should remain the default for code.
	if got := rt.ReviewerFor(string(IntentCode)); got != config.AgentIDPlanner {
		t.Errorf("ReviewerFor after partial override = %q, want %q", got, config.AgentIDPlanner)
	}
}

func TestRoutingTable_SetRoute_NewIntent(t *testing.T) {
	rt := NewDefaultRoutingTable()

	// Add a route for an intent that has no default.
	rt.SetRoute("custom-intent", config.AgentIDAnalyst, config.AgentIDCoder)

	if got := rt.ActorFor("custom-intent"); got != config.AgentIDAnalyst {
		t.Errorf("ActorFor(new intent) = %q, want %q", got, config.AgentIDAnalyst)
	}
	if got := rt.ReviewerFor("custom-intent"); got != config.AgentIDCoder {
		t.Errorf("ReviewerFor(new intent) = %q, want %q", got, config.AgentIDCoder)
	}
}

func TestRoutingTable_NilSafe(t *testing.T) {
	var rt *RoutingTable

	// ActorFor and ReviewerFor on a nil receiver should return sensible
	// defaults without panicking.
	if got := rt.ActorFor(string(IntentCode)); got != config.AgentIDCoder {
		t.Errorf("nil ActorFor = %q, want %q", got, config.AgentIDCoder)
	}
	if got := rt.ReviewerFor(string(IntentCode)); got != config.AgentIDPlanner {
		t.Errorf("nil ReviewerFor = %q, want %q", got, config.AgentIDPlanner)
	}

	// SetRoute on nil should be a no-op, not a panic.
	rt.SetRoute("foo", "bar", "baz")
}

// --- PlannerThresholds tests ---

func TestPlannerThresholds_Defaults(t *testing.T) {
	pt := NewDefaultThresholds()

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"InterviewAmbiguity", pt.InterviewAmbiguity, float64(0.6)},
		{"MaxPlanSteps", pt.MaxPlanSteps, 10},
		{"PlannerTimeout", pt.PlannerTimeout, 120 * time.Second},
		{"SimpleInputMaxChars", pt.SimpleInputMaxChars, 100},
		{"PairInputMinChars", pt.PairInputMinChars, 200},
		{"ApprovalStepThreshold", pt.ApprovalStepThreshold, 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			switch want := tc.want.(type) {
			case float64:
				got := tc.got.(float64)
				if got != want {
					t.Errorf("%s = %v, want %v", tc.name, got, want)
				}
			case int:
				got := tc.got.(int)
				if got != want {
					t.Errorf("%s = %v, want %v", tc.name, got, want)
				}
			case time.Duration:
				got := tc.got.(time.Duration)
				if got != want {
					t.Errorf("%s = %v, want %v", tc.name, got, want)
				}
			default:
				t.Fatalf("unsupported type for %s", tc.name)
			}
		})
	}
}

// --- BuildPlannerPromptHint tests ---

// newTestRegistryWithSpecs builds a minimal AgentRegistry with the given specs
// pre-registered, without triggering AGENT.md discovery.
func newTestRegistryWithSpecs(specs ...*AgentSpec) *AgentRegistry {
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]map[string]*AgentLoop),
		activeQueues:    make(map[string]*QueueEntry),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	for _, s := range specs {
		_ = r.RegisterSpec(s)
	}
	return r
}

func TestBuildPlannerPromptHint_NilRegistry(t *testing.T) {
	got := BuildPlannerPromptHint(nil)
	if got != "" {
		t.Errorf("BuildPlannerPromptHint(nil) = %q, want empty string", got)
	}
}

func TestBuildPlannerPromptHint_EmptyRegistry(t *testing.T) {
	r := newTestRegistryWithSpecs()
	got := BuildPlannerPromptHint(r)
	if got != "" {
		t.Errorf("BuildPlannerPromptHint(empty registry) = %q, want empty string", got)
	}
}

func TestBuildPlannerPromptHint_IncludesExecutors(t *testing.T) {
	r := newTestRegistryWithSpecs(
		&AgentSpec{
			ID:          config.AgentIDCoder,
			Name:        "Coder",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: "writes and refactors code",
		},
		&AgentSpec{
			ID:          config.AgentIDDebugger,
			Name:        "Debugger",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: "troubleshoots and fixes bugs",
		},
	)

	got := BuildPlannerPromptHint(r)

	// Both executors should appear.
	if !strings.Contains(got, config.AgentIDCoder) {
		t.Errorf("hint should contain coder ID; got:\n%s", got)
	}
	if !strings.Contains(got, config.AgentIDDebugger) {
		t.Errorf("hint should contain debugger ID; got:\n%s", got)
	}
	if !strings.Contains(got, "writes and refactors code") {
		t.Errorf("hint should contain coder description; got:\n%s", got)
	}
}

func TestBuildPlannerPromptHint_ExcludesPlanner(t *testing.T) {
	r := newTestRegistryWithSpecs(
		&AgentSpec{
			ID:          config.AgentIDPlanner,
			Name:        "Planner",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: "decomposes tasks into steps",
		},
		&AgentSpec{
			ID:          config.AgentIDCoder,
			Name:        "Coder",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: "writes code",
		},
	)

	got := BuildPlannerPromptHint(r)

	// Planner should NOT appear in the hint.
	if strings.Contains(got, config.AgentIDPlanner) {
		t.Errorf("hint should NOT contain planner ID; got:\n%s", got)
	}
	// Coder should still appear.
	if !strings.Contains(got, config.AgentIDCoder) {
		t.Errorf("hint should contain coder ID; got:\n%s", got)
	}
}

func TestBuildPlannerPromptHint_ExcludesDisabled(t *testing.T) {
	r := newTestRegistryWithSpecs(
		&AgentSpec{
			ID:          config.AgentIDCoder,
			Name:        "Coder",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: "writes code",
		},
		&AgentSpec{
			ID:          config.AgentIDDebugger,
			Name:        "Debugger",
			Role:        RoleExecutor,
			Enabled:     false, // disabled
			Description: "fixes bugs",
		},
	)

	got := BuildPlannerPromptHint(r)

	if !strings.Contains(got, config.AgentIDCoder) {
		t.Errorf("hint should contain enabled coder; got:\n%s", got)
	}
	if strings.Contains(got, config.AgentIDDebugger) {
		t.Errorf("hint should NOT contain disabled debugger; got:\n%s", got)
	}
}

func TestBuildPlannerPromptHint_ExcludesNonExecutors(t *testing.T) {
	r := newTestRegistryWithSpecs(
		&AgentSpec{
			ID:          config.AgentIDCoder,
			Name:        "Coder",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: "writes code",
		},
		&AgentSpec{
			ID:           "code-reviewer",
			Name:         "Code Reviewer",
			Role:         RoleReviewer,
			Enabled:      true,
			Description:  "reviews code",
			ReviewsDomain: "code",
		},
	)

	got := BuildPlannerPromptHint(r)

	if !strings.Contains(got, config.AgentIDCoder) {
		t.Errorf("hint should contain coder executor; got:\n%s", got)
	}
	if strings.Contains(got, "code-reviewer") {
		t.Errorf("hint should NOT contain reviewer; got:\n%s", got)
	}
}

func TestBuildPlannerPromptHint_TruncatesDescription(t *testing.T) {
	longDesc := strings.Repeat("x", 150) // 150 chars, well over the 80-char limit

	r := newTestRegistryWithSpecs(
		&AgentSpec{
			ID:          config.AgentIDCoder,
			Name:        "Coder",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: longDesc,
		},
	)

	got := BuildPlannerPromptHint(r)

	// The hint should contain the coder ID and a truncated description.
	if !strings.Contains(got, config.AgentIDCoder) {
		t.Fatalf("hint should contain coder ID; got:\n%s", got)
	}

	// Extract the description portion (after "→ ").
	idx := strings.Index(got, "→ ")
	if idx < 0 {
		t.Fatalf("hint should contain arrow separator; got:\n%s", got)
	}
	descPart := strings.TrimSpace(got[idx+len("→ "):])
	if len(descPart) != maxHintDescriptionLen {
		t.Errorf("description should be truncated to %d chars, got %d: %q",
			maxHintDescriptionLen, len(descPart), descPart)
	}
}

func TestBuildPlannerPromptHint_FallsBackToPurpose(t *testing.T) {
	r := newTestRegistryWithSpecs(
		&AgentSpec{
			ID:          config.AgentIDCoder,
			Name:        "Coder",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: "", // empty description
			Purpose:     "purpose-driven code agent",
		},
	)

	got := BuildPlannerPromptHint(r)

	if !strings.Contains(got, "purpose-driven code agent") {
		t.Errorf("hint should fall back to Purpose when Description is empty; got:\n%s", got)
	}
}

func TestBuildPlannerPromptHint_Format(t *testing.T) {
	r := newTestRegistryWithSpecs(
		&AgentSpec{
			ID:          config.AgentIDCoder,
			Name:        "Coder",
			Role:        RoleExecutor,
			Enabled:     true,
			Description: "writes code",
		},
	)

	got := BuildPlannerPromptHint(r)

	// Verify each line follows the "- "<ID>" → <desc>" pattern.
	expectedLine := "- \"" + config.AgentIDCoder + "\" → writes code\n"
	if !strings.Contains(got, expectedLine) {
		t.Errorf("hint should contain formatted line %q; got:\n%s", expectedLine, got)
	}
}

// --- SecurityKeywords tests ---

func TestSecurityKeywords(t *testing.T) {
	kw := SecurityKeywords()

	if len(kw) == 0 {
		t.Fatal("SecurityKeywords() returned empty list")
	}

	// Verify "security" is present.
	found := false
	for _, k := range kw {
		if k == "security" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SecurityKeywords() should contain \"security\"; got %v", kw)
	}

	// Verify the full expected list.
	expected := []string{
		"security", "authentication", "authorization",
		"encryption", "credential", "password", "token",
		"vulnerable", "vulnerability", "cve",
	}
	if len(kw) != len(expected) {
		t.Fatalf("SecurityKeywords() has %d items, want %d", len(kw), len(expected))
	}
	for i, want := range expected {
		if kw[i] != want {
			t.Errorf("SecurityKeywords()[%d] = %q, want %q", i, kw[i], want)
		}
	}
}
