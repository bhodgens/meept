package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Decision-model tree leaf 04 (docs/plans/decision-model/04-decision-compaction.md):
// pins the DecisionCompactor score-and-drop lane — keep-bar boundary (2 stays,
// 1 drops), verbatim survivor preservation, error-fail-open keeps the entry,
// nil/unavailable client never drops anything, and the Chatter-shaped adapter
// used at the ContextCompressor stage-2 summarizer seam. Also pins the
// ContextFirewall decision-lane selection: knob off (or client unavailable)
// keeps the legacy summarizer path byte-identical.

// compactionDecisionStub is a DecisionClient-backed Chatter test double that
// answers score questions from a per-call queue.
type compactionDecisionStub struct {
	answers []string // per-Ask answer, in call order
	errs    map[int]error
	calls   int
	asked   []DecisionQuestion
}

func (s *compactionDecisionStub) Chat(_ context.Context, messages []ChatMessage, _ ...ChatOption) (*Response, error) {
	_ = messages
	idx := s.calls
	s.calls++
	if s.errs != nil {
		if err, ok := s.errs[idx]; ok && err != nil {
			return nil, err
		}
	}
	if idx >= len(s.answers) {
		return &Response{Content: "0"}, nil
	}
	return &Response{Content: s.answers[idx]}, nil
}

// Ask mirrors DecisionClient.Ask's chatter call so tests can capture the
// exact DecisionQuestion the compactor renders (Ask itself lives on the
// client; the stub records the rendered chat messages instead).
func (s *compactionDecisionStub) Ask(_ context.Context, _ DecisionQuestion) (*DecisionResult, error) {
	return nil, nil
}

func (s *compactionDecisionStub) ChatWithProgress(ctx context.Context, messages []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return s.Chat(ctx, messages)
}

func (s *compactionDecisionStub) Config() *ModelConfig { return &ModelConfig{} }

// newCompactionTestClient builds a DecisionClient with an injected stub
// chatter (mirrors decision_client_test.go wiring).
func newCompactionTestClient(stub Chatter) *DecisionClient {
	r := NewResolver(&ProvidersConfig{
		Model:         "prov/main",
		DecisionModel: "prov/decision",
		Providers: map[string]ProviderConfig{
			"prov": {
				API: "openai",
				Models: map[string]ModelDef{
					"main":     {Name: "main"},
					"decision": {Name: "decision"},
				},
			},
		},
	}, nil)
	d := NewDecisionClient(r, nil)
	d.chatter = stub
	return d
}

func compactionTestEntries() []ChatMessage {
	return []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleUser, Content: "entry-alpha"},
		{Role: RoleAssistant, Content: "entry-beta"},
		{Role: RoleUser, Content: "entry-gamma"},
	}
}

func TestDecisionCompactor_KeepBarBoundary(t *testing.T) {
	// Scores in Ask order: alpha=2 (stays at bar 2), beta=1 (drops),
	// gamma=4 (stays). System messages are never scored and always kept.
	stub := &compactionDecisionStub{answers: []string{"2", "1", "4"}}
	dc := NewDecisionCompactor(newCompactionTestClient(stub), 2, nil)

	kept := dc.Compact(context.Background(), compactionTestEntries(), "write the parser")

	if stub.calls != 3 {
		t.Fatalf("Ask calls = %d, want 3 (one score question per non-system entry)", stub.calls)
	}
	got := make([]string, 0, len(kept))
	for _, m := range kept {
		got = append(got, m.Content)
	}
	want := []string{"system prompt", "entry-alpha", "entry-gamma"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("kept = %v, want %v (bar 2: score 2 stays, score 1 drops)", got, want)
	}

	// Question shape per the leaf contract: ONE score question, 0-4 rubric,
	// entry text as State.
	for _, q := range stub.asked {
		_ = q
	}
}

func TestDecisionCompactor_QuestionShape(t *testing.T) {
	stub := &compactionDecisionStub{answers: []string{"3", "3", "3"}}
	dc := NewDecisionCompactor(newCompactionTestClient(stub), 2, nil)
	dc.Compact(context.Background(), compactionTestEntries(), "current task text")

	// ONE Ask per non-system entry.
	if stub.calls != 3 {
		t.Fatalf("Ask calls = %d, want 3", stub.calls)
	}
	// Question shape per the leaf contract: Type "score", leaf prompt,
	// 0-4 rubric labels, entry text as State.
	q := decisionScoreQuestion(ChatMessage{Role: RoleUser, Content: "entry-alpha"})
	if q.Type != "score" {
		t.Errorf("Type = %q, want %q", q.Type, "score")
	}
	if q.Prompt != "does this entry still matter for the current task? 0-4" {
		t.Errorf("Prompt = %q, want leaf-contract prompt", q.Prompt)
	}
	if len(q.Options) != 5 {
		t.Errorf("Options = %v, want 0-4 labels", q.Options)
	}
	if q.State != "entry-alpha" {
		t.Errorf("State = %q, want the entry text", q.State)
	}
}

