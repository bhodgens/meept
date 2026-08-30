package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/skills"
)

// ---------------------------------------------------------------------------
// Local test stubs (shared mocks in evolver_test.go are not mutated)
// ---------------------------------------------------------------------------

// capturingLLMChatter records the user prompt of every Chat call so tests can
// assert on the exact prompt the evolver built.
type capturingLLMChatter struct {
	mu      sync.Mutex
	prompts []string
	resp    string
}

func (c *capturingLLMChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	user := ""
	if len(messages) > 0 {
		user = messages[len(messages)-1].Content
	}
	c.mu.Lock()
	c.prompts = append(c.prompts, user)
	c.mu.Unlock()
	return &llm.Response{Content: c.resp}, nil
}

func (c *capturingLLMChatter) lastPrompt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.prompts) == 0 {
		return ""
	}
	return c.prompts[len(c.prompts)-1]
}

func (c *capturingLLMChatter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.prompts)
}

// stubTraceProvider is a fixed TraceProvider that records the sampling budgets
// it was called with.
type stubTraceProvider struct {
	records                                []selfimprove.TraceRecord
	gotMaxFails, gotMaxPasses, gotMaxChars int
}

func (s *stubTraceProvider) Sample(maxFails, maxPasses, maxChars int) ([]selfimprove.TraceRecord, error) {
	s.gotMaxFails, s.gotMaxPasses, s.gotMaxChars = maxFails, maxPasses, maxChars
	return s.records, nil
}

// improvedWikiContent is the candidate content the refine LLM mock returns.
const improvedWikiContent = "---\nname: wiki-skill\ndescription: improved\n---\n\n# wiki-skill\n\nimproved"

// newWikiTestEvolver builds a Pass-A-ready evolver: one registered skill with
// enough injections, a capturing LLM mock producing an improve_skill decision,
// and a heuristic verifier in the requested accept/reject mode.
func newWikiTestEvolver(t *testing.T, accept bool, autoApply bool, chat *capturingLLMChatter) (*Evolver, *capturingLLMChatter) {
	t.Helper()
	skillName := "wiki-skill"
	skillContent := "---\nname: wiki-skill\ndescription: test\n---\n\n# wiki-skill\n\noriginal content"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	if err := writer.WriteSkill(skillName, skillContent); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}
	registry := skills.NewRegistry()
	parsed, err := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	registry.Register(parsed)

	usage := newStubUsageTracker()
	usage.SetStats(skillName, &UsageStats{
		SkillName:     skillName,
		InjectCount:   10,
		PositiveCount: 5,
		Effectiveness: 0.5,
	})

	if chat == nil {
		chat = &capturingLLMChatter{resp: makeRefineLLMResponse("improve_skill", improvedWikiContent)}
	}
	cfg := defaultEvolverConfig()
	cfg.AutoApply = autoApply

	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		newTestVerifier(accept), chat, nil,
		cfg, slog.Default(),
	)
	return evolver, chat
}

// ---------------------------------------------------------------------------
// Task 1: options + struct fields
// ---------------------------------------------------------------------------

func TestEvolverWikiOptions_NilGuards(t *testing.T) {
	e := &Evolver{}
	WithTraceProvider(nil)(e)
	WithWikiStore(nil)(e)
	if e.traceProvider != nil || e.wiki != nil {
		t.Fatal("nil options must be no-ops")
	}
}

func TestEvolverWikiOptions_WireFields(t *testing.T) {
	e := &Evolver{}
	tp := &stubTraceProvider{}
	ws := selfimprove.NewWikiStore(t.TempDir(), slog.Default())
	WithTraceProvider(tp)(e)
	WithWikiStore(ws)(e)
	if e.traceProvider != tp {
		t.Fatal("WithTraceProvider must wire the provider")
	}
	if e.wiki != ws {
		t.Fatal("WithWikiStore must wire the store")
	}
}

// ---------------------------------------------------------------------------
// Task 2: trace-fed refine prompt
// ---------------------------------------------------------------------------

