package agent

// F5 bughunt pin tests (C5, C6, C7, C12, C16, C17). Each test pins one
// verified finding's fix at the manager level:
//
//   - C5: the cold-resume path enforces the live path's resumable-state
//     contract — a completed child is refused; an interrupted child resumes.
//   - C6: Cancel persists the interrupted state to the durable store.
//   - C7: captureSeed's shrink boundary never leaves an orphan tool result
//     as the seed's first message.
//   - C12: the resume-time persist preserves AgentID (second cold resume
//     works).
//   - C16: the runtime nesting cap refuses depth beyond maxDelegateDepth.
//   - C17: the resumed brief renders through childPrompt (output_schema
//     instruction present when the spawn had one).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
)

// C5 pin: a completed child's cold resume is REFUSED (terminal state); an
// interrupted child's cold resume proceeds.
func TestResume_ColdCompletedRefusedInterruptedResumes(t *testing.T) {
	store := session.NewMemoryStore(nil)
	mgr := NewDelegateManager(nil, nil, nil)
	mgr.SetStore(store)
	ctx := context.Background()

	// Completed child.
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "seed message",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	mgr.SettleForTest(res.ChildID, ChildStateCompleted, "done", "")

	fresh := NewDelegateManager(nil, nil, nil)
	fresh.SetStore(store)
	if _, err := fresh.Resume(ctx, "sess-parent", res.ChildID); !errors.Is(err, ErrChildNotResumable) {
		t.Fatalf("cold resume of completed child err = %v, want ErrChildNotResumable", err)
	}

	// Interrupted child (fresh spawn, interrupted settle) resumes.
	res2, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "another",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn 2: %v", err)
	}
	mgr.SettleForTest(res2.ChildID, ChildStateInterrupted, "", "cancelled")
	if _, err := fresh.Resume(ctx, "sess-parent", res2.ChildID); err != nil {
		t.Fatalf("cold resume of interrupted child: %v", err)
	}
}

// C5 pin: a failed (terminal) child's cold resume is refused too.
func TestResume_ColdFailedRefused(t *testing.T) {
	store := session.NewMemoryStore(nil)
	mgr := NewDelegateManager(nil, nil, nil)
	mgr.SetStore(store)
	ctx := context.Background()
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "m",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	mgr.SettleForTest(res.ChildID, ChildStateFailed, "", "boom")
	fresh := NewDelegateManager(nil, nil, nil)
	fresh.SetStore(store)
	if _, err := fresh.Resume(ctx, "sess-parent", res.ChildID); !errors.Is(err, ErrChildNotResumable) {
		t.Fatalf("cold resume of failed child err = %v, want ErrChildNotResumable", err)
	}
}

