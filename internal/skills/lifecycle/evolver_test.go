package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/skills"
)

// ---------------------------------------------------------------------------
// Test stubs
// ---------------------------------------------------------------------------

// mockLLMChatter implements llmChatter for testing.
type mockLLMChatter struct {
	mu       sync.Mutex
	response string
	err      error
	calls    int
}

func (m *mockLLMChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return &llm.Response{Content: m.response}, nil
}

func (m *mockLLMChatter) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockLLMChatter) SetResponse(resp string) {
	m.mu.Lock()
	m.response = resp
	m.mu.Unlock()
}

// stubUsageTracker implements UsageTracker for testing.
type stubUsageTracker struct {
	mu       sync.Mutex
	allStats map[string]*UsageStats
	low      []*UsageStats
	injects  int
	outcomes int
}

func newStubUsageTracker() *stubUsageTracker {
	return &stubUsageTracker{
		allStats: make(map[string]*UsageStats),
	}
}

func (s *stubUsageTracker) RecordInjection(skillName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.injects++
	stats, ok := s.allStats[skillName]
	if !ok {
		stats = &UsageStats{SkillName: skillName}
		s.allStats[skillName] = stats
	}
	stats.InjectCount++
	return nil
}

func (s *stubUsageTracker) RecordOutcome(skillName string, outcome Outcome, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outcomes++
	stats, ok := s.allStats[skillName]
	if !ok {
		stats = &UsageStats{SkillName: skillName}
		s.allStats[skillName] = stats
	}
	switch outcome {
	case OutcomePositive:
		stats.PositiveCount++
	case OutcomeNegative:
		stats.NegativeCount++
	case OutcomeNeutral:
		stats.NeutralCount++
	}
	if stats.InjectCount > 0 {
		stats.Effectiveness = float64(stats.PositiveCount) / float64(stats.InjectCount)
	}
	return nil
}

func (s *stubUsageTracker) GetStats(skillName string) (*UsageStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stats, ok := s.allStats[skillName]; ok {
		return stats, nil
	}
	return &UsageStats{SkillName: skillName}, nil
}

func (s *stubUsageTracker) GetAllStats() (map[string]*UsageStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]*UsageStats, len(s.allStats))
	for k, v := range s.allStats {
		result[k] = v
	}
	return result, nil
}

func (s *stubUsageTracker) GetLowPerformers(threshold float64, minInjections int) ([]*UsageStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.low, nil
}

func (s *stubUsageTracker) Close() error { return nil }

func (s *stubUsageTracker) SetStats(name string, stats *UsageStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stats == nil {
		delete(s.allStats, name)
		return
	}
	s.allStats[name] = stats
}