// TestPassARefine_PromptCarriesLedgerTracesIndex runs one Pass A cycle with a
// wiki store carrying a rejected ledger row and a trace provider returning one
// failure + one success, and asserts the refine prompt carries all three
// sections in the frozen order.
func TestPassARefine_PromptCarriesLedgerTracesIndex(t *testing.T) {
	ws := selfimprove.NewWikiStore(t.TempDir(), slog.Default())
	if err := ws.AppendSkillImpact(selfimprove.SkillImpactEntry{
		Time:      time.Now().UTC(),
		Action:    "improve_skill",
		SkillName: "wiki-skill",
		Diff:      "rejected diff",
		Score:     0.4,
		Accepted:  false,
		Reason:    "grounded_in_evidence score 0.50 is below floor 0.50",
	}); err != nil {
		t.Fatalf("AppendSkillImpact: %v", err)
	}
	// Seed one pattern page so the rebuilt index has a bullet to carry.
	now := time.Now().UTC()
	if _, err := ws.UpsertPattern(&selfimprove.LearnedPattern{
		ID: "pat-1", Type: selfimprove.PatternTypeStrategy, Status: selfimprove.PatternStatusActive,
		Domain: "code", Description: "wiki seed pattern", Pattern: "p",
		Confidence: 0.8, UseCount: 5,
		CreatedAt: now, UpdatedAt: now,
		ContentHash: "seedhash01",
	}); err != nil {
		t.Fatalf("UpsertPattern: %v", err)
	}
	if err := ws.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}

	tp := &stubTraceProvider{records: []selfimprove.TraceRecord{
		{
			ID: "tr-fail-1", SessionID: "s1", Domain: "code",
			Outcome: selfimprove.TraceOutcomeFailure, Error: "boom",
			Steps: []selfimprove.TraceStep{
				{Action: "edit_file", Input: "a.go", Output: "compile error", Success: false},
			},
			CreatedAt: time.Now().UTC(),
		},
		{
			ID: "tr-pass-1", SessionID: "s2", Domain: "debugging",
			Outcome: selfimprove.TraceOutcomeSuccess,
			Steps: []selfimprove.TraceStep{
				{Action: "assistant_response", Output: "fixed", Success: true},
			},
			CreatedAt: time.Now().UTC(),
		},
	}}

	evolver, mock := newWikiTestEvolver(t, true, true, nil)
	WithWikiStore(ws)(evolver)
	WithTraceProvider(tp)(evolver)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if report.Refined != 1 {
		t.Fatalf("expected 1 refined, got %d (report: %+v)", report.Refined, report)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", mock.callCount())
	}
	if tp.gotMaxFails != traceSampleMaxFails || tp.gotMaxPasses != traceSampleMaxPass || tp.gotMaxChars != traceSampleMaxChars {
		t.Fatalf("sampling budgets wrong: fails=%d pass=%d chars=%d",
			tp.gotMaxFails, tp.gotMaxPasses, tp.gotMaxChars)
	}

	prompt := mock.lastPrompt()
	ledgerIdx := strings.Index(prompt, "--- Prior Skill Impact (do not repeat rejected proposals) ---")
	skillIdx := strings.Index(prompt, "wiki-skill")
	indexIdx := strings.Index(prompt, "--- Wiki Index ---")
	tracesIdx := strings.Index(prompt, "--- Execution Traces ---")
	failIdx := strings.Index(prompt, "[outcome=failure]")
	passIdx := strings.Index(prompt, "[outcome=success]")
	for name, idx := range map[string]int{
		"ledger header":  ledgerIdx,
		"skill name":     skillIdx,
		"index header":   indexIdx,
		"traces header":  tracesIdx,
		"failure marker": failIdx,
		"success marker": passIdx,
	} {
		if idx < 0 {
			t.Fatalf("prompt missing %s:\n%s", name, prompt)
		}
	}
	if !(ledgerIdx < indexIdx && indexIdx < tracesIdx) {
		t.Fatalf("section order wrong: ledger=%d index=%d traces=%d", ledgerIdx, indexIdx, tracesIdx)
	}
	if !strings.Contains(prompt, "rejected diff") {
		t.Fatalf("prompt must carry the ledger row diff:\n%s", prompt)
	}
	if !(failIdx < passIdx) {
		t.Fatalf("failures must render before successes:\n%s", prompt)
	}
	if stepIdx := strings.Index(prompt, "edit_file"); stepIdx < tracesIdx {
		t.Fatalf("trace steps must render after the traces header:\n%s", prompt)
	}
	if !strings.Contains(prompt, "patterns/") {
		t.Fatalf("prompt must carry wiki index bullets:\n%s", prompt)
	}
	// The prepended context comes before the usage-stats line.
	if usageIdx := strings.Index(prompt, "Usage stats:"); usageIdx < tracesIdx {
		t.Fatalf("wiki context must be prepended before the usage stats:\n%s", prompt)
	}
}

