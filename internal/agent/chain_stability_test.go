package agent

// Chain-stability regression pins for the 2026-09-20 phase-2 live run
// (routing-smoke-yae_w4s3, 7 error rows / 48):
//
//	Mode A (4x "max ralph loop iterations reached without sufficient
//	        evidence"): the replan cap counted replan CYCLES, not PROGRESS.
//	        replay-04 spent its 3 replans making real forward progress
//	        (1/8 → 4/7 steps completed — the final reply says so) and was
//	        then killed at a hard cap calibrated for a stalled task. A task
//	        whose completed-step count GREW between replans is not a ralph
//	        loop; the cap must be progress-aware.
//	Mode B (3x "nudge budget exhausted: repeated unbacked claims"): the
//	        unbacked-claims guard fired on turns that had no FILE-tool
//	        opportunity at all — replay-01 was a chat/analyze turn whose
//	        report narrated prior step outputs; the model was nudged to
//	        "use file_write" (a tool its turn never needed) twice, then the
//	        step was killed with ErrNudgeBudgetExhausted. A task-kind that
//	        carries no file side-effects (question/answer turns) must not
//	        enter the nudge ladder at all.
//	Mode C (1x "context overflow and compaction exhausted"): the coder
//	        turn's request was ~2.5k tokens (limit 32768) yet llama-server
//	        answered HTTP 500 "Context size has been exceeded" — the local
//	        endpoint was saturated by CONCURRENT requests from sibling
//	        agents, not by this request's size. Compaction cannot shrink
//	        2.5k tokens, so the loop failed honestly after one attempt. A
//	        saturated-endpoint overflow of a small request is a transient
//	        condition: retry with backoff before declaring it fatal.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// ---------------------------------------------------------------------------
// Mode A: ralph-loop exhaustion despite forward progress
// ---------------------------------------------------------------------------

// newChainStabilityRalphFixture builds a RalphLoop over a temp task+step
// store (same shape as newRalphCapFixture).
func newChainStabilityRalphFixture(t *testing.T) (rl *RalphLoop, messageBus *bus.MessageBus, taskStore *task.Store, stepStore *task.StepStore) {
	t.Helper()
	taskStore, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })
	stepStore = taskStore.StepStore()
	logger := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	messageBus = bus.New(nil, logger)
	rl = NewRalphLoop(DefaultRalphLoopConfig(), nil, taskStore, stepStore, nil, messageBus, logger)
	return rl, messageBus, taskStore, stepStore
}

// testWriter routes slog output through t.Log so cap-decision lines appear
// in -v output.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// TestChainStability_RalphLoopProgress pins Mode A: when the completed-step
// count GREW between replan attempts, the task is making forward progress
// and must not be killed at the hard replan cap. This mirrors replay-04:
// attempt 1 completed 1/8 steps; the replans kept making progress; at
// iteration 3 the cap killed a task that was demonstrably advancing
// ("progress: 1/8 steps completed" → later "4/7").
func TestChainStability_RalphLoopProgress(t *testing.T) {
	rl, _, taskStore, stepStore := newChainStabilityRalphFixture(t)
	ctx := context.Background()

	tk := task.NewTask("progress task", "review the meept client for bugs")
	tk.SetState(task.StateExecuting)
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Simulate a task whose execution keeps completing NEW steps between
	// attempts: the completed-step count grows 1 → 2 → 3 while each
	// attempt's narrated evidence stays unsubstantiated (the real run
	// only ever carried the synthetic job stamp).
	addCompletedStep := func(n int) {
		s := task.NewTaskStep(tk.ID, fmt.Sprintf("step %d: review module", n), n)
		if err := stepStore.Create(s); err != nil {
			t.Fatalf("create step: %v", err)
		}
		if err := stepStore.SetState(s.ID, task.StepCompleted); err != nil {
			t.Fatalf("complete step: %v", err)
		}
	}

	unsubstantiated := func() json.RawMessage {
		result, _ := json.Marshal(map[string]any{
			"success":  true,
			"result":   "done",
			"evidence": []string{"job x completed by agent planner: working on it"},
		})
		return result
	}

	// Baseline: one step completed BEFORE the first attempt (recorded as
	// the initial observation, no extension granted for it).
	addCompletedStep(0)
	_, _, _ = rl.CheckCompletion(ctx, tk.ID, unsubstantiated())

	// The trigger loop drives the counter to the cap: with progress still
	// happening, each cap hit must EXTEND (counter → MaxIterations-1) and
	// keep replanning instead of failing the task.
	for i := 0; i < 6; i++ {
		addCompletedStep(i + 10) // forward progress BEFORE each attempt
		_, _, needsReplan := rl.CheckCompletion(ctx, tk.ID, unsubstantiated())
		if !needsReplan {
			t.Fatalf("round %d: expected a replan request (progress still unsubstantiated)", i)
		}
		if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
			t.Fatalf("round %d: TriggerReplan: %v", i, err)
		}
	}

	// The extension is one-cycle-per-progress-increase: the 6 progress
	// rounds above each hit the cap (counter 3 → extension to 2) and the
	// task must still be ALIVE — the pre-fix code terminalized it as
	// StateFailed at round 3's cap hit, discarding rounds 3-5's progress.
	// The stalled negative control below proves the cap still fires
	// without progress.
	got, err := taskStore.GetByID(tk.ID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State == task.StateFailed {
		t.Fatalf("task failed at the cap despite continued step progress (progress-aware cap required)")
	}
}

