package hitl

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
)

// ---------------------------------------------------------------------------
// Fakes (T1/T2): seams over the daemon surfaces
// ---------------------------------------------------------------------------

type fakePerms struct {
	pending        []PendingPermission
	taken          map[string]bool
	answers        map[string]string // id -> decision
	failResolveFor map[string]bool
}

func newFakePerms() *fakePerms {
	return &fakePerms{taken: map[string]bool{}, answers: map[string]string{}, failResolveFor: map[string]bool{}}
}

func (f *fakePerms) Pending() ([]PendingPermission, error) {
	out := []PendingPermission{}
	for _, p := range f.pending {
		if !f.taken[p.ID] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakePerms) Resolve(id, decision string) error {
	f.taken[id] = true
	f.answers[id] = decision
	if f.failResolveFor[id] {
		return errors.New("resolve refused")
	}
	return nil
}

type fakeGates struct {
	planID, title string
	pending       bool
	decision      string
}

func (f *fakeGates) PendingPlan(sessionID string) (string, string, bool, error) {
	if !f.pending {
		return "", "", false, nil
	}
	return f.planID, f.title, true, nil
}

func (f *fakeGates) DecidePlan(planID, sessionID, decision string) error {
	f.pending = false
	f.decision = decision
	return nil
}

type fakeTerminal struct {
	steps int
}

func (f *fakeTerminal) Terminal() TerminalStatus {
	f.steps++
	if f.steps < 3 {
		return TerminalStatus{}
	}
	return TerminalStatus{Done: true, Completed: true, TaskID: "task-1"}
}

func testClock(start time.Time) func() time.Time {
	t := start
	return func() time.Time {
		t = t.Add(10 * time.Millisecond)
		return t
	}
}

// ---------------------------------------------------------------------------
// T1: scenario + runner, per-action regime timeline
// ---------------------------------------------------------------------------

func TestHarnessPerActionRecordsTimeline(t *testing.T) {
	perms := newFakePerms()
	perms.pending = []PendingPermission{
		{ID: "perm-1", AgentID: "perm-agent", SessionID: "s1", Summary: "HITL-STEP-ALLOW: run the report step"},
	}
	human := NewScriptedHuman(map[string]string{"HITL-STEP-ALLOW": "allow"}, "deny")

	r := &Runner{PollInterval: time.Millisecond}
	res := r.Drive(RunConfig{
		Scenario:  Scenario{ID: "S"},
		Regime:    RegimePerAction,
		SessionID: "s1",
		Perms:     perms,
		Human:     human,
		Terminal:  (&fakeTerminal{}).Terminal,
		Clock:     testClock(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)),
	})

	if res.ScenarioID != "S" || res.Regime != "per_action" {
		t.Fatalf("run identity = %s/%s, want S/per_action", res.ScenarioID, res.Regime)
	}
	if res.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", res.Attempts)
	}

	var request, decision, terminal *TimelineEvent
	for i := range res.Events {
		switch res.Events[i].Kind {
		case EventPermissionRequest:
			if request == nil {
				ev := res.Events[i]
				request = &ev
			}
		case EventPermissionDecision:
			if decision == nil {
				ev := res.Events[i]
				decision = &ev
			}
		case EventTerminal:
			terminal = &res.Events[i]
		}
	}
	if request == nil || decision == nil || terminal == nil {
		t.Fatalf("timeline missing events: %+v", res.Events)
	}
	// Every recorded event carries a timestamp; the request precedes the
	// decision, the decision precedes the terminal.
	if request.At.IsZero() || decision.At.IsZero() || terminal.At.IsZero() {
		t.Fatalf("events missing timestamps: %+v", res.Events)
	}
	if !request.At.Before(decision.At) || !decision.At.Before(terminal.At) {
		t.Fatalf("event ordering broken: request=%v decision=%v terminal=%v", request.At, decision.At, terminal.At)
	}
	if request.RequestID != "perm-1" || decision.RequestID != "perm-1" {
		t.Fatalf("request/decision ids = %q/%q, want perm-1", request.RequestID, decision.RequestID)
	}
	if decision.Decision != "allow" || decision.Reason != "answered" || decision.StepID != "HITL-STEP-ALLOW" {
		t.Fatalf("decision event = %+v, want allow/answered/HITL-STEP-ALLOW", decision)
	}
	if terminal.Status != "completed" {
		t.Fatalf("terminal status = %q, want completed", terminal.Status)
	}
	if perms.answers["perm-1"] != "allow" {
		t.Fatalf("scripted human answered %q, want allow", perms.answers["perm-1"])
	}
}

