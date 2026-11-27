//go:build e2e

// producer-lane-01 (L6, owner ruling 2026-11-27): the non-step review
// producers emit verdicts to the collector lane WITH the executing-agent
// identity. Three real-path assertions in one hermetic rig:
//
//	(pin a) pair_manager: a converged pair round publishes through the
//	   REAL ReviewManager seam -> the REAL collector bus subscription ->
//	   a model_review_verdicts row with agent_id set to the ACTOR agent
//	   and judge identities from the REVIEWER, not the step reviewer.
//	(pin b) verification_hook: a scripted adversarial PASS publishes a
//	   row whose judge_agent_id names the ephemeral verifier role.
//	(pin c) step lane: a step row with a known AgentID stores agent_id
//	   through the real collector, with the blame join still resolving
//	   reviewed_model_id from llm_calls (L7 is_final posture included).
//
// Rig posture follows the suite's existing shape (no full daemon): real
// bus + collector + ReviewManager + PairManager + metrics.Store, a
// scripted Chatter for the pair agents (the graceful-errors gate-seam
// pattern), direct seeds where the path is one constructor deep.
package modelrating

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/task"
)

// scriptedChatter answers every call with a canned completion (the
// plan-critic suite's chatter shape, one shared response).
type scriptedChatter struct {
	response string
	modelID  string
}

func (c *scriptedChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: c.response, FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}}, nil
}

func (c *scriptedChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *scriptedChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: c.modelID}
}

// waitForVerdicts polls the store until FinalVerdictsForStep returns rows
// (the collector drains its subscription on a goroutine).
func waitForVerdicts(t *testing.T, store *metrics.Store, stepID string, want int) []metrics.ReviewVerdictRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := store.FinalVerdictsForStep(stepID)
		if err == nil && len(rows) >= want {
			return rows
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("collector never persisted %d verdict rows for step %q", want, stepID)
	return nil
}

// rowsForTrace reads raw rows (any finality) for a trace via the store's
// read handle — FinalVerdictsForStep only returns the last cycle.
func rawRowsForStep(t *testing.T, store *metrics.Store, stepID string) []struct {
	AgentID      string `db:"agent_id"`
	JudgeAgentID string `db:"judge_agent_id"`
	RubricID     string `db:"rubric_id"`
	Category     string `db:"category"`
	IsFinal      bool   `db:"is_final"`
	Scope        string `db:"scope"`
	Model        string `db:"reviewed_model_id"`
} {
	t.Helper()
	var rows []struct {
		AgentID      string `db:"agent_id"`
		JudgeAgentID string `db:"judge_agent_id"`
		RubricID     string `db:"rubric_id"`
		Category     string `db:"category"`
		IsFinal      bool   `db:"is_final"`
		Scope        string `db:"scope"`
		Model        string `db:"reviewed_model_id"`
	}
	err := store.DB().Select(&rows,
		`SELECT agent_id, judge_agent_id, rubric_id, category, is_final, scope, reviewed_model_id
		 FROM model_review_verdicts WHERE step_id = ? ORDER BY id`, stepID)
	if err != nil {
		t.Fatalf("raw rows for step %q: %v", stepID, err)
	}
	return rows
}