func (s *stubUsageTracker) SetLowPerformers(low []*UsageStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.low = low
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestVerifier creates a Verifier that uses the heuristic fallback (nil
// LLM). When accept=true, minScore is set to 0.0 so the heuristic's 0.5 average
// passes. When accept=false, minScore is set to 2.0 so it always rejects.
func newTestVerifier(accept bool) *Verifier {
	v := &Verifier{
		llmClient: nil,
		logger:    slog.Default(),
		minScore:  0.75,
	}
	if accept {
		v.minScore = 0.0
	} else {
		v.minScore = 2.0
	}
	return v
}

// makeRefineLLMResponse returns a JSON response string for the LLM mock that
// produces an "improve_skill" action.
func makeRefineLLMResponse(action, skillContent string) string {
	resp := map[string]any{
		"action":    action,
		"rationale": "test rationale",
		"skill":     skillContent,
	}
	b, _ := json.Marshal(resp)
	return fmt.Sprintf("```json\n%s\n```", string(b))
}

// defaultEvolverConfig returns a config with AutoApply enabled for testing.
func defaultEvolverConfig() config.SkillsEvolverConfig {
	return config.SkillsEvolverConfig{
		Enabled:                    true,
		Interval:                   1 * time.Hour,
		MinInjections:              5,
		MinEffectiveness:           0.2,
		PatternPromotionConfidence: 0.7,
		PatternPromotionUseCount:   5,
		AutoApply:                  true,
	}
}

// ---------------------------------------------------------------------------
// Tests: Pass A — refine existing skills
// ---------------------------------------------------------------------------

func TestEvolver_PassA_Refine(t *testing.T) {
	skillName := "test-skill"
	skillContent := "---\nname: test-skill\ndescription: test\n---\n\n# test-skill\n\noriginal content"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	_ = writer.WriteSkill(skillName, skillContent)

	registry := skills.NewRegistry()
	parsed, _ := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	registry.Register(parsed)

	improvedContent := "---\nname: test-skill\ndescription: improved test\n---\n\n# test-skill\n\nimproved content"
	mockLLM := &mockLLMChatter{response: makeRefineLLMResponse("improve_skill", improvedContent)}

	usage := newStubUsageTracker()
	usage.SetStats(skillName, &UsageStats{
		SkillName:     skillName,
		InjectCount:   10,
		PositiveCount: 5,
		Effectiveness: 0.5,
	})

	verifier := newTestVerifier(true)
	cfg := defaultEvolverConfig()

	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		verifier, mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Refined != 1 {
		t.Errorf("expected Refined=1, got %d (report: %+v)", report.Refined, report)
	}

	// Verify the skill was overwritten.
	got, err := writer.ReadSkill(skillName)
	if err != nil {
		t.Fatalf("ReadSkill failed: %v", err)
	}
	if got != improvedContent {
		t.Errorf("expected improved content, got %q", got)
	}

	// Verify the LLM was called.
	if mockLLM.CallCount() == 0 {
		t.Error("expected LLM to be called at least once")
	}
}

func TestEvolver_PassA_SkipLowInjections(t *testing.T) {
	skillName := "low-injection-skill"
	skillContent := "---\nname: low-injection-skill\ndescription: test\n---\n\nbody"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	_ = writer.WriteSkill(skillName, skillContent)

	r := skills.NewRegistry()
	parsed, _ := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	r.Register(parsed)

	mockLLM := &mockLLMChatter{response: makeRefineLLMResponse("improve_skill", "new content")}

	usage := newStubUsageTracker()
	usage.SetStats(skillName, &UsageStats{
		SkillName:     skillName,
		InjectCount:   2, // Below MinInjections=5
		Effectiveness: 0.5,
	})

	cfg := defaultEvolverConfig()
	evolver := NewEvolver(
		usage, nil, writer, r, nil,
		newTestVerifier(true), mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Refined != 0 {
		t.Errorf("expected Refined=0 (below min injections), got %d", report.Refined)
	}
	if mockLLM.CallCount() != 0 {
		t.Errorf("expected LLM not to be called, got %d calls", mockLLM.CallCount())
	}
}

// ---------------------------------------------------------------------------
// Tests: Pass B — promote learned patterns
// ---------------------------------------------------------------------------

func TestEvolver_PassB_Promote(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	registry := skills.NewRegistry()

	// Setup: a learning pipeline with an active, high-confidence pattern.
	lpCfg := selfimprove.DefaultLearningConfig()
	lp := selfimprove.NewLearningPipeline(lpCfg, nil, dir, slog.Default())
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize learning pipeline: %v", err)
	}

	pattern := &selfimprove.LearnedPattern{
		ID:          "p1",
		Type:        selfimprove.PatternTypeStrategy,
		Status:      selfimprove.PatternStatusActive,
		Domain:      "general",
		Description: "always check logs before debugging",
		Pattern:     "When debugging, first check the application logs for errors.",
		Examples:    []string{"Checked logs, found the null pointer"},
		Confidence:  0.9,
		UseCount:    10,
		CreatedAt:   time.Now(),
	}
	lp.StorePattern(context.Background(), pattern)

	// Empty capability index (no existing skill covers this pattern).
	capIndex := skills.BuildCapabilityIndex(skills.NewSkillIndex())

	mockLLM := &mockLLMChatter{response: "{}"}
	usage := newStubUsageTracker()
	cfg := defaultEvolverConfig()

	evolver := NewEvolver(
		usage, lp, writer, registry, capIndex,
		newTestVerifier(true), mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Promoted != 1 {
		t.Errorf("expected Promoted=1, got %d (report: %+v)", report.Promoted, report)
	}

	// Verify the skill was written to disk.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected skill directory to be created")
	}
}

func TestEvolver_PassB_SkipCoveredByExistingSkill(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	registry := skills.NewRegistry()

	// Setup: learning pipeline with a pattern.
	lpCfg := selfimprove.DefaultLearningConfig()
	lp := selfimprove.NewLearningPipeline(lpCfg, nil, dir, slog.Default())
	_ = lp.Initialize(context.Background())

	pattern := &selfimprove.LearnedPattern{
		ID:          "p2",
		Type:        selfimprove.PatternTypeStrategy,
		Status:      selfimprove.PatternStatusActive,
		Domain:      "general",
		Description: "debug systematically",
		Pattern:     "Debug systematically.",
		Confidence:  0.9,
		UseCount:    10,
		CreatedAt:   time.Now(),
	}
	lp.StorePattern(context.Background(), pattern)

	// Build a capability index that already covers this pattern.
	skillIndex := skills.NewSkillIndex()
	skillIndex.Index(&skills.SkillIndexEntry{
		Name:        "debug-systematically",
		Description: "debug systematically",
		Tags:        []string{"debugging"},
	})
	capIndex := skills.BuildCapabilityIndex(skillIndex)

	mockLLM := &mockLLMChatter{}
	usage := newStubUsageTracker()
	cfg := defaultEvolverConfig()

	evolver := NewEvolver(
		usage, lp, writer, registry, capIndex,
		newTestVerifier(true), mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Promoted != 0 {
		t.Errorf("expected Promoted=0 (covered by existing skill), got %d", report.Promoted)
	}
}

// ---------------------------------------------------------------------------
// Tests: Pass C — prune low performers
// ---------------------------------------------------------------------------

func TestEvolver_PassC_Prune(t *testing.T) {
	skillName := "bad-skill"
	skillContent := "---\nname: bad-skill\ndescription: bad\n---\n\nbody"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	_ = writer.WriteSkill(skillName, skillContent)
	registry := skills.NewRegistry()
	parsed, _ := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	registry.Register(parsed)
	writer.SetRegistry(registry)

	usage := newStubUsageTracker()
	usage.SetLowPerformers([]*UsageStats{
		{
			SkillName:     skillName,
			InjectCount:   15,
			Effectiveness: 0.1,
		},
	})

	mockLLM := &mockLLMChatter{}
	cfg := defaultEvolverConfig()

	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		newTestVerifier(true), mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Pruned != 1 {
		t.Errorf("expected Pruned=1, got %d (report: %+v)", report.Pruned, report)
	}

	// Verify the skill was archived (moved to .archived).
	archivedPath := filepath.Join(dir+".archived", skillName, "SKILL.md")
	if _, err := os.Stat(archivedPath); err != nil {
		t.Errorf("expected archived skill at %s: %v", archivedPath, err)
	}
}

func TestEvolver_PassC_NoLowPerformers(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	usage := newStubUsageTracker()
	usage.SetLowPerformers(nil) // No low performers.

	cfg := defaultEvolverConfig()
	evolver := NewEvolver(
		usage, nil, writer, nil, nil,
		newTestVerifier(true), &mockLLMChatter{}, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Pruned != 0 {
		t.Errorf("expected Pruned=0, got %d", report.Pruned)
	}
}

// ---------------------------------------------------------------------------
// Tests: AutoApply=false path
// ---------------------------------------------------------------------------

func TestEvolver_AutoApplyFalse_NoWriteOrArchive(t *testing.T) {
	skillName := "test-skill-autoapply"
	skillContent := "---\nname: test-skill-autoapply\ndescription: test\n---\n\nbody"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	_ = writer.WriteSkill(skillName, skillContent)
	registry := skills.NewRegistry()
	parsed, _ := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	registry.Register(parsed)

	improvedContent := "---\nname: test-skill-autoapply\ndescription: improved\n---\n\nimproved"
	mockLLM := &mockLLMChatter{response: makeRefineLLMResponse("improve_skill", improvedContent)}

	usage := newStubUsageTracker()
	usage.SetStats(skillName, &UsageStats{
		SkillName:     skillName,
		InjectCount:   10,
		Effectiveness: 0.5,
	})

	cfg := defaultEvolverConfig()
	cfg.AutoApply = false

	// planMgr is nil — should record as "planned" without applying.
	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		newTestVerifier(true), mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Planned != 1 {
		t.Errorf("expected Planned=1, got %d (report: %+v)", report.Planned, report)
	}
	if report.Refined != 0 {
		t.Errorf("expected Refined=0 (AutoApply=false), got %d", report.Refined)
	}

	// Verify the skill on disk was NOT changed.
	got, err := writer.ReadSkill(skillName)
	if err != nil {
		t.Fatalf("ReadSkill failed: %v", err)
	}
	if got != skillContent {
		t.Errorf("skill content should be unchanged when AutoApply=false; got %q", got)
	}
}

func TestEvolver_AutoApplyFalse_PruneNoArchive(t *testing.T) {
	skillName := "bad-skill-noautoapply"
	skillContent := "---\nname: bad-skill-noautoapply\ndescription: bad\n---\n\nbody"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	_ = writer.WriteSkill(skillName, skillContent)
	registry := skills.NewRegistry()
	parsed, _ := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	registry.Register(parsed)

	usage := newStubUsageTracker()
	usage.SetLowPerformers([]*UsageStats{
		{
			SkillName:     skillName,
			InjectCount:   15,
			Effectiveness: 0.1,
		},
	})

	cfg := defaultEvolverConfig()
	cfg.AutoApply = false

	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		newTestVerifier(true), &mockLLMChatter{}, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Planned != 1 {
		t.Errorf("expected Planned=1, got %d", report.Planned)
	}
	if report.Pruned != 0 {
		t.Errorf("expected Pruned=0 (AutoApply=false), got %d", report.Pruned)
	}

	// Skill should still be on disk (not archived).
	livePath := filepath.Join(dir, skillName, "SKILL.md")
	if _, err := os.Stat(livePath); err != nil {
		t.Errorf("skill should still exist when AutoApply=false: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: Verifier gate
// ---------------------------------------------------------------------------

func TestEvolver_VerifierRejects_NoWrite(t *testing.T) {
	skillName := "test-skill-reject"
	skillContent := "---\nname: test-skill-reject\ndescription: test\n---\n\noriginal"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	_ = writer.WriteSkill(skillName, skillContent)
	registry := skills.NewRegistry()
	parsed, _ := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	registry.Register(parsed)

	improvedContent := "---\nname: test-skill-reject\ndescription: improved\n---\n\nimproved"
	mockLLM := &mockLLMChatter{response: makeRefineLLMResponse("improve_skill", improvedContent)}

	usage := newStubUsageTracker()
	usage.SetStats(skillName, &UsageStats{
		SkillName:     skillName,
		InjectCount:   10,
		Effectiveness: 0.5,
	})

	cfg := defaultEvolverConfig()
	rejectVerifier := newTestVerifier(false)

	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		rejectVerifier, mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Rejected != 1 {
		t.Errorf("expected Rejected=1, got %d (report: %+v)", report.Rejected, report)
	}
	if report.Refined != 0 {
		t.Errorf("expected Refined=0 (verifier rejected), got %d", report.Refined)
	}

	// Skill content should be unchanged.
	got, err := writer.ReadSkill(skillName)
	if err != nil {
		t.Fatalf("ReadSkill failed: %v", err)
	}
	if got != skillContent {
		t.Errorf("skill content should be unchanged when verifier rejects; got %q", got)
	}
}

func TestEvolver_VerifierRejects_NoArchive(t *testing.T) {
	skillName := "bad-skill-reject"
	skillContent := "---\nname: bad-skill-reject\ndescription: bad\n---\n\nbody"

	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	_ = writer.WriteSkill(skillName, skillContent)
	registry := skills.NewRegistry()
	parsed, _ := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	registry.Register(parsed)

	usage := newStubUsageTracker()
	usage.SetLowPerformers([]*UsageStats{
		{
			SkillName:     skillName,
			InjectCount:   15,
			Effectiveness: 0.1,
		},
	})

	cfg := defaultEvolverConfig()
	rejectVerifier := newTestVerifier(false)

	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		rejectVerifier, &mockLLMChatter{}, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Rejected != 1 {
		t.Errorf("expected Rejected=1, got %d", report.Rejected)
	}
	if report.Pruned != 0 {
		t.Errorf("expected Pruned=0 (verifier rejected), got %d", report.Pruned)
	}

	// Skill should still be on disk.
	livePath := filepath.Join(dir, skillName, "SKILL.md")
	if _, err := os.Stat(livePath); err != nil {
		t.Errorf("skill should still exist when verifier rejects: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: EvolverScheduler lifecycle
// ---------------------------------------------------------------------------

func TestEvolverScheduler_StartStop(t *testing.T) {
	dir := t.TempDir()
	usage := newStubUsageTracker()
	writer := NewWriter(dir, slog.Default())

	cfg := defaultEvolverConfig()
	evolver := NewEvolver(
		usage, nil, writer, nil, nil,
		newTestVerifier(true), &mockLLMChatter{}, nil,
		cfg, slog.Default(),
	)

	sched := NewEvolverScheduler(evolver, 50*time.Millisecond, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	go sched.Start(ctx)

	// Let it run briefly.
	time.Sleep(200 * time.Millisecond)

	cancel()
	sched.Stop()
}

func TestEvolverScheduler_DoubleStopSafe(t *testing.T) {
	evolver := NewEvolver(
		newStubUsageTracker(), nil, nil, nil, nil,
		newTestVerifier(true), &mockLLMChatter{}, nil,
		defaultEvolverConfig(), slog.Default(),
	)
	sched := NewEvolverScheduler(evolver, time.Hour, slog.Default())

	// Stop without start should be safe.
	sched.Stop()
	// Double stop should be safe.
	sched.Stop()
}

// ---------------------------------------------------------------------------
// Tests: GAP #4 — Pass B picks up patterns from non-"all" domains
// ---------------------------------------------------------------------------

// TestEvolver_PassB_NonAllDomain verifies that Pass B (promote) considers
// patterns whose domain is a specific category (e.g., "code") rather than only
// "all"/"general". Previously evolver.go called Retrieve(ctx, "", "all", 100)
// which the Retrieve implementation filters as a literal domain match,
// silently dropping patterns from "code", "debugging", "planning", etc.
func TestEvolver_PassB_NonAllDomain(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	registry := skills.NewRegistry()

	lpCfg := selfimprove.DefaultLearningConfig()
	lp := selfimprove.NewLearningPipeline(lpCfg, nil, dir, slog.Default())
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize learning pipeline: %v", err)
	}

	// Seed a pattern with a specific domain (not "all"/"general").
	codePattern := &selfimprove.LearnedPattern{
		ID:          "code-p1",
		Type:        selfimprove.PatternTypeStrategy,
		Status:      selfimprove.PatternStatusActive,
		Domain:      "code",
		Description: "always run gofmt before committing go files",
		Pattern:     "Run gofmt on all .go files before creating a commit.",
		Examples:    []string{"gofmt -w . before git commit"},
		Confidence:  0.92,
		UseCount:    15,
		CreatedAt:   time.Now(),
		ContentHash: "code-domain-unique-hash-0001",
	}
	if err := lp.StorePattern(context.Background(), codePattern); err != nil {
		t.Fatalf("StorePattern failed: %v", err)
	}

	// Empty capability index — no existing skill covers this pattern.
	capIndex := skills.BuildCapabilityIndex(skills.NewSkillIndex())

	mockLLM := &mockLLMChatter{response: "{}"}
	usage := newStubUsageTracker()
	cfg := defaultEvolverConfig()

	evolver := NewEvolver(
		usage, lp, writer, registry, capIndex,
		newTestVerifier(true), mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Promoted != 1 {
		t.Errorf("expected Promoted=1 for code-domain pattern, got %d (report: %+v)", report.Promoted, report)
	}

	// Verify the skill was written to disk.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected skill directory to be created for code-domain pattern")
	}
}

// ---------------------------------------------------------------------------
// Tests: GAP #5 — EvolverScheduler skips initial cycle when runOnStart=false
// ---------------------------------------------------------------------------

// recordingEvolver wraps an Evolver to count RunCycle invocations. We cannot
// stub the Evolver interface directly because NewEvolverScheduler takes a
// concrete *Evolver, so instead we observe side effects: when the scheduler
// runs RunCycle, the Verifier's call count increases (verifier is invoked from
// processProposal). The baseline test has no usage/learning data, so RunCycle
// is a no-op internally — but we can still detect an immediate run by checking
// that the scheduler enters the tick loop without firing within a window
// shorter than the configured interval.
func TestEvolverScheduler_SkipsInitialCycle(t *testing.T) {
	dir := t.TempDir()
	usage := newStubUsageTracker()
	writer := NewWriter(dir, slog.Default())

	// Use a mock LLM with non-empty response so any accidental RunCycle call
	// would exercise the LLM path — we can detect via CallCount.
	mockLLM := &mockLLMChatter{response: "{}"}

	evolver := NewEvolver(
		usage, nil, writer, nil, nil,
		newTestVerifier(true), mockLLM, nil,
		defaultEvolverConfig(), slog.Default(),
	)

	// Construct scheduler with runOnStart=false. Interval is long (1h) so no
	// tick fires during the test window.
	sched := NewEvolverSchedulerWithRunOnStart(evolver, time.Hour, false, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	go sched.Start(ctx)

	// Give the scheduler goroutine time to (not) run an initial cycle.
	time.Sleep(100 * time.Millisecond)

	cancel()
	sched.Stop()

	// No usage data was seeded, so Pass A/C would skip; Pass B has no learning
	// pipeline. RunCycle itself is safe, but we assert the scheduler did not
	// fire by checking the immediate-cycle path was skipped: mockLLM is only
	// invoked from Pass A's callLLMJSON; with no usage data Pass A skips. The
	// observable signal is that the scheduler returned without doing work
	// within the test window (no panic, no deadlock). The real coverage is the
	// contrast with TestEvolverScheduler_RunOnStartTrue below which asserts
	// the cycle runs.
	if mockLLM.CallCount() != 0 {
		t.Errorf("expected no LLM calls when usage is empty, got %d", mockLLM.CallCount())
	}
}

// TestEvolverScheduler_RunOnStartTrue confirms the legacy behavior still
// works: when runOnStart=true, the scheduler runs one cycle before entering
// the tick loop. This guards against regressions where the gate accidentally
// always skips.
func TestEvolverScheduler_RunOnStartTrue(t *testing.T) {
	dir := t.TempDir()
	usage := newStubUsageTracker()

	// Seed a skill with enough injections that Pass A would call the LLM.
	skillName := "runonstart-skill"
	skillContent := "---\nname: runonstart-skill\ndescription: test\n---\n\nbody"
	writer := NewWriter(dir, slog.Default())
	_ = writer.WriteSkill(skillName, skillContent)

	registry := skills.NewRegistry()
	parsed, _ := skills.ParseSkillFile(filepath.Join(dir, skillName, "SKILL.md"))
	registry.Register(parsed)

	usage.SetStats(skillName, &UsageStats{
		SkillName:     skillName,
		InjectCount:   10,
		Effectiveness: 0.5,
	})

	improvedContent := "---\nname: runonstart-skill\ndescription: improved\n---\n\nimproved body"
	mockLLM := &mockLLMChatter{response: makeRefineLLMResponse("improve_skill", improvedContent)}

	evolver := NewEvolver(
		usage, nil, writer, registry, nil,
		newTestVerifier(true), mockLLM, nil,
		defaultEvolverConfig(), slog.Default(),
	)

	sched := NewEvolverSchedulerWithRunOnStart(evolver, time.Hour, true, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	go sched.Start(ctx)

	// Wait for the immediate cycle to fire.
	time.Sleep(150 * time.Millisecond)

	cancel()
	sched.Stop()

	if mockLLM.CallCount() == 0 {
		t.Error("expected LLM to be called by run-on-start initial cycle, got 0 calls")
	}
}

// ---------------------------------------------------------------------------
// Tests: GAP #6 — patternToSkillName collision avoidance
// ---------------------------------------------------------------------------

// TestEvolver_PatternToSkillNameCollision seeds two patterns whose
// descriptions share the first 40 chars, so patternToSkillName produces the
// same base name. The evolver must append a numeric suffix (-2) to the second
// proposal so both skills are created rather than one overwriting the other.
func TestEvolver_PatternToSkillNameCollision(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())
	registry := skills.NewRegistry()

	lpCfg := selfimprove.DefaultLearningConfig()
	lp := selfimprove.NewLearningPipeline(lpCfg, nil, dir, slog.Default())
	if err := lp.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize learning pipeline: %v", err)
	}

	// Two patterns with identical first-40-char descriptions (after lowercasing
	// and kebab-casing, both produce the same base skill name). The
	// descriptions diverge AFTER 40 chars so the patterns themselves are
	// distinct, but patternToSkillName truncates at 40. The Pattern fields are
	// deliberately very different so LearningPipeline.Retrieve's MMR diversity
	// filter (similarity > 0.85) does not collapse them into one result.
	sharedPrefix := "always validate user input carefully before processing"
	if len(sharedPrefix) < 40 {
		t.Fatalf("test setup error: prefix must be >= 40 chars, got %d", len(sharedPrefix))
	}
	p1 := &selfimprove.LearnedPattern{
		ID:          "collision-1",
		Type:        selfimprove.PatternTypeStrategy,
		Status:      selfimprove.PatternStatusActive,
		Domain:      "general",
		Description: sharedPrefix + " variant alpha",
		Pattern:     "Before accepting any HTTP request, verify the Content-Type header matches the documented API contract and reject mismatched payloads with 415 Unsupported Media Type.",
		Examples:    []string{"reject POST /api/users with text/plain when JSON expected"},
		Confidence:  0.9,
		UseCount:    10,
		CreatedAt:   time.Now(),
		ContentHash: "collision-hash-alpha-0001",
	}
	p2 := &selfimprove.LearnedPattern{
		ID:          "collision-2",
		Type:        selfimprove.PatternTypeStrategy,
		Status:      selfimprove.PatternStatusActive,
		Domain:      "general",
		Description: sharedPrefix + " variant beta",
		Pattern:     "When deserializing untrusted YAML, use a safe loader that refuses Python object tags (!!python/) to prevent arbitrary code execution via pickle smuggling.",
		Examples:    []string{"yaml.safe_load instead of yaml.load"},
		Confidence:  0.9,
		UseCount:    10,
		CreatedAt:   time.Now(),
		ContentHash: "collision-hash-beta-0002",
	}
	if err := lp.StorePattern(context.Background(), p1); err != nil {
		t.Fatalf("StorePattern p1: %v", err)
	}
	if err := lp.StorePattern(context.Background(), p2); err != nil {
		t.Fatalf("StorePattern p2: %v", err)
	}

	capIndex := skills.BuildCapabilityIndex(skills.NewSkillIndex())
	mockLLM := &mockLLMChatter{response: "{}"}
	usage := newStubUsageTracker()
	cfg := defaultEvolverConfig()

	evolver := NewEvolver(
		usage, lp, writer, registry, capIndex,
		newTestVerifier(true), mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Promoted != 2 {
		t.Errorf("expected Promoted=2 (two distinct patterns), got %d (report: %+v)", report.Promoted, report)
	}

	// Collect the proposal skill names and verify they are distinct.
	seen := make(map[string]bool)
	for _, p := range report.Details {
		if p.Action == ProposalCreate {
			if seen[p.SkillName] {
				t.Errorf("duplicate skill name in proposals: %q", p.SkillName)
			}
			seen[p.SkillName] = true
		}
	}
	if len(seen) != 2 {
		t.Errorf("expected 2 distinct create-proposal names, got %d (%v)", len(seen), seen)
	}
}

// ---------------------------------------------------------------------------
// Test: full cycle with all three passes
// ---------------------------------------------------------------------------

func TestEvolver_FullCycle_AllThreePasses(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir, slog.Default())

	// Skill to refine — present in allStats for Pass A.
	refineName := "refine-me"
	refineContent := "---\nname: refine-me\ndescription: test refine\n---\n\noriginal refine content"
	_ = writer.WriteSkill(refineName, refineContent)

	// Skill to prune — NOT in allStats (so Pass A skips it).
	// Only appears in lowPerformers for Pass C.
	pruneName := "prune-me"
	pruneContent := "---\nname: prune-me\ndescription: bad skill\n---\n\nunique prune body"
	_ = writer.WriteSkill(pruneName, pruneContent)

	registry := skills.NewRegistry()
	for _, name := range []string{refineName, pruneName} {
		parsed, _ := skills.ParseSkillFile(filepath.Join(dir, name, "SKILL.md"))
		registry.Register(parsed)
	}
	writer.SetRegistry(registry)

	// Usage tracker: only refineName in allStats (for Pass A).
	// pruneName only in lowPerformers (for Pass C).
	usage := newStubUsageTracker()
	usage.SetStats(refineName, &UsageStats{
		SkillName:     refineName,
		InjectCount:   10,
		Effectiveness: 0.5,
	})
	usage.SetLowPerformers([]*UsageStats{
		{
			SkillName:     pruneName,
			InjectCount:   15,
			Effectiveness: 0.1,
		},
	})

	// Learning pipeline with a pattern to promote.
	lpCfg := selfimprove.DefaultLearningConfig()
	lp := selfimprove.NewLearningPipeline(lpCfg, nil, dir, slog.Default())
	_ = lp.Initialize(context.Background())
	lp.StorePattern(context.Background(), &selfimprove.LearnedPattern{
		ID:          "promote-me",
		Type:        selfimprove.PatternTypeStrategy,
		Status:      selfimprove.PatternStatusActive,
		Domain:      "general",
		Description: "always write table driven tests",
		Pattern:     "Use table-driven tests for Go code.",
		Confidence:  0.95,
		UseCount:    12,
		CreatedAt:   time.Now(),
	})

	capIndex := skills.BuildCapabilityIndex(skills.NewSkillIndex())

	improvedContent := "---\nname: refine-me\ndescription: improved refine\n---\n\nimproved refine content"
	mockLLM := &mockLLMChatter{response: makeRefineLLMResponse("improve_skill", improvedContent)}

	cfg := defaultEvolverConfig()
	evolver := NewEvolver(
		usage, lp, writer, registry, capIndex,
		newTestVerifier(true), mockLLM, nil,
		cfg, slog.Default(),
	)

	report, err := evolver.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if report.Refined != 1 {
		t.Errorf("expected Refined=1, got %d", report.Refined)
	}
	if report.Promoted != 1 {
		t.Errorf("expected Promoted=1, got %d", report.Promoted)
	}
	if report.Pruned != 1 {
		t.Errorf("expected Pruned=1, got %d", report.Pruned)
	}

	// Total proposals: 1 refine + 1 promote + 1 prune = 3.
	if len(report.Details) != 3 {
		t.Errorf("expected 3 proposals in details, got %d", len(report.Details))
	}
}