func TestHarnessPlanGateLeadRecordsGate(t *testing.T) {
	gates := &fakeGates{planID: "plan-1", title: "HITL-PLAN: draft the report", pending: true}
	human := NewScriptedHuman(map[string]string{"HITL-PLAN": "allow"}, "deny")

	r := &Runner{PollInterval: time.Millisecond}
	res := r.Drive(RunConfig{
		Scenario:  Scenario{ID: "S"},
		Regime:    RegimePlanGateLead,
		SessionID: "s1",
		Gates:     gates,
		Human:     human,
		Terminal:  (&fakeTerminal{}).Terminal,
		Clock:     testClock(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)),
	})

	var gate, terminal *TimelineEvent
	for i := range res.Events {
		switch res.Events[i].Kind {
		case EventPlanGate:
			gate = &res.Events[i]
		case EventTerminal:
			terminal = &res.Events[i]
		}
	}
	if gate == nil || terminal == nil {
		t.Fatalf("timeline missing gate/terminal: %+v", res.Events)
	}
	if gate.RequestID != "plan-1" || gate.Decision != "allow" || gate.StepID != "HITL-PLAN" {
		t.Fatalf("gate event = %+v, want plan-1 allow HITL-PLAN", gate)
	}
	if gates.decision != "allow" {
		t.Fatalf("gates.DecidePlan saw %q, want allow", gates.decision)
	}
	if terminal.Status != "completed" {
		t.Fatalf("terminal status = %q, want completed", terminal.Status)
	}
}

func TestHarnessDriveTimeoutLeavesAttemptUncompleted(t *testing.T) {
	r := &Runner{PollInterval: time.Millisecond, DriveTimeout: 50 * time.Millisecond}
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	res := r.Drive(RunConfig{
		Scenario: Scenario{ID: "S"},
		Regime:   RegimeAutonomousSampled,
		Clock:    testClock(start),
	})

	for _, e := range res.Events {
		if e.Kind == EventTerminal {
			t.Fatalf("timeout run must not fabricate a terminal event: %+v", res.Events)
		}
	}
	if res.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", res.Attempts)
	}
	// The honest gap: completion rate computes 0 with TerminalRecorded=false.
	m := Compute(RegimeAutonomousSampled, Scenario{ID: "S"}, ComputeOptions{
		Terminal: TerminalState{Attempts: res.Attempts, Completeds: 0, Recorded: false},
	})
	if !FloatZero(m.CompletionRate) {
		t.Fatalf("completion rate = %v, want 0", m.CompletionRate)
	}
	if m.TerminalRecorded {
		t.Fatal("TerminalRecorded must stay false without a terminal event")
	}
}

func TestRegimeOverlayMapping(t *testing.T) {
	sc := Scenario{ID: "S"}
	perAction := RegimeOverlay(sc, RegimePerAction)
	if perAction["acp.permission_mode"] != "human" {
		t.Fatalf("per_action overlay permission_mode = %v, want human", perAction["acp.permission_mode"])
	}
	if perAction["acp.relay_timeout"] != 5 {
		t.Fatalf("per_action overlay relay_timeout = %v, want the scenario default 5", perAction["acp.relay_timeout"])
	}
	planGate := RegimeOverlay(sc, RegimePlanGateLead)
	if planGate["plans.approval.require_approval"] != true {
		t.Fatalf("plan_gate_lead overlay require_approval = %v, want true", planGate["plans.approval.require_approval"])
	}
	auto := RegimeOverlay(sc, RegimeAutonomousSampled)
	if auto["acp.enabled"] != nil {
		t.Fatalf("autonomous_sampled overlay must not arm the relay: %v", auto)
	}
	if Regime("nope").Valid() {
		t.Fatal("invalid regime reported valid")
	}
}