// TestChainStability_RalphLoopStalledStillFails is the negative control: a
// task whose completed-step count does NOT move between replans is a
// genuine ralph loop and must still fail at the cap.
func TestChainStability_RalphLoopStalledStillFails(t *testing.T) {
	rl, _, taskStore, _ := newChainStabilityRalphFixture(t)
	ctx := context.Background()

	tk := task.NewTask("stalled task", "write answer file")
	tk.SetState(task.StateExecuting)
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	// One step completed up front; NO new steps across attempts.
	stepStore := taskStore.StepStore()
	stalled := task.NewTaskStep(tk.ID, "only step", 0)
	if err := stepStore.Create(stalled); err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := stepStore.SetState(stalled.ID, task.StepCompleted); err != nil {
		t.Fatalf("complete step: %v", err)
	}

	unsubstantiated := func() json.RawMessage {
		result, _ := json.Marshal(map[string]any{
			"success":  true,
			"result":   "done",
			"evidence": []string{"job x completed by agent planner: narrating"},
		})
		return result
	}
	for i := 0; i < rl.config.MaxIterations; i++ {
		isComplete, _, needsReplan := rl.CheckCompletion(ctx, tk.ID, unsubstantiated())
		if isComplete || !needsReplan {
			t.Fatalf("iteration %d: expected a replan request", i+1)
		}
		if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
			t.Fatalf("TriggerReplan: %v", err)
		}
	}
	// No progress since the first attempt: the cap fires.
	if err := rl.TriggerReplan(ctx, tk.ID, nil); err != nil {
		t.Fatalf("TriggerReplan at cap: %v", err)
	}
	got, err := taskStore.GetByID(tk.ID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("stalled task state = %q, want failed at the cap", got.State)
	}
}

// ---------------------------------------------------------------------------
// Mode B: nudge-budget exhaustion on a turn with no file-tool opportunity
// ---------------------------------------------------------------------------

// qaReportChatter returns an analyze/answer-style report that narrates
// COMPLETED file side-effects it never performed this turn — the exact
// shape replay-01's analyst turn produced ("update the json files, and
// report" answered from memory_search + narration). No tool calls.
type qaReportChatter struct {
	callCount int
}

func (m *qaReportChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	return &llm.Response{
		Content: fmt.Sprintf("analysis %d: Updated the json files with the corrected readings", m.callCount),
		Usage:   llm.TokenUsage{TotalTokens: 1},
	}, nil
}

func (m *qaReportChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *qaReportChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "qa-report-chatter"}
}