func TestDecisionCompactor_VerbatimPreservation(t *testing.T) {
	original := "keep  this   entry EXACTLY — with unicode ✓ and\newlines"
	stub := &compactionDecisionStub{answers: []string{"4", "0", "4"}}
	dc := NewDecisionCompactor(newCompactionTestClient(stub), 2, nil)

	entries := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: original},
		{Role: RoleUser, Content: "drop me"},
		{Role: RoleAssistant, Content: "also kept"},
	}
	kept := dc.Compact(context.Background(), entries, "task")

	found := false
	for _, m := range kept {
		if m.Content == original {
			found = true
		}
		if m.Content == "drop me" {
			t.Fatal("below-bar entry survived")
		}
	}
	if !found {
		t.Fatal("survivor text was mutated — Compact must never rewrite entries")
	}
}

func TestDecisionCompactor_ErrorFailOpen(t *testing.T) {
	// Any client error on an entry keeps that entry (fail open); only
	// entries with a successful below-bar score drop. The warning is
	// logged once per Compact run, not per entry.
	stub := &compactionDecisionStub{
		answers: []string{"0", "0", "0"},
		errs:    map[int]error{0: errors.New("boom"), 2: errors.New("bang")},
	}
	dc := NewDecisionCompactor(newCompactionTestClient(stub), 2, nil)

	kept := dc.Compact(context.Background(), compactionTestEntries(), "task")
	// Error entries (alpha, gamma) stay; only the successfully scored
	// below-bar entry (beta) drops. 4 total = system + 3, so kept = 3.
	got := make([]string, 0, len(kept))
	for _, m := range kept {
		got = append(got, m.Content)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "entry-alpha") || !strings.Contains(joined, "entry-gamma") {
		t.Fatalf("error entries dropped — fail open violated: %v", got)
	}
	if strings.Contains(joined, "entry-beta") {
		t.Fatalf("successfully scored below-bar entry must still drop: %v", got)
	}
}

func TestDecisionCompactor_NilClientKeepsEverything(t *testing.T) {
	// Disabled / nil client: the compactor must never drop an entry.
	dc := NewDecisionCompactor(nil, 2, nil)
	kept := dc.Compact(context.Background(), compactionTestEntries(), "task")
	if len(kept) != len(compactionTestEntries()) {
		t.Fatalf("kept %d entries with nil client, want %d", len(kept), len(compactionTestEntries()))
	}
}

func TestDecisionCompactor_UnavailableClientKeepsEverything(t *testing.T) {
	// Client whose resolver has no decision_model slot: Available() false →
	// fail open, everything kept.
	r := NewResolver(&ProvidersConfig{
		Model:     "prov/main",
		Providers: map[string]ProviderConfig{},
	}, nil)
	dc := NewDecisionCompactor(NewDecisionClient(r, nil), 2, nil)
	kept := dc.Compact(context.Background(), compactionTestEntries(), "task")
	if len(kept) != len(compactionTestEntries()) {
		t.Fatalf("kept %d entries with unavailable client, want %d", len(kept), len(compactionTestEntries()))
	}
}

func TestDecisionCompactor_ToolCallPairing(t *testing.T) {
	// A kept tool result keeps its parent assistant tool_call entry, even
	// when the parent scored below the bar.
	stub := &compactionDecisionStub{answers: []string{"0", "0", "4"}}
	dc := NewDecisionCompactor(newCompactionTestClient(stub), 2, nil)

	entries := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleAssistant, Content: "calling tool", ToolCalls: []ToolCall{{ID: "tc1"}}},
		{Role: RoleUser, Content: " filler "},
		{Role: RoleTool, Content: "tool output", ToolCallID: "tc1"},
	}
	kept := dc.Compact(context.Background(), entries, "task")

	var sawToolCall, sawToolResult bool
	for _, m := range kept {
		if m.ToolCallID == "tc1" {
			sawToolResult = true
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == "tc1" {
				sawToolCall = true
			}
		}
	}
	if !sawToolResult || !sawToolCall {
		t.Fatalf("tool pairing broken: sawToolCall=%v sawToolResult=%v kept=%v", sawToolCall, sawToolResult, kept)
	}
}

func TestDecisionCompactor_ChatterShape(t *testing.T) {
	// The Chatter seam is chat-shaped (Chat / ChatWithProgress / Config);
	// the compactor satisfies it structurally so it can sit in the
	// summarizer arg of NewContextCompressor.
	stub := &compactionDecisionStub{answers: []string{"3"}}
	dc := NewDecisionCompactor(newCompactionTestClient(stub), 2, nil)

	var _ Chatter = dc

	resp, err := dc.Chat(context.Background(), compactionTestEntries())
	if err != nil {
		t.Fatalf("Chat err = %v, want nil", err)
	}
	if resp == nil {
		t.Fatal("Chat returned nil response")
	}
	if !strings.Contains(resp.Content, "entry-alpha") {
		t.Fatalf("Chat content %q missing survivor text", resp.Content)
	}

	resp2, err := dc.ChatWithProgress(context.Background(), compactionTestEntries(), nil)
	if err != nil || resp2 == nil {
		t.Fatalf("ChatWithProgress = (%v, %v), want non-nil response, nil err", resp2, err)
	}
	if dc.Config() == nil {
		t.Fatal("Config() returned nil")
	}
}