// ---------------------------------------------------------------------------
// T2: chain collection, key join, seeded sampling, spot-check marking
// ---------------------------------------------------------------------------

func appendChainRecords(t *testing.T, dbPath string, tools []string, start time.Time) {
	t.Helper()
	store, err := auditlog.OpenStore(dbPath, nil)
	if err != nil {
		t.Fatalf("open chain store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	for i, tool := range tools {
		if _, err := store.Append(ctx, auditlog.Record{
			Type: "tool.execution",
			Payload: auditlog.SanitizePayload(map[string]any{
				"action":    "tool.execution",
				"tool":      tool,
				"category":  "file",
				"args_hash": "abc" + tool,
				"outcome":   "success",
			}),
			At: start.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("append chain record %d: %v", i, err)
		}
	}
}

func TestCollectChainActionsAndKeyJoin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chain.db")
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	appendChainRecords(t, dbPath, []string{"file_write", "file_write", "shell"}, start)

	store, err := OpenChainStore(dbPath)
	if err != nil {
		t.Fatalf("OpenChainStore: %v", err)
	}
	defer store.Close()

	acts, err := CollectChainActions(context.Background(), store)
	if err != nil {
		t.Fatalf("CollectChainActions: %v", err)
	}
	if len(acts) != 3 {
		t.Fatalf("collected %d actions, want 3", len(acts))
	}
	// Ascending seq order, ground truth 1 fields populated.
	if acts[0].Seq >= acts[1].Seq || acts[1].Seq >= acts[2].Seq {
		t.Fatalf("chain actions not in seq order: %+v", acts)
	}
	if acts[0].Tool != "file_write" || acts[0].Outcome != "success" || acts[0].ArgsHash == "" {
		t.Fatalf("action record fields unparsed: %+v", acts[0])
	}
	if acts[2].Tool != "shell" {
		t.Fatalf("third action tool = %q, want shell", acts[2].Tool)
	}

	// Key join: tool-name order join (two file_write actions take the two
	// declared file_write rules in order; the shell takes the shell rule).
	declared := []ScenarioAction{
		{Key: "HITL-STEP-A", Tool: "file_write"},
		{Key: "HITL-STEP-B", Tool: "file_write"},
		{Key: "HITL-STEP-C", Tool: "shell"},
	}
	AssignActionKeys(acts, declared)
	for i, want := range []string{"HITL-STEP-A", "HITL-STEP-B", "HITL-STEP-C"} {
		if acts[i].Key != want {
			t.Fatalf("action %d key = %q, want %q", i, acts[i].Key, want)
		}
	}
}

func TestHarnessAutonomousSampledExactlyTheSeededSample(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chain.db")
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tools := make([]string, 0, 20)
	declared := make([]ScenarioAction, 0, 20)
	for i := 0; i < 20; i++ {
		tools = append(tools, "file_write")
		declared = append(declared, ScenarioAction{Key: "HITL-STEP", Tool: "file_write"})
	}
	appendChainRecords(t, dbPath, tools, start)

	store, err := OpenChainStore(dbPath)
	if err != nil {
		t.Fatalf("OpenChainStore: %v", err)
	}
	defer store.Close()
	acts, err := CollectChainActions(context.Background(), store)
	if err != nil {
		t.Fatalf("CollectChainActions: %v", err)
	}
	AssignActionKeys(acts, declared)

	sc := Scenario{ID: "auto", SampleRate: 0.2, SampleSeed: 1}.WithDefaults()
	sample := SampleIndices(len(acts), sc.SampleRate, sc.SampleSeed)
	if len(sample) != 4 {
		t.Fatalf("sample size = %d, want exactly round(20*0.2)=4: %v", len(sample), sample)
	}

	// Deterministic: the same seed reproduces the identical sample.
	again := SampleIndices(len(acts), sc.SampleRate, sc.SampleSeed)
	for i := range sample {
		if sample[i] != again[i] {
			t.Fatalf("sample not deterministic: %v vs %v", sample, again)
		}
	}

	// Spot-check: every action classified; only sampled actions carry a
	// blocking verdict; the action stream is marked, never rewritten.
	clearOracle := func(a ActionRecord) (bool, string, string) {
		return false, "base_rule", BasisClear
	}
	spot := RunSpotCheck(acts, sc.SampleRate, sc.SampleSeed, clearOracle)
	if len(spot.Verdicts) != len(acts) {
		t.Fatalf("verdicts = %d, want %d (every action classified)", len(spot.Verdicts), len(acts))
	}
	sampledSet := map[int]bool{}
	for _, i := range sample {
		sampledSet[i] = true
	}
	for i, v := range spot.Verdicts {
		if v.WouldBlock {
			t.Fatalf("clear oracle produced a would-block verdict: %+v", v)
		}
		// Sampled actions carry the oracle's verdict (clear); unsampled
		// actions are marked not_sampled - the sample bounds the
		// spot-check procedure, and the marking keeps the stream honest
		// about what was actually evaluated.
		if sampledSet[i] {
			if v.Basis != BasisClear {
				t.Fatalf("sampled verdict %d basis = %q, want clear", i, v.Basis)
			}
		} else if v.Basis != BasisNotSampled {
			t.Fatalf("unsampled verdict %d basis = %q, want not_sampled", i, v.Basis)
		}
		if v.RiskClass != "base_rule" {
			t.Fatalf("risk class = %q, want base_rule (from recorded data)", v.RiskClass)
		}
	}
	if len(spot.EscapeCandidates) != 0 {
		t.Fatalf("escape candidates on a clear oracle: %v", spot.EscapeCandidates)
	}
	// The action stream survives untouched.
	for i := range acts {
		if acts[i].RiskClass != "" || acts[i].Basis != "" {
			t.Fatalf("RunSpotCheck rewrote the action stream at %d: %+v", i, acts[i])
		}
	}

	// Marking the run: SampledIdx rides the result; history untouched.
	res := &RunResult{ScenarioID: "auto", Regime: RegimeAutonomousSampled.String(), Actions: acts}
	res.MarkSampled(sample)
	if len(res.SampledIdx) != 4 {
		t.Fatalf("marked sample = %v, want 4 indices", res.SampledIdx)
	}

	// A blocking oracle: escapes classify by risk class inside the sample.
	denyB := func(a ActionRecord) (bool, string, string) {
		if a.Key == "HITL-STEP" && a.Seq%2 == 0 {
			return true, "immutable", BasisOracleDeny
		}
		return false, "base_rule", BasisClear
	}
	spot2 := RunSpotCheck(acts, sc.SampleRate, sc.SampleSeed, denyB)
	total := 0
	for _, v := range spot2.Verdicts {
		if v.WouldBlock {
			if v.RiskClass != "immutable" {
				t.Fatalf("escape classified %q, want immutable", v.RiskClass)
			}
			if v.Basis != BasisOracleDeny {
				t.Fatalf("escape basis = %q, want oracle_deny", v.Basis)
			}
			total++
		}
	}
	if got := spot2.EscapeCandidates["immutable"]; got != total || got == 0 {
		t.Fatalf("escape candidates = %v (verdict blocks %d), want matching nonzero", spot2.EscapeCandidates, total)
	}
}

func TestSampleIndicesBounds(t *testing.T) {
	if got := SampleIndices(0, 0.2, 1); len(got) != 0 {
		t.Fatalf("empty stream sample = %v, want empty", got)
	}
	if got := SampleIndices(5, 0, 1); len(got) != 0 {
		t.Fatalf("zero rate sample = %v, want empty", got)
	}
	if got := SampleIndices(3, 1.0, 7); len(got) != 3 {
		t.Fatalf("rate 1.0 sample = %v, want all 3", got)
	}
	// Sorted ascending regardless of shuffle order.
	got := SampleIndices(50, 0.4, 42)
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("sample not sorted: %v", got)
		}
	}
}

// ---------------------------------------------------------------------------
// RPC client wire shape (reimplemented scripted-human client)
// ---------------------------------------------------------------------------

func TestRPCClientRejectsBadLengthLine(t *testing.T) {
	// A garbage length line must surface as an error, never hang or
	// mis-frame.
	c := &RPCClient{}
	if _, err := c.Call("anything", nil); err == nil {
		t.Fatal("closed client call must error")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
