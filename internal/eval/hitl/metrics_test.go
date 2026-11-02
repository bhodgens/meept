package hitl

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/security"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// metricsFixture is the leaf's canonical worked example shape: a decision
// stream with 3 interventions, 1 of which changed the outcome.
func metricsFixture() (DecisionStream, CounterfactualResult) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ds := DecisionStream{Events: []TimelineEvent{
		{At: base, Kind: EventPermissionRequest, RequestID: "p1", Summary: "HITL-CHANGED: write the artifact"},
		{At: base.Add(time.Second), Kind: EventPermissionDecision, RequestID: "p1", Decision: "allow", Reason: "answered", StepID: "HITL-CHANGED"},
		{At: base.Add(2 * time.Second), Kind: EventPermissionRequest, RequestID: "p2", Summary: "HITL-RUBBER: write the boilerplate"},
		{At: base.Add(3 * time.Second), Kind: EventPermissionDecision, RequestID: "p2", Decision: "allow", Reason: "answered", StepID: "HITL-RUBBER"},
		{At: base.Add(4 * time.Second), Kind: EventPermissionRequest, RequestID: "p3", Summary: "HITL-TIMEOUT: nobody home"},
		{At: base.Add(9 * time.Second), Kind: EventPermissionDecision, RequestID: "p3", Decision: "deny", Reason: "timeout", StepID: "HITL-TIMEOUT"},
		// Plan gates are interventions too.
		{At: base.Add(10 * time.Second), Kind: EventPlanGate, RequestID: "plan-1", Decision: "allow", Reason: "answered", StepID: "HITL-PLAN"},
	}}
	cf := CounterfactualResult{ChangedStepIDs: []string{"HITL-CHANGED"}}
	return ds, cf
}