// C6 pin: Cancel persists an interrupted row; a cold resume of it works; a
// fresh child cancelled mid-run leaves an interrupted row in the store.
func TestCancel_PersistsInterruptedRow(t *testing.T) {
	store := session.NewMemoryStore(nil)
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	mgr.SetStore(store)
	ctx := context.Background()

	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "worker", Mode: "spawn", Message: "long task",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := mgr.Cancel(res.ChildID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// The durable row exists and is interrupted.
	row, err := store.GetChildDescriptor(ctx, res.ChildID)
	if err != nil {
		t.Fatalf("cancelled child left no durable row: %v", err)
	}
	if row.State != string(ChildStateInterrupted) {
		t.Fatalf("durable row state = %q, want interrupted", row.State)
	}

	// Cold resume of the cancelled child works on a fresh manager.
	fresh := NewDelegateManager(nil, nil, nil)
	fresh.SetStore(store)
	if _, err := fresh.Resume(ctx, "sess-parent", res.ChildID); err != nil {
		t.Fatalf("cold resume of cancelled child: %v", err)
	}
}

// C6 pin: a fresh child cancelled mid-run (before its goroutine settles)
// leaves an interrupted durable row (the settle's later no-op does not wipe it).
func TestCancel_MidRunFreshChildLeavesInterruptedRow(t *testing.T) {
	store := session.NewMemoryStore(nil)
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	mgr.SetStore(store)
	ctx := context.Background()
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "worker", Mode: "spawn", Message: "task",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// Cancel immediately (the unwired-registry goroutine already returned;
	// the child is settled running -> Cancel flips it interrupted and
	// persists BEFORE close(settle)).
	if err := mgr.Cancel(res.ChildID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	row, err := store.GetChildDescriptor(ctx, res.ChildID)
	if err != nil {
		t.Fatalf("durable row missing after mid-run cancel: %v", err)
	}
	if row.State != string(ChildStateInterrupted) {
		t.Fatalf("row state = %q, want interrupted", row.State)
	}
}

// C7 pin: an oversized transcript whose cut lands inside a tool exchange
// yields a seed that starts with a NON-tool message (no orphan tool result).
func TestCaptureSeed_CutInsideToolExchangeStartsNonTool(t *testing.T) {
	// Build a transcript where the oldest kept region after a blind
	// message-count cut would begin on a tool result. The tool exchange
	// sits so that dropping to the last 40 messages would leave the tool
	// result as the head.
	var seed []llm.ChatMessage
	// 38 plain alternating messages (they will be front-dropped).
	for i := range 38 {
		role := llm.RoleUser
		if i%2 == 1 {
			role = llm.RoleAssistant
		}
		seed = append(seed, llm.ChatMessage{Role: role, Content: fmt.Sprintf("m%02d", i)})
	}
	// Assistant with an unresolved-by-prefix tool call... actually resolve
	// it: assistant tool_calls, then TWO tool results, then a final assistant.
	seed = append(seed, llm.ChatMessage{Role: llm.RoleAssistant, Content: "calling tool",
		ToolCalls: []llm.ToolCall{{ID: "tc1"}}})
	seed = append(seed, llm.ChatMessage{Role: llm.RoleTool, Content: "tool result", ToolCallID: "tc1"})
	seed = append(seed, llm.ChatMessage{Role: llm.RoleAssistant, Content: "final answer"})

	var d ChildDescriptor
	d.captureSeed(seed)
	if len(d.SeedMessages) == 0 {
		t.Fatal("captureSeed kept nothing")
	}
	if d.SeedMessages[0].Role == llm.RoleTool {
		t.Fatalf("seed starts on an orphan tool result (role %s)", d.SeedMessages[0].Role)
	}
	// The seed must stay under the caps.
	if len(d.SeedMessages) > seedDescriptorMaxMessages {
		t.Fatalf("seed len %d over the message cap", len(d.SeedMessages))
	}
	if seedJSONBytes(d.SeedMessages) > seedDescriptorMaxBytes {
		t.Fatal("seed over the byte cap")
	}
}

// C7 pin (byte budget): few huge messages where the byte cut would land
// between an assistant tool_call and its tool result — the kept seed starts
// non-tool.
func TestCaptureSeed_ByteCutInsideToolExchangeStartsNonTool(t *testing.T) {
	big := strings.Repeat("x", seedDescriptorMaxBytes/5)
	seed := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: big},
		{Role: llm.RoleAssistant, Content: big, ToolCalls: []llm.ToolCall{{ID: "tc9"}}},
		{Role: llm.RoleTool, Content: big, ToolCallID: "tc9"},
		{Role: llm.RoleAssistant, Content: "settled"},
	}
	var d ChildDescriptor
	d.captureSeed(seed)
	if len(d.SeedMessages) == 0 {
		t.Fatal("captureSeed kept nothing")
	}
	if d.SeedMessages[0].Role == llm.RoleTool {
		t.Fatal("byte-budget cut left an orphan tool result as the seed head")
	}
}