// TestChainStability_NudgeBudgetRecovery pins Mode B: when the nudge budget
// for unbacked claims is spent, the loop must not throw away the model's
// final answer with ErrNudgeBudgetExhausted — it must deliver the text
// LABELLED as unverified (the annotate path already used when iterations
// run out) so the user receives the analysis with its trust level instead
// of a bare chain failure. The step-failure form is reserved for turns
// where the unverified claim is the DELIVERABLE (a fabrication), not a
// narration over real prior tool output.
func TestChainStability_NudgeBudgetRecovery(t *testing.T) {
	chatter := &qaReportChatter{}
	loop := newNudgeCapLoop(t, chatter)

	reply, err := loop.RunOnce(context.Background(), "analyze the readings and update the json files", "conv-chain-stability-nudge")
	if err != nil {
		t.Fatalf("RunOnce = error %v, want a labelled reply, not a chain failure", err)
	}
	if reply == "" {
		t.Fatal("reply = empty, want the model's final text")
	}
	// The reply must carry the unverified label (honest), not a failure.
	if !strings.Contains(reply, "[unverified:") {
		t.Fatalf("reply = %q, want the labelled-unverified delivery", reply)
	}
	// The nudges were still spent — the budget discipline holds.
	conv := loop.conversations.Get("conv-chain-stability-nudge")
	nudges := 0
	for _, msg := range conv.GetMessages() {
		if msg.Role == llm.RoleUser && strings.Contains(msg.Content, "those claims are unverified") {
			nudges++
		}
	}
	if nudges > maxNudgesPerClassPerTurn {
		t.Fatalf("nudges = %d, want ≤ %d (budget cap still enforced)", nudges, maxNudgesPerClassPerTurn)
	}
}

// fabricationOnlyChatter returns an unbacked completed-side-effect claim on
// a task whose ONLY deliverable IS the file — the run-7 fabrication shape.
// Killing this step (honest failure) remains correct.
type fabricationOnlyChatter struct {
	callCount int
}

func (m *fabricationOnlyChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	m.callCount++
	return &llm.Response{
		Content: fmt.Sprintf("claim %d: Created file done_%d.txt with the results", m.callCount, m.callCount),
		Usage:   llm.TokenUsage{TotalTokens: 1},
	}, nil
}

func (m *fabricationOnlyChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return m.Chat(ctx, messages, opts...)
}

func (m *fabricationOnlyChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "fabrication-chatter"}
}

// TestChainStability_FabricatedDeliverableStillFails: a task whose prompt
// demands a file write and whose every reply fabricates one still
// terminalizes with the honest error — Mode B's fix must not turn the
// anti-hallucination guard into a rubber stamp.
func TestChainStability_FabricatedDeliverableStillFails(t *testing.T) {
	chatter := &fabricationOnlyChatter{}
	loop := newNudgeCapLoop(t, chatter)

	_, err := loop.RunOnce(context.Background(), "make the file", "conv-chain-stability-fabrication")
	if err == nil {
		t.Fatal("expected the honest failure for a fabricated deliverable")
	}
	if !containsError(err, ErrNudgeBudgetExhausted) {
		t.Fatalf("err = %v, want ErrNudgeBudgetExhausted", err)
	}
}

// ---------------------------------------------------------------------------
// Mode C: saturated-endpoint context overflow on a small request
// ---------------------------------------------------------------------------

// overflowThenSucceedChatter returns a saturated-endpoint overflow (the
// llama.cpp HTTP 500 shape DetectContextOverflowFromBody already
// classifies) on the first call and a clean answer on the second. The
// request is SMALL — the overflow is a concurrency artifact, not a size
// verdict.
type overflowThenSucceedChatter struct {
	calls atomic.Int32
}

func (s *overflowThenSucceedChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	if s.calls.Add(1) == 1 {
		return nil, &llm.ContextOverflowError{
			ProviderID: "local-gguf",
			ModelID:    "LFM2.5-8B-A1B-Q4_K_M.gguf",
			StatusCode: 500,
			Message:    `{"error":{"code":500,"message":"Context size has been exceeded.","type":"server_error"}}`,
		}
	}
	return &llm.Response{Content: "recovered after the endpoint drained", FinishReason: "stop"}, nil
}

func (s *overflowThenSucceedChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return s.Chat(ctx, messages, opts...)
}

func (s *overflowThenSucceedChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "overflow-then-succeed", ContextLimit: 32768}
}