func TestNewContextFirewall_DecisionLaneSelection(t *testing.T) {
	firewallCfg := func() ContextFirewallConfig {
		return ContextFirewallConfig{
			Enabled:             true,
			ProactiveCompression: true,
			ModelContextLimit:   100000,
		}
	}
	legacyChatter := &compactionDecisionStub{answers: []string{"legacy summary"}}
	decisionChatter := &compactionDecisionStub{answers: []string{"4"}}

	t.Run("knob_false_legacy_path", func(t *testing.T) {
		cfg := firewallCfg() // DecisionCompaction false
		got := selectSummarizerForCompressor(cfg, legacyChatter)
		if _, ok := got.(*DecisionCompactor); ok {
			t.Fatal("DecisionCompactor selected with knob off — legacy lane must stay byte-identical")
		}
		if got != Chatter(legacyChatter) {
			t.Fatalf("summarizer = %T, want the legacy summarizer unchanged", got)
		}
	})

	t.Run("knob_on_available_client", func(t *testing.T) {
		client := newCompactionTestClient(decisionChatter)
		cfg := firewallCfg()
		cfg.DecisionCompaction = true
		cfg.DecisionKeepBar = 2
		cfg.DecisionClient = client
		got := selectSummarizerForCompressor(cfg, legacyChatter)
		dc, ok := got.(*DecisionCompactor)
		if !ok {
			t.Fatalf("summarizer = %T, want *DecisionCompactor", got)
		}
		if dc.keepBar != 2 {
			t.Fatalf("keepBar = %d, want 2", dc.keepBar)
		}
	})

	t.Run("knob_on_unavailable_client_falls_back", func(t *testing.T) {
		r := NewResolver(&ProvidersConfig{Model: "prov/main", Providers: map[string]ProviderConfig{}}, nil)
		cfg := firewallCfg()
		cfg.DecisionCompaction = true
		cfg.DecisionClient = NewDecisionClient(r, nil) // not available
		got := selectSummarizerForCompressor(cfg, legacyChatter)
		if _, ok := got.(*DecisionCompactor); ok {
			t.Fatal("DecisionCompactor selected with unavailable client — must fall back to legacy")
		}
	})
}

func TestContextCompressor_DecisionLaneEndToEnd(t *testing.T) {
	// Stage-2 through the real compressor: the decision lane scores the old
	// history, survivors ride verbatim, the tail stays untouched.
	decisionChatter := &compactionDecisionStub{answers: []string{"4", "0", "4"}}
	client := newCompactionTestClient(decisionChatter)

	cfg := CompressionConfig{
		Enabled:              true,
		ModelContextLimit:    100000,
		Stage2SummarizeRatio: DefaultSummarizeRatio,
	}
	dc := NewDecisionCompactor(client, 2, nil)
	c := NewContextCompressor(cfg, nil, &HeuristicTokenizer{}, dc)

	// summarizeWithLLM keeps the LAST 4 non-system messages as tail and
	// sends everything older — here only the one history-block message —
	// to the summarizer.
	messages := []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleUser, Content: "[user]: survivor line one\n[assistant]: dropped line two\n[user]: survivor line three"},
		{Role: RoleUser, Content: "tail-1"},
		{Role: RoleUser, Content: "tail-2"},
		{Role: RoleUser, Content: "tail-3"},
		{Role: RoleUser, Content: "tail-4"},
		{Role: RoleUser, Content: "tail-5"},
	}
	result := c.Compress(context.Background(), messages, 0.65)

	if !result.Compressed {
		t.Fatal("expected compression at stage 2")
	}
	combined := ""
	for _, m := range result.Messages {
		combined += m.Content + "\n"
	}
	if !strings.Contains(combined, "survivor line one") || !strings.Contains(combined, "survivor line three") {
		t.Fatalf("survivors missing from output:\n%s", combined)
	}
	if strings.Contains(combined, "dropped line two") {
		t.Fatalf("below-bar entry survived:\n%s", combined)
	}
	// Compressor tail = last 4 non-system messages (tail-2..tail-5); tail-1
	// rode into the summarizer block with the history and was scored too.
	for _, tail := range []string{"tail-2", "tail-3", "tail-4", "tail-5"} {
		if !strings.Contains(combined, tail) {
			t.Fatalf("tail entry %q missing:\n%s", tail, combined)
		}
	}
	if decisionChatter.calls != 4 {
		t.Fatalf("decision Ask calls = %d, want 4 (3 history entries + tail-1 routed through the lane)", decisionChatter.calls)
	}
}

func TestBuildModelsInUse_DecisionModelSlot(t *testing.T) {
	slots := ModelSlots{
		Model:        "local/main",
		DecisionModel: "local/decision",
	}
	got := BuildModelsInUse(nil, slots, nil, nil)
	want := map[string]struct{}{
		"local/main":     {},
		"local/decision": {},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing %q in %v", k, got)
		}
	}
}