// TestProducerLane01 is the L6 scenario: pair-review and adversarial-hook
// verdicts land in model_review_verdicts through the REAL collector, and
// every row carries the executing agent_id.
func TestProducerLane01(t *testing.T) {
	home := t.TempDir()
	store := newSuiteStoreAt(t, filepath.Join(home, "metrics.db"))
	logger := slog.New(slog.NewTextHandler(discardWriter{}, nil))
	msgBus := bus.New(nil, logger)
	t.Cleanup(func() { msgBus.Close() })

	// The REAL collector path: its step.* subscription persists whatever
	// the producers publish (grade-capture-01's contract shape).
	collector := metrics.NewCollector(store, msgBus, metrics.DefaultCollectorConfig())
	t.Cleanup(collector.Shutdown)

	taskStore, err := task.NewStore(filepath.Join(home, "tasks.db"), logger)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { _ = taskStore.Close() })
	stepStore := taskStore.StepStore()

	rm := agent.NewReviewManager(agent.ReviewManagerConfig{
		StepStore: stepStore,
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    logger,
	})
	rm.SetMetricsStore(store)

	// --- pin (a): pair_manager round verdict -> collector -> agent_id ---
	// Two registry agents (actor + reviewer) on scripted chatter; the
	// reviewer answers with an approval keyword so parseReviewOutput
	// yields ReviewApproved and the session converges in one round
	// (terminal -> is_final=true).
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		Logger:        logger,
		SharedChatter: &scriptedChatter{response: "approved: all requirements met", modelID: "pair-judge-model"},
	})
	if err := reg.RegisterSpec(&agent.AgentSpec{ID: "pair-actor", Name: "Actor", Role: agent.RoleExecutor, Enabled: true, Model: "pair-actor-model"}); err != nil {
		t.Fatalf("register actor spec: %v", err)
	}
	if err := reg.RegisterSpec(&agent.AgentSpec{ID: "pair-reviewer", Name: "Reviewer", Role: agent.RoleExecutor, Enabled: true, Model: "pair-judge-model"}); err != nil {
		t.Fatalf("register reviewer spec: %v", err)
	}
	// Registry loops get the hook seam too (mirrors the daemon wiring).
	reg.SetReviewVerdictEmitter(rm)

	tsk := task.NewTask("pair-lane", "produce the artifact")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	pm := agent.NewPairManager(agent.PairManagerConfig{
		Registry:  reg,
		TaskStore: taskStore,
		StepStore: stepStore,
		Bus:       msgBus,
		Logger:    logger,
	})
	pm.SetReviewVerdictEmitter(rm)
	session := pm.CreateSession(tsk.ID, "ship the artifact", "pair-actor", "pair-reviewer", 2)
	session.SetCriteria([]string{"artifact shipped"})

	if _, err := pm.RunRound(context.Background(), session.ID); err != nil {
		t.Fatalf("pair RunRound: %v", err)
	}
	// The collector drains its subscription on a goroutine; poll the raw
	// rows (deadline-bounded, hermetic).
	deadline := time.Now().Add(5 * time.Second)
	var rows []struct {
		AgentID      string `db:"agent_id"`
		JudgeAgentID string `db:"judge_agent_id"`
		RubricID     string `db:"rubric_id"`
		Category     string `db:"category"`
		IsFinal      bool   `db:"is_final"`
		Scope        string `db:"scope"`
		Model        string `db:"reviewed_model_id"`
	}
	for {
		rows = rawRowsForStep(t, store, "")
		if len(rows) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var pairRow *struct {
		AgentID      string `db:"agent_id"`
		JudgeAgentID string `db:"judge_agent_id"`
		RubricID     string `db:"rubric_id"`
		Category     string `db:"category"`
		IsFinal      bool   `db:"is_final"`
		Scope        string `db:"scope"`
		Model        string `db:"reviewed_model_id"`
	}
	for i := range rows {
		if rows[i].JudgeAgentID == "pair-reviewer" {
			pairRow = &rows[i]
			break
		}
	}
	if pairRow == nil {
		t.Fatalf("pair round published no verdict row through the collector (rows: %+v)", rows)
	}
	if pairRow.AgentID != "pair-actor" {
		t.Errorf("pair row agent_id = %q, want the ACTOR agent \"pair-actor\" (L6 agent dimension)", pairRow.AgentID)
	}
	if pairRow.Model != "pair-actor-model" {
		t.Errorf("pair row reviewed_model_id = %q, want %q (actor's model, not the step reviewer's)", pairRow.Model, "pair-actor-model")
	}
	if pairRow.Category != "pass" || pairRow.RubricID != "" {
		t.Errorf("pair approved row = %+v, want the pass row (empty rubric, category pass)", *pairRow)
	}
	if !pairRow.IsFinal {
		t.Errorf("converged pair round row is_final=false; a terminal round is final (L7 rollups must count it)")
	}

	// --- pin (b): adversarial verification hook verdict -> row --------
	hookRowStep := "lane-hook-step"
	tracker := agent.NewVerificationTracker(1)
	tracker.RecordToolCall("file_write", "x.go")
	spawner := &stubVerifierSpawner{output: "VERDICT: PASS\nCHECK: tests\nRESULT: PASS\n"}
	hook := agent.NewVerificationAutoTrigger(tracker, agent.VerificationConfig{Enabled: true, AutoTrigger: true, MaxFixLoops: 3})
	hook.SetSpawner(spawner)
	hook.SetReviewVerdictEmitter(rm)
	hook.SetAgentIDSource(func() string { return "hook-executor" })
	hook.SetTaskIDSource(func() string { return "task-hook" })
	// The step-job trace context carries "stp-"+stepID; the hook lane
	// derives its step binding from it.
	ctx := agent.WithTraceContext(context.Background(), "stp-"+hookRowStep)
	// Seed the blame join: the executor's final successful call.
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: time.Now().UTC(), Provider: "p", ModelID: "served-hook-model", AgentID: "hook-executor", LatencyMs: 20, TraceID: "stp-" + hookRowStep})
	mod := hook.PrepareNextTurn(ctx, agent.TurnState{ModelRef: "served-hook-model"})
	if mod.Modified {
		t.Errorf("PASS verdict expected no turn modification, got %+v", mod)
	}
	hookDeadline := time.Now().Add(5 * time.Second)
	var hookRows []struct {
		AgentID      string `db:"agent_id"`
		JudgeAgentID string `db:"judge_agent_id"`
		RubricID     string `db:"rubric_id"`
		Category     string `db:"category"`
		IsFinal      bool   `db:"is_final"`
		Scope        string `db:"scope"`
		Model        string `db:"reviewed_model_id"`
	}
	for {
		hookRows = rawRowsForStep(t, store, hookRowStep)
		if len(hookRows) > 0 || time.Now().After(hookDeadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(hookRows) == 0 {
		t.Fatalf("verification hook published no verdict row (pin b)")
	}
	h := hookRows[0]
	if h.AgentID != "hook-executor" {
		t.Errorf("hook row agent_id = %q, want \"hook-executor\"", h.AgentID)
	}
	if h.JudgeAgentID != "adversarial-verifier" {
		t.Errorf("hook row judge_agent_id = %q, want the verifier role id", h.JudgeAgentID)
	}
	if h.Category != "pass" {
		t.Errorf("hook PASS row category = %q, want pass", h.Category)
	}
	if h.Model != "served-hook-model" {
		t.Errorf("hook row reviewed_model_id = %q, want the blame-join served model", h.Model)
	}
	if !h.IsFinal {
		t.Errorf("hook PASS row is_final=false; a passed verification is terminal for the lane")
	}

	// --- pin (c): step lane stores agent_id from the step's AgentID ---
	stepRow := task.NewTaskStep("task-step-lane", "write the report", 0)
	stepRow.ID = "lane-step-3"
	stepRow.AgentID = "scribe"
	if err := stepStore.Create(stepRow); err != nil {
		t.Fatalf("create step: %v", err)
	}
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: time.Now().UTC(), Provider: "p", ModelID: "scribe-served-model", AgentID: "scribe", LatencyMs: 20, TraceID: "stp-lane-step-3"})
	rm.PublishReviewVerdict("lane-step-3", "task-step-lane", &agent.ReviewResult{
		Status:     agent.ReviewApproved,
		Feedback:   "clean",
		Confidence: 0.9,
		ReviewerID: "test-reviewer",
	}, 0)
	stepRows := waitForVerdicts(t, store, "lane-step-3", 1)
	rec := stepRows[0]
	if rec.AgentID != "scribe" {
		t.Errorf("step lane agent_id = %q, want the step row's AgentID \"scribe\" (payload reviewed_agent_id -> collector -> store)", rec.AgentID)
	}
	if rec.ReviewedModelID != "scribe-served-model" {
		t.Errorf("step lane reviewed_model_id = %q, want the blame-join model", rec.ReviewedModelID)
	}
	if !rec.IsFinal {
		t.Errorf("approved step review is_final=false (L7 regression)")
	}

	// Cross-check the model x agent breakdown sees the step-lane pair.
	breakdown, err := store.QueryReviewVerdictModelAgentBreakdown(time.Time{})
	if err != nil {
		t.Fatalf("model-agent breakdown: %v", err)
	}
	found := false
	for _, r := range breakdown {
		if r.Model == "scribe-served-model" && r.Agent == "scribe" {
			found = true
		}
	}
	if !found {
		t.Errorf("breakdown = %+v, want the (scribe-served-model, scribe) pair", breakdown)
	}

	// --- surfacing: endpoint group_by=agent + CLI --group-by agent ----
	// The same producer-written rows must answer on both user surfaces
	// (AGENTS.md: every feature needs a surface; the endpoint and CLI
	// shapes stay byte-compatible for the pre-L6 modes — exercised by
	// surface-01).
	srvBase, httpc := startReviewsServer(t, store, home)
	resp, err := httpc.Get(srvBase + "/api/v1/metrics/reviews?window=all&group_by=agent")
	if err != nil {
		t.Fatalf("GET reviews group_by=agent: %v", err)
	}
	defer resp.Body.Close()
	body := reviewsHTTPResponse(t, resp.Body)
	if body["group_by"] != "agent" {
		t.Errorf("endpoint group_by = %v, want agent", body["group_by"])
	}
	breakdownRaw, _ := body["breakdown"].([]any)
	agentPair := false
	for _, br := range breakdownRaw {
		m, _ := br.(map[string]any)
		if m["model"] == "scribe-served-model" && m["agent"] == "scribe" {
			agentPair = true
			if _, hasCat := m["category"]; !hasCat {
				t.Errorf("agent-mode breakdown row lost the always-present category key: %v", m)
			}
		}
	}
	if !agentPair {
		t.Errorf("endpoint breakdown group_by=agent = %v, want the (scribe-served-model, scribe) pair", breakdownRaw)
	}

	cliOut := runAnalyticsReviews(t, home, "--days", "3650", "--group-by", "agent")
	if !strings.Contains(cliOut, "scribe-served-model") || !strings.Contains(cliOut, "scribe") {
		t.Errorf("CLI --group-by agent output missing the seeded pair:\n%s", cliOut)
	}
	jsonOut := runAnalyticsReviews(t, home, "--days", "3650", "--group-by", "agent", "--json")
	var cliJSON struct {
		GroupBy   string `json:"group_by"`
		Breakdown []struct {
			Model string `json:"model"`
			Agent string `json:"agent"`
			Count int64  `json:"count"`
		} `json:"breakdown"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &cliJSON); err != nil {
		t.Fatalf("CLI --json unmarshal: %v (out: %s)", err, jsonOut)
	}
	if cliJSON.GroupBy != "agent" {
		t.Errorf("CLI json group_by = %q, want agent", cliJSON.GroupBy)
	}
	cliPair := false
	for _, r := range cliJSON.Breakdown {
		if r.Model == "scribe-served-model" && r.Agent == "scribe" {
			cliPair = true
		}
	}
	if !cliPair {
		t.Errorf("CLI json breakdown = %+v, want the seeded pair (surface parity with the endpoint)", cliJSON.Breakdown)
	}
}

// startReviewsServer boots the real reviews route table on loopback TLS
// (the TestSurface01 rig shape) against the store the producers wrote
// through, returning the base URL and a client that skips the
// self-signed verify.
func startReviewsServer(t *testing.T, store *metrics.Store, home string) (string, *http.Client) {
	t.Helper()
	port := harness.FreePort(t)
	cfg := commhttp.DefaultServerConfig()
	cfg.Addr = "127.0.0.1:" + strconv.Itoa(port)
	cfg.RequireAuth = false
	cfg.TLSCertFile = filepath.Join(home, "tls-b", "cert.pem")
	cfg.TLSKeyFile = filepath.Join(home, "tls-b", "key.pem")
	cfg.FingerprintFile = filepath.Join(home, "tls-b", "fingerprint.txt")
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // self-signed test cert
	srvCtx, srvCancel := context.WithCancel(context.Background())
	t.Cleanup(srvCancel)
	srv := commhttp.NewServer(cfg, nil, nil, store, nil, nil,
		commhttp.WithReviewVerdictProvider(store))
	if srv == nil {
		t.Fatalf("NewServer returned nil")
	}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Start(srvCtx) }()
	t.Cleanup(func() {
		transport.CloseIdleConnections()
	})
	base := "https://127.0.0.1:" + strconv.Itoa(port)
	httpc := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp0, err0 := httpc.Get(base + "/health")
		if err0 == nil {
			_ = resp0.Body.Close()
			return base, httpc
		}
		if time.Now().After(deadline) {
			t.Fatalf("reviews server never became ready: %v", <-srvErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stubVerifierSpawner answers SpawnVerifier with canned output.
type stubVerifierSpawner struct{ output string }

func (s *stubVerifierSpawner) SpawnVerifier(ctx context.Context, prompt string, modelRef string) (string, error) {
	return s.output, nil
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