// C12 pin: resume -> (simulated restart: fresh manager, same store) ->
// resume again succeeds; agent_id stays intact in the durable row.
func TestResume_SecondColdResumeAgentIDIntact(t *testing.T) {
	store := session.NewMemoryStore(nil)
	mgr := NewDelegateManager(nil, nil, nil)
	mgr.SetStore(store)
	ctx := context.Background()

	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "m",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	mgr.SettleForTest(res.ChildID, ChildStateInterrupted, "", "cancelled")

	// First cold resume (fresh manager = simulated restart).
	first := NewDelegateManager(nil, nil, nil)
	first.SetStore(store)
	if _, err := first.Resume(ctx, "sess-parent", res.ChildID); err != nil {
		t.Fatalf("first cold resume: %v", err)
	}
	// The resume-time persist preserved AgentID.
	row, err := store.GetChildDescriptor(ctx, res.ChildID)
	if err != nil {
		t.Fatalf("row after resume: %v", err)
	}
	if row.AgentID != "probe" {
		t.Fatalf("agent_id after resume = %q, want probe (C12: blanking refuses the next resume)", row.AgentID)
	}
	// Flip the durable row back to interrupted (the first resume marked it
	// running): the row is rewritten through the store directly — a fresh
	// manager has no live entry, so SettleForTest would no-op. AgentID is
	// preserved (this is the invariant under test).
	row, err = store.GetChildDescriptor(ctx, res.ChildID)
	if err != nil {
		t.Fatalf("row after first resume: %v", err)
	}
	row.State = string(ChildStateInterrupted)
	if err := store.SaveChildDescriptor(ctx, row); err != nil {
		t.Fatalf("re-save interrupted row: %v", err)
	}
	if row.AgentID != "probe" {
		t.Fatalf("agent_id after second cycle = %q, want probe", row.AgentID)
	}
	second := NewDelegateManager(nil, nil, nil)
	second.SetStore(store)
	if _, err := second.Resume(ctx, "sess-parent", res.ChildID); err != nil {
		t.Fatalf("second cold resume: %v (agent_id intact required)", err)
	}
}

// C16 pin: a depth-4 chain succeeds; a depth-5 spawn is refused (never
// clamped). The chain is threaded via contextWithDelegateDepth.
func TestSpawn_DepthCapRefusesBeyondMax(t *testing.T) {
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	ctx := context.Background()

	// depth-4 chain: caller depth 0 -> child 1 -> ... -> child 4 succeeds.
	depth := 0
	for depth < maxDelegateDepth {
		c := ctx
		if depth > 0 {
			c = contextWithDelegateDepth(ctx, depth)
		}
		res, err := mgr.Spawn(c, DelegateRequest{AgentID: "w", Message: "m"}, "sess")
		if err != nil {
			t.Fatalf("depth-%d spawn: %v", depth+1, err)
		}
		rec, _ := mgr.Lookup(res.ChildID)
		if rec.Depth != depth+1 {
			t.Fatalf("depth-%d child recorded depth %d", depth+1, rec.Depth)
		}
		depth++
	}
	// depth-5: refused, not clamped. Spawn surfaces refusals through the
	// result envelope (Success:false + Error) per the C1 tool contract.
	res5, rec5, err := spawnAtDepth(mgr, ctx, maxDelegateDepth)
	if err == nil && res5.Success {
		t.Fatal("depth-5 spawn succeeded; want refusal")
	}
	refused := err != nil || strings.Contains(res5.Error, "max nesting depth")
	if !refused {
		t.Fatalf("depth-5 result = %+v (err %v), want the max-nesting-depth refusal", res5, err)
	}
	_ = rec5
}

// spawnAtDepth spawns with the given caller depth, returning (result, rec, err).
func spawnAtDepth(mgr *DelegateManager, ctx context.Context, callerDepth int) (DelegateResult, ChildRecord, error) {
	c := contextWithDelegateDepth(ctx, callerDepth)
	res, err := mgr.Spawn(c, DelegateRequest{AgentID: "w", Message: "m"}, "sess")
	if err != nil {
		return DelegateResult{}, ChildRecord{}, err
	}
	rec, _ := mgr.Lookup(res.ChildID)
	return res, rec, nil
}