func newTestEngine(t *testing.T) *security.Engine {
	t.Helper()
	cfg := &config.SecurityConfig{
		BlockFinancial:              false,
		RequireConfirmationHigh:     false,
		RequireConfirmationCritical: false,
	}
	engine, err := security.NewEngine(filepath.Join(t.TempDir(), "audit.db"), cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

// ---------------------------------------------------------------------------
// T3: C7 field computation
// ---------------------------------------------------------------------------

func TestMetricsCompletionWallClockInterrupt(t *testing.T) {
	ds, cf := metricsFixture()
	m := Compute(RegimePerAction, Scenario{ID: "S"}, ComputeOptions{
		Decisions:      ds,
		Terminal:       TerminalState{Attempts: 2, Completeds: 1, Recorded: true},
		WallClock:      15 * time.Second,
		Counterfactual: &cf,
	})

	if !FloatEq(m.CompletionRate, 0.5) {
		t.Errorf("completion_rate = %v, want 0.5", m.CompletionRate)
	}
	if !FloatEq(m.WallClockSeconds, 15) {
		t.Errorf("wall_clock_seconds = %v, want 15", m.WallClockSeconds)
	}
	// 3 permission decisions + 1 plan gate = 4 interrupts.
	if m.InterruptCount != 4 {
		t.Errorf("interrupt_count = %d, want 4", m.InterruptCount)
	}
	if m.InterruptsTotal != 4 || m.InterruptsChangedOutcome != 1 || m.TimedOutInterrupts != 1 {
		t.Errorf("precision basis = %d/%d (timeout %d), want 1/4 (timeout 1)",
			m.InterruptsChangedOutcome, m.InterruptsTotal, m.TimedOutInterrupts)
	}
	if m.Regime != "per_action" || m.ScenarioID != "S" {
		t.Errorf("identity = %s/%s, want per_action/S", m.Regime, m.ScenarioID)
	}
}

func TestMetricsInterventionPrecision(t *testing.T) {
	ds, cf := metricsFixture()
	m := Compute(RegimePerAction, Scenario{ID: "S"}, ComputeOptions{
		Decisions:      ds,
		Terminal:       TerminalState{Attempts: 1, Completeds: 1, Recorded: true},
		Counterfactual: &cf,
	})
	// Only HITL-CHANGED diverged; the timeout is denominator-only.
	if !FloatEq(m.InterventionPrecision, 1.0/4.0) {
		t.Fatalf("precision = %v, want 1/4", m.InterventionPrecision)
	}
}

func TestMetricsPrecisionNilCounterfactualStaysHonest(t *testing.T) {
	ds, _ := metricsFixture()
	m := Compute(RegimePerAction, Scenario{ID: "S"}, ComputeOptions{
		Decisions: ds,
		Terminal:  TerminalState{Attempts: 1, Completeds: 1, Recorded: true},
	})
	if m.InterruptsTotal != 4 {
		t.Fatalf("interrupts total = %d, want 4", m.InterruptsTotal)
	}
	if !FloatZero(m.InterventionPrecision) {
		t.Fatalf("precision without counterfactual evidence = %v, want 0", m.InterventionPrecision)
	}
}

func TestMetricsZeroInterventionsIsNotFakePrecision(t *testing.T) {
	m := Compute(RegimeAutonomousSampled, Scenario{ID: "auto"}, ComputeOptions{
		Terminal: TerminalState{Attempts: 1, Completeds: 1, Recorded: true},
	})
	if m.InterruptCount != 0 {
		t.Fatalf("interrupt_count = %d, want 0", m.InterruptCount)
	}
	if !FloatZero(m.InterventionPrecision) {
		t.Fatalf("precision = %v, want 0 (the renderer explains the 0-denominator case)", m.InterventionPrecision)
	}
}

func TestMetricsUnrecordedTerminalCountsNotCompleted(t *testing.T) {
	m := Compute(RegimePerAction, Scenario{ID: "S"}, ComputeOptions{
		Terminal: TerminalState{Attempts: 1, Completeds: 0, Recorded: false},
	})
	if !FloatZero(m.CompletionRate) {
		t.Fatalf("completion_rate = %v, want 0 for an unrecorded terminal", m.CompletionRate)
	}
	if m.TerminalRecorded {
		t.Fatal("TerminalRecorded must be false")
	}
}

func TestMetricsJSONRoundTripMatchesC7Tags(t *testing.T) {
	ds, cf := metricsFixture()
	m := Compute(RegimePlanGateLead, Scenario{ID: "S"}, ComputeOptions{
		Decisions:      ds,
		Terminal:       TerminalState{Attempts: 1, Completeds: 1, Recorded: true},
		WallClock:      2 * time.Second,
		Counterfactual: &cf,
	})
	report := HitlRegimeReport{Scenarios: []RegimeMetrics{m}, GeneratedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	b, err := RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var back HitlRegimeReport
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back.Scenarios) != 1 {
		t.Fatalf("decoded %d scenarios, want 1", len(back.Scenarios))
	}
	got := back.Scenarios[0]
	if got.Regime != m.Regime || got.ScenarioID != m.ScenarioID {
		t.Fatalf("identity round-trip broken: %+v", got)
	}
	if !FloatEq(got.CompletionRate, m.CompletionRate) || !FloatEq(got.InterventionPrecision, m.InterventionPrecision) {
		t.Fatalf("float round-trip broken: %+v vs %+v", got, m)
	}
	if got.InterruptCount != m.InterruptCount {
		t.Fatalf("interrupt round-trip broken: %d vs %d", got.InterruptCount, m.InterruptCount)
	}
}

// ---------------------------------------------------------------------------
// T3: escape rate from ground truth (chain + RuleSource + reference deny)
// ---------------------------------------------------------------------------

func TestMetricsEscapeRatePerRiskClass(t *testing.T) {
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	acts := []ActionRecord{
		{Seq: 1, At: start, Tool: "file_write", Outcome: "success", Key: "HITL-ESCAPE", RiskClass: "immutable", Basis: BasisOracleDeny},
		{Seq: 2, At: start.Add(time.Second), Tool: "file_write", Outcome: "success", Key: "HITL-IMM2", RiskClass: "immutable", Basis: BasisClear},
		{Seq: 3, At: start.Add(2 * time.Second), Tool: "shell", Outcome: "success", Key: "HITL-OK2", RiskClass: "command_pattern", Basis: BasisClear},
		{Seq: 4, At: start.Add(3 * time.Second), Tool: "shell", Outcome: "success", Key: "HITL-ESCAPE2", RiskClass: "command_pattern", Basis: BasisOracleDeny},
	}
	// Sample covers indices 0 and 3 (the two escapes). The numerator is
	// bounded by the seeded sample; the denominator is the CLASS TOTAL
	// (2 immutable actions, 2 command_pattern actions).
	spot := &SpotCheckResult{
		Rate:             0.5,
		Seed:             1,
		Sampled:          []int{0, 3},
		EscapeCandidates: map[string]int{"immutable": 1, "command_pattern": 1},
		Verdicts: []SpotCheckVerdict{
			{Index: 0, WouldBlock: true, RiskClass: "immutable", Basis: BasisOracleDeny},
			{Index: 1, WouldBlock: false, RiskClass: "immutable", Basis: BasisNotSampled},
			{Index: 2, WouldBlock: false, RiskClass: "command_pattern", Basis: BasisNotSampled},
			{Index: 3, WouldBlock: true, RiskClass: "command_pattern", Basis: BasisOracleDeny},
		},
	}
	m := Compute(RegimeAutonomousSampled, Scenario{ID: "auto"}, ComputeOptions{
		Terminal: TerminalState{Attempts: 1, Completeds: 1, Recorded: true},
		Actions:  acts,
		Sample:   spot,
	})

	if !FloatEq(m.EscapeRate["immutable"], 1.0/2.0) {
		t.Errorf("escape_rate[immutable] = %v, want 0.5 (1 blocked in class / 2 in class)", m.EscapeRate["immutable"])
	}
	if !FloatEq(m.EscapeRate["command_pattern"], 1.0/2.0) {
		t.Errorf("escape_rate[command_pattern] = %v, want 0.5", m.EscapeRate["command_pattern"])
	}
	// base_rule has classified actions but no sampled block: 0.0 is the
	// measured value (no escapes in the class), not a fabrication.
	if !FloatEq(m.EscapeRate["base_rule"], 0) {
		t.Errorf("escape_rate[base_rule] = %v, want 0 (classified class, zero sampled blocks)", m.EscapeRate["base_rule"])
	}
	if m.AutonomousActions != 4 {
		t.Errorf("autonomous_actions = %d, want 4", m.AutonomousActions)
	}
}

func TestMetricsEscapeRateAlwaysAllowSplit(t *testing.T) {
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	acts := []ActionRecord{
		{Seq: 1, At: start, Tool: "shell", Outcome: "success", Key: "K1", RiskClass: "immutable"},
		{Seq: 2, At: start.Add(time.Second), Tool: "shell", Outcome: "success", Key: "K2", RiskClass: "immutable"},
	}
	// Index 0 was an "always"-allow attribution; its escape goes to the
	// including map but NOT the excluding map (R0 B.3).
	spot := &SpotCheckResult{
		Rate:             1.0,
		Seed:             1,
		Sampled:          []int{0, 1},
		EscapeCandidates: map[string]int{"immutable": 2},
		Verdicts: []SpotCheckVerdict{
			{Index: 0, WouldBlock: true, RiskClass: "immutable", Basis: BasisOracleDeny},
			{Index: 1, WouldBlock: true, RiskClass: "immutable", Basis: BasisOracleDeny},
		},
	}
	m := Compute(RegimeAutonomousSampled, Scenario{ID: "auto"}, ComputeOptions{
		Terminal:         TerminalState{Attempts: 1, Completeds: 1, Recorded: true},
		Actions:          acts,
		Sample:           spot,
		AlwaysAttributed: map[int]bool{0: true},
	})
	if !FloatEq(m.EscapeRate["immutable"], 1.0) {
		t.Errorf("escape_rate = %v, want 1.0", m.EscapeRate["immutable"])
	}
	if !FloatEq(m.EscapeRateExcludesAlways["immutable"], 1.0) {
		t.Errorf("escape_rate excludes-always = %v, want 1.0 (1 escape / 1 non-always action)", m.EscapeRateExcludesAlways["immutable"])
	}
	if m.EscapeRateAlwaysActions != 1 {
		t.Errorf("always-attributed actions = %d, want 1", m.EscapeRateAlwaysActions)
	}
}

func TestMetricsEmptyEscapeFeedsStayEmpty(t *testing.T) {
	m := Compute(RegimePerAction, Scenario{ID: "S"}, ComputeOptions{
		Terminal: TerminalState{Attempts: 1, Completeds: 1, Recorded: true},
	})
	if len(m.EscapeRate) != 0 {
		t.Fatalf("escape rate without an action stream = %v, want empty (no fabricated rows)", m.EscapeRate)
	}
	if m.EscapeOracle == "" || m.MetricProvenance == "" {
		t.Fatalf("oracle/provenance must stamp every row: %+v", m)
	}
}

// ---------------------------------------------------------------------------
// T3: the oracle - dedicated security engine deny replay
// ---------------------------------------------------------------------------

func TestScenarioOracleReplayDenyIsEscapeConfirmationEscalationIsNot(t *testing.T) {
	engineDBPath := filepath.Join(t.TempDir(), "audit.db")
	cfg := &config.SecurityConfig{
		BlockFinancial:              false,
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}
	engine, err := security.NewEngine(engineDBPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	// The seeded immutable table hard-denies "rm -rf /" (engine replay:
	// decision RuleSource = immutable).
	d := engine.Check(security.ActionShellExecute, "shell", map[string]string{"command": "rm -rf /"}, "")
	if d.Allowed || d.RequiresConfirmation {
		t.Fatalf("seed table must hard-deny rm -rf /: %+v", d)
	}
	oracle := NewScenarioOracle(engine, nil)

	acts := []ActionRecord{
		{Seq: 1, Tool: "shell", Key: "rm -rf /", RiskClass: ""},
	}
	oracle.ClassifyActions(acts)
	if acts[0].RiskClass != security.RuleSourceImmutable {
		t.Fatalf("risk class = %q, want %q (RuleSource from the replay decision)", acts[0].RiskClass, security.RuleSourceImmutable)
	}

	block, class, basis := oracle.Classify(acts[0])
	if !block || basis != BasisOracleDeny {
		t.Fatalf("oracle verdict = %v/%s, want block/oracle_deny", block, basis)
	}
	if class != security.RuleSourceImmutable {
		t.Fatalf("class = %q, want immutable", class)
	}

	// Replay isolation: the oracle's decisions landed in ITS OWN
	// decision_log - the engine instance was constructed against a
	// sandbox db path, so nothing touched the daemon's audit surfaces.
	// The isolation property is structural (NewScenarioOracle only sees
	// the instance it is handed); pin it by counting the sandbox file.
	if got := countDecisionLogRows(t, engineDBPath); got == 0 {
		t.Fatal("oracle replay appended nothing to its own decision_log")
	}
}

// countDecisionLogRows opens dbPath and counts decision_log rows.
func countDecisionLogRows(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM decision_log").Scan(&n); err != nil {
		t.Fatalf("count decision_log in %s: %v", dbPath, err)
	}
	return n
}

func TestScenarioOracleReferenceDenyRule(t *testing.T) {
	oracle := NewScenarioOracle(nil, map[string]bool{"HITL-DENIED": true})
	block, class, basis := oracle.Classify(ActionRecord{Seq: 1, Tool: "file_write", Key: "HITL-DENIED", RiskClass: "base_rule"})
	if !block || basis != BasisReferenceDeny {
		t.Fatalf("reference deny = %v/%s, want block/reference_deny (R0 spot-check rule)", block, basis)
	}
	if class != "base_rule" {
		t.Fatalf("class = %q, want the recorded risk class", class)
	}
	// An action with no key and no engine classifies clear, never blocked.
	block, _, basis = oracle.Classify(ActionRecord{Seq: 2, Tool: "file_write", Key: "", RiskClass: "base_rule"})
	if block || basis != BasisClear {
		t.Fatalf("unkeyed action = %v/%s, want clear (evidenced, not judged)", block, basis)
	}
}

func TestReferenceDeniedSetFromStream(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ds := DecisionStream{Events: []TimelineEvent{
		{At: base, Kind: EventPermissionDecision, Decision: "deny", Reason: "answered", StepID: "HITL-D1"},
		{At: base.Add(time.Second), Kind: EventPermissionDecision, Decision: "allow", Reason: "answered", StepID: "HITL-A1"},
		{At: base.Add(2 * time.Second), Kind: EventPermissionDecision, Decision: "deny", Reason: "timeout", StepID: "HITL-T1"},
	}}
	denied := ReferenceDeniedSet(ds)
	if !denied["HITL-D1"] || denied["HITL-A1"] {
		t.Fatalf("reference denied set = %v, want only the answered deny", denied)
	}
	// A timeout deny is fail-closed machinery, not a human judgment: it
	// does NOT enter the reference set.
	if denied["HITL-T1"] {
		t.Fatal("timeout deny must not count as a reference denial")
	}
}

// ---------------------------------------------------------------------------
// Ground truth 1: chain integrity precondition
// ---------------------------------------------------------------------------

func TestChainIntegrityVerification(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chain.db")
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	appendChainRecords(t, dbPath, []string{"file_write", "shell"}, start)

	store, err := OpenChainStore(dbPath)
	if err != nil {
		t.Fatalf("OpenChainStore: %v", err)
	}
	defer store.Close()

	res, err := VerifyChainIsIntact(context.Background(), store.ChainDB())
	if err != nil {
		t.Fatalf("VerifyChainIsIntact: %v", err)
	}
	if !res.OK {
		t.Fatalf("chain reported broken: %+v", res)
	}
	if res.Records != 2 {
		t.Fatalf("records = %d, want 2", res.Records)
	}
}

// ---------------------------------------------------------------------------
// Determinism: identical inputs, identical metrics
// ---------------------------------------------------------------------------

func TestMetricsDeterministicAcrossRuns(t *testing.T) {
	build := func() RegimeMetrics {
		ds, cf := metricsFixture()
		start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		acts := []ActionRecord{
			{Seq: 1, At: start, Tool: "shell", Key: "K1", RiskClass: "immutable"},
			{Seq: 2, At: start.Add(time.Second), Tool: "shell", Key: "K2", RiskClass: "base_rule"},
		}
		spot := RunSpotCheck(acts, 0.5, 9, func(a ActionRecord) (bool, string, string) {
			return a.Key == "K1", a.RiskClass, BasisOracleDeny
		})
		return Compute(RegimeAutonomousSampled, Scenario{ID: "det"}, ComputeOptions{
			Decisions:      ds,
			Terminal:       TerminalState{Attempts: 2, Completeds: 1, Recorded: true},
			WallClock:      3 * time.Second,
			Actions:        acts,
			Sample:         spot,
			Counterfactual: &cf,
		})
	}
	a, b := build(), build()
	if !FloatEq(a.CompletionRate, b.CompletionRate) ||
		!FloatEq(a.InterventionPrecision, b.InterventionPrecision) ||
		a.InterruptCount != b.InterruptCount {
		t.Fatalf("metrics not deterministic: %+v vs %+v", a, b)
	}
	if !FloatEq(a.EscapeRate["immutable"], b.EscapeRate["immutable"]) {
		t.Fatalf("escape rate not deterministic: %v vs %v", a.EscapeRate, b.EscapeRate)
	}
}