// TestPassARefine_NilWikiNilTraces_GoldenPrompt pins the degraded path: with
// no wiki and no trace provider the prompt must be byte-identical to the
// pre-change buildRefinePrompt output (usage stats lead).
func TestPassARefine_NilWikiNilTraces_GoldenPrompt(t *testing.T) {
	// AutoApply=false so the cycle does not rewrite the skill before the
	// golden comparison reads its content.
	evolver, mock := newWikiTestEvolver(t, true, false, nil)
	if _, err := evolver.RunCycle(context.Background()); err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", mock.callCount())
	}

	currentContent, err := evolver.writer.ReadSkill("wiki-skill")
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	allStats, err := evolver.usage.GetAllStats()
	if err != nil {
		t.Fatalf("GetAllStats: %v", err)
	}
	golden := evolver.buildRefinePrompt("wiki-skill", currentContent, allStats["wiki-skill"], "")
	if mock.lastPrompt() != golden {
		t.Fatalf("nil wiki+traces prompt diverged from golden:\n--- got ---\n%s\n--- want ---\n%s",
			mock.lastPrompt(), golden)
	}
	if !strings.Contains(golden, "Usage stats: inject_count=10, positive=5, negative=0, neutral=0, effectiveness=0.50") {
		t.Fatalf("golden prompt must lead with usage stats:\n%s", golden)
	}
}

// eLoadCycleWikiContextForTest builds a throwaway evolver wired with the
// given wiki/trace sources and runs loadCycleWikiContext against them.
func eLoadCycleWikiContextForTest(t *testing.T, ws *selfimprove.WikiStore, tp TraceProvider) *cycleWikiContext {
	t.Helper()
	evolver := &Evolver{wiki: ws, traceProvider: tp, logger: slog.New(slog.DiscardHandler)}
	return evolver.loadCycleWikiContext()
}