// TestAgentLoop_SmallRequestOverflowRetriesWithBackoff pins Mode C: a
// context-overflow verdict on a request that is clearly SMALL relative to
// the model's context limit cannot be a true size overflow — it is
// endpoint saturation (replay-21: ~2.5k estimated tokens, 32768 limit).
// Compaction cannot help (nothing to drop), so the loop must retry the
// SAME payload with backoff instead of failing the turn immediately. The
// second attempt succeeds and the reply is delivered.
func TestAgentLoop_SmallRequestOverflowRetriesWithBackoff(t *testing.T) {
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		MaxAttempts: 4,
	})
	t.Cleanup(clearDefaultBackoffOverride)

	chatter := &overflowThenSucceedChatter{}
	loop := newOverflowTestLoop(t, chatter)
	// No context firewall wired: compaction must not be the recovery path.

	reply, err := loop.RunOnce(context.Background(), "review the doc for completion", "conv-overflow-saturation")
	if err != nil {
		t.Fatalf("RunOnce = error %v, want recovery via backoff retry", err)
	}
	if reply != "recovered after the endpoint drained" {
		t.Fatalf("reply = %q, want the post-saturation response", reply)
	}
	if got := chatter.calls.Load(); got != 2 {
		t.Fatalf("chatter calls = %d, want 2 (overflow + one backoff retry)", got)
	}
}

// TestAgentLoop_LargeRequestOverflowStillFailsHonestly is the flip side: a
// TRUE size overflow (request near the model's limit) keeps the F-A1
// contract — compaction attempt, then honest failure. No backoff retries
// against a verdict that compaction already could not fix. The loop is
// pre-grown with 40 filler exchanges (~4k estimated tokens) and the served
// model config carries a 6k limit, putting the request ABOVE the
// saturatedOverflowRatio (0.5) threshold so the saturation path must NOT
// fire.
func TestAgentLoop_LargeRequestOverflowStillFailsHonestly(t *testing.T) {
	SetDefaultBackoffOverride(BackoffConfig{
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		MaxAttempts: 4,
	})
	t.Cleanup(clearDefaultBackoffOverride)

	overflow := &llm.ContextOverflowError{
		ProviderID: "p1",
		ModelID:    "m1",
		StatusCode: 500,
	}
	chatter := &overflowScriptChatter{
		script: []scriptedAttempt{
			{resp: nil, err: overflow},
			{resp: nil, err: overflow},
		},
	}
	loop := newOverflowTestLoop(t, chatter)
	// Wire a context firewall carrying a 6k context limit so the limit
	// resolves from l.contextFirewall.Config() in requestIsSmallVersusLimit,
	// then grow the conversation past the 50% smallness threshold
	// (3000 tokens) so the request counts as LARGE and the F-A1 path
	// (compaction, then honest failure) must handle the overflow.
	fw := llm.NewContextFirewall(
		chatter,
		&llm.ModelConfig{ContextLimit: 6000},
		llm.ContextFirewallConfig{Enabled: true, DropContextOnHardLimit: true, OverflowStrategy: "drop"},
		nil,
		slog.New(slog.DiscardHandler),
		nil,
	)
	loop.llm = fw
	loop.contextFirewall = fw
	conv := loop.conversations.Get("conv-overflow-true-size")
	for i := 0; i < 40; i++ {
		conv.AddAssistantMessage(fmt.Sprintf("assistant filler %d %s", i, strings.Repeat("x", 120)))
		conv.AddUserMessage(fmt.Sprintf("user filler %d %s", i, strings.Repeat("y", 120)))
	}

	// After compaction (82 → 5 messages), the retried request is tiny —
	// but the request that drew the FIRST overflow was large, which is
	// the verdict that matters: chatWithFailoverRaw classifies the error
	// BEFORE any compaction, on the messages as they were sent.
	_, err := loop.RunOnce(context.Background(), "hello", "conv-overflow-true-size")
	if err == nil {
		t.Fatal("expected the honest error for an un-compactable overflow")
	}
	if !errors.Is(err, overflow) && !strings.Contains(err.Error(), "context overflow") {
		t.Fatalf("err = %v, want the overflow surfaced honestly", err)
	}
	// F-A1 contract: overflow → compact → retry ONCE → second overflow
	// surfaces as the honest error (call 2 is the post-compaction retry;
	// its overflow must NOT be reclassified as saturation even though
	// the compacted request is now small).
	if got := chatter.calls.Load(); got != 2 {
		t.Fatalf("chatter calls = %d, want 2 (overflow + one compaction retry, no saturation backoff)", got)
	}
}