// C16 pin: a req.Depth ABOVE the caller's context depth cannot deepen the
// chain past the ceiling.
func TestSpawn_ReqDepthCannotExceedCap(t *testing.T) {
	mgr := NewDelegateManager(nil, func(string, any) {}, func(string) error { return nil })
	_, _, err := spawnAtDepth(mgr, context.Background(), maxDelegateDepth-1)
	// caller depth 3 -> child 4 is allowed...
	if err != nil {
		t.Fatalf("depth-4 spawn refused: %v", err)
	}
	// ...but a req.Depth of 5 from a top-level caller is refused (via the
	// result envelope, the C1 tool contract shape).
	res, err := mgr.Spawn(context.Background(), DelegateRequest{
		AgentID: "w", Message: "m", Depth: maxDelegateDepth + 1,
	}, "sess")
	if err == nil && res.Success {
		t.Fatal("req.Depth beyond the cap must be refused, not clamped")
	}
	if err == nil && !strings.Contains(res.Error, "max nesting depth") {
		t.Fatalf("req.Depth-over-cap result = %+v, want the max-nesting-depth refusal", res)
	}
}

// C10 pin: the child's run is executed through the run seam with the RESOLVED
// route (provider/model) as the model override — the allowlist-gated route
// gates the child's actual LLM call, not just the record metadata.
func TestRunChildTurn_EnforcesResolvedRoute(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["rogue"] = &AgentSpec{ID: "rogue", Enabled: true, Model: "banned-model"}
	reg.mu.Unlock()

	var gotRef string
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetRunFunc(func(ctx context.Context, agentID, message, conversationID, modelRef string) (string, error) {
		gotRef = modelRef
		return "ok", nil
	})

	// The child spec is pinned to a disallowed model, but the RESOLVED route
	// (record.Model/Provider — what the allowlist gated at spawn) is what
	// reaches the run seam as the override.
	rec := ChildRecord{
		ChildID: "delegate-route", AgentID: "rogue", ParentSessionID: "s",
		Mode: "spawn", ConversationID: "conv-route",
		State:    ChildStateRunning,
		Provider: "allowed-provider",
		Model:    "allowed-model",
	}
	mgr.children[rec.ChildID] = &childEntry{rec: rec, settle: make(chan struct{})}
	mgr.resumeChild(context.Background(), rec.ChildID, rec, "do the thing")
	if gotRef != "allowed-provider/allowed-model" {
		t.Fatalf("run-seam modelRef = %q, want allowed-provider/allowed-model (the resolved route overrides the disallowed spec pin)", gotRef)
	}

	// Identity route (no provider pin): the bare model rides through (the
	// loop resolves it through its own alias chain), never an empty drop.
	rec2 := rec
	rec2.ChildID = "delegate-route2"
	rec2.Provider = ""
	rec2.Model = "lfm-1.2b"
	mgr.children[rec2.ChildID] = &childEntry{rec: rec2, settle: make(chan struct{})}
	mgr.resumeChild(context.Background(), rec2.ChildID, rec2, "x")
	if gotRef != "lfm-1.2b" {
		t.Fatalf("identity-route modelRef = %q, want lfm-1.2b", gotRef)
	}
}

// C17 pin: the resumed fork brief carries the output_schema instruction when
// the spawn had one, and the Context prefix when set.
func TestResumeChild_BriefCarriesOutputSchemaAndContext(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.mu.Unlock()

	var got string
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetRunFunc(func(ctx context.Context, agentID, message, conversationID, modelRef string) (string, error) {
		got = message
		return "ok", nil
	})

	rec := ChildRecord{
		ChildID: "delegate-brief", AgentID: "probe", ParentSessionID: "s",
		Mode: "fork", ConversationID: "conv-brief",
		State:        ChildStateRunning,
		Context:      "the caller context",
		OutputSchema: []byte(`{"type":"object"}`),
	}
	mgr.children[rec.ChildID] = &childEntry{rec: rec, settle: make(chan struct{})}
	mgr.resumeChild(context.Background(), rec.ChildID, rec, "do the thing")

	if !strings.Contains(got, "IMPORTANT: Your response must be valid JSON matching this schema") {
		t.Fatalf("resumed brief missing the output_schema instruction:\n%s", got)
	}
	if !strings.Contains(got, "Context: the caller context") {
		t.Fatalf("resumed brief missing the Context prefix:\n%s", got)
	}
	if !strings.Contains(got, "do the thing") {
		t.Fatalf("resumed brief missing the seed payload:\n%s", got)
	}
}