// TestBuildWikiContext_KeepsNewestRows checks the impactLedgerMaxChars cap
// keeps the newest ledger rows and drops the oldest.
func TestBuildWikiContext_KeepsNewestRows(t *testing.T) {
	ws := selfimprove.NewWikiStore(t.TempDir(), slog.Default())
	longReason := strings.Repeat("r", 500)
	for i := 0; i < 50; i++ {
		if err := ws.AppendSkillImpact(selfimprove.SkillImpactEntry{
			Time:      time.Now().UTC(),
			Action:    "improve_skill",
			SkillName: fmt.Sprintf("skill-%02d", i),
			Score:     0.5,
			Reason:    longReason,
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	wctx := eLoadCycleWikiContextForTest(t, ws, nil)
	if len(wctx.ledger) == 0 || len(wctx.ledger) > impactLedgerMaxChars {
		t.Fatalf("ledger cap violated: %d chars", len(wctx.ledger))
	}
	if !strings.Contains(wctx.ledger, "skill-49") {
		t.Fatalf("newest row must be kept:\n%s", wctx.ledger)
	}
	if strings.Contains(wctx.ledger, "skill-00") {
		t.Fatalf("oldest row must be dropped when over cap:\n%s", wctx.ledger)
	}
}

// ---------------------------------------------------------------------------
// Task 3: impact ledger on every verdict
// ---------------------------------------------------------------------------

func TestProcessProposal_RecordsRejectAndAccept(t *testing.T) {
	wikiDir := t.TempDir()
	ws := selfimprove.NewWikiStore(wikiDir, slog.Default())

	// Reject branch: newTestVerifier(false) always rejects.
	evolver, _ := newWikiTestEvolver(t, false, true, nil)
	WithWikiStore(ws)(evolver)
	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle (reject): %v", err)
	}
	if report.Rejected != 1 || report.Refined != 0 {
		t.Fatalf("expected 1 rejected / 0 refined, got %+v", report)
	}

	data, err := os.ReadFile(filepath.Join(wikiDir, "skill-impact.md"))
	if err != nil {
		t.Fatalf("ledger must exist after a reject verdict: %v", err)
	}
	var entry selfimprove.SkillImpactEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry); err != nil {
		t.Fatalf("ledger row not JSONL: %v\nrow: %s", err, data)
	}
	if entry.Accepted {
		t.Fatal("first verdict must be Accepted=false")
	}
	if entry.Action != "improve_skill" || entry.SkillName != "wiki-skill" {
		t.Fatalf("entry fields wrong: %+v", entry)
	}
	if entry.Score <= 0 {
		t.Fatalf("entry score must carry verifier score: %+v", entry)
	}
	if entry.Diff != improvedWikiContent {
		t.Fatalf("diff must record candidate content: %q", entry.Diff)
	}
	if entry.Reason == "" {
		t.Fatal("entry must carry rejection reasons")
	}

	// Accept branch: fresh evolver, accept verifier, AutoApply=false,
	// planMgr=nil → the verdict is accepted but nothing is applied; the
	// ledger records the verdict, not the apply outcome.
	evolver2, _ := newWikiTestEvolver(t, true, false, nil)
	WithWikiStore(ws)(evolver2)
	report2, err := evolver2.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle (accept): %v", err)
	}
	if report2.Refined != 0 || report2.Planned != 1 {
		t.Fatalf("accept with nil planMgr must plan-not-apply: %+v", report2)
	}

	after, err := os.ReadFile(filepath.Join(wikiDir, "skill-impact.md"))
	if err != nil {
		t.Fatalf("ledger read 2: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(after)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 ledger rows, got %d:\n%s", len(lines), after)
	}
	var entry2 selfimprove.SkillImpactEntry
	if err := json.Unmarshal([]byte(lines[1]), &entry2); err != nil {
		t.Fatalf("row 2 not JSONL: %v", err)
	}
	if !entry2.Accepted {
		t.Fatalf("second verdict must be Accepted=true: %+v", entry2)
	}
}

// TestAppendSkillImpact_NilWikiNoOpAndDiffCap checks the helper degrades with
// nil wiki and truncates oversized diffs to impactDiffMaxChars.
func TestAppendSkillImpact_NilWikiNoOpAndDiffCap(t *testing.T) {
	// Nil wiki: no panic, no-op.
	evolver, _ := newWikiTestEvolver(t, true, false, nil)
	evolver.appendSkillImpact("improve_skill", EvolutionProposal{
		Action:           ProposalRefine,
		SkillName:        "wiki-skill",
		CandidateContent: improvedWikiContent,
	}, &VerificationResult{Action: ActionAccept, Score: 0.8})

	// Oversized diff truncation.
	ws := selfimprove.NewWikiStore(t.TempDir(), slog.Default())
	WithWikiStore(ws)(evolver)
	big := strings.Repeat("x", impactDiffMaxChars+500)
	evolver.appendSkillImpact("improve_skill", EvolutionProposal{
		Action:           ProposalRefine,
		SkillName:        "wiki-skill",
		CandidateContent: big,
	}, &VerificationResult{Action: ActionReject, Score: 0.4, Reasons: []string{"r1", "r2"}})

	got, err := ws.ReadSkillImpact()
	if err != nil {
		t.Fatalf("ReadSkillImpact: %v", err)
	}
	if got == "" {
		t.Fatal("ledger row missing")
	}
	var entry selfimprove.SkillImpactEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &entry); err != nil {
		t.Fatalf("row not JSONL: %v", err)
	}
	if len(entry.Diff) != impactDiffMaxChars {
		t.Fatalf("diff must truncate to %d chars, got %d", impactDiffMaxChars, len(entry.Diff))
	}
	if entry.Accepted {
		t.Fatal("verdict must be recorded as rejected")
	}
	if entry.Reason != "r1; r2" {
		t.Fatalf("reasons must join with '; ': %q", entry.Reason)
	}
}
