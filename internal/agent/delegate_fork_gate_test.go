package agent

// DSH-1 fork-seed gate: a Mode:"fork" spawn seeds the child from the parent
// transcript ONLY when the TARGET child agent's own config opts in
// (delegate_context: full); a default (task-scope) child degrades to the
// fresh-brief behavior with the request untouched. The parent conversation
// rides the tool-execution context (tools.ContextWithConversation); without
// it even a full-context child cannot seed.
//
// Contract: .hermes/contracts/01-delegation.md B2 + the user's DSH-1 knob
// design; slicer semantics pinned by delegate_fork_test.go.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// parentConvWithMarker builds a parent conversation with one completed turn
// carrying marker, plus an in-flight trailing user message that the slicer
// must exclude.
func parentConvWithMarker(marker string) *Conversation {
	conv := NewConversation()
	conv.AddUserMessage("analyze this: " + marker)
	conv.AddAssistantMessage("analysis done referencing " + marker)
	conv.AddUserMessage("in-flight, not yet answered")
	return conv
}

// seedGateHarness builds a manager over a registry holding two real specs
// (one full-context, one default) and wires the conversation-seed adapter so
// seeded children's conversations can be inspected.
func seedGateHarness(t *testing.T) (*DelegateManager, *AgentRegistry) {
	t.Helper()
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["fork-full"] = &AgentSpec{ID: "fork-full", Enabled: true, DelegateContext: "full"}
	reg.specs["fork-task"] = &AgentSpec{ID: "fork-task", Enabled: true}
	reg.mu.Unlock()

	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetForkSeedWriter(reg.ForkSeedWriter())
	return mgr, reg
}

// TestSpawn_ForkSeedsFullContextChild: a fork request on a delegate_context:full
// child captures the parent's completed-turn prefix (in-flight turn excluded)
// and stamps it onto the child's conversation before its first turn.
func TestSpawn_ForkSeedsFullContextChild(t *testing.T) {
	mgr, reg := seedGateHarness(t)

	marker := "PARENT-MARKER-FULL"
	ctx := tools.ContextWithConversation(context.Background(), parentConvWithMarker(marker))
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "fork-full",
		Message: "continue the analysis",
		Mode:    "fork",
	}, "sess-gate")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if !res.Success {
		t.Fatalf("spawn failed: %s", res.Error)
	}

	// The child runs on a goroutine; seeding happens inside runChild before
	// its first turn. The loop errors fast (no LLM client) so Join settles
	// promptly — wait for it to avoid racing the seed write.
	if _, err := mgr.Join(context.Background(), "sess-gate", res.ChildID, 5*time.Second); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// The captured seed rides the child record (capture-once at Spawn).
	rec, ok := mgr.Lookup(res.ChildID)
	if !ok {
		t.Fatalf("child %s not registered", res.ChildID)
	}
	if len(rec.seed) == 0 {
		t.Fatal("full-context fork child got an empty seed")
	}
	found := false
	for _, m := range rec.seed {
		if strings.Contains(m.Content, marker) {
			found = true
		}
		if m.Content == "in-flight, not yet answered" {
			t.Fatal("in-flight parent turn leaked into the fork seed")
		}
	}
	if !found {
		t.Fatal("seed missing the parent's completed-turn marker")
	}

	// The conversation-seed adapter stamped the SAME messages onto the
	// child's conversation (before its first turn).
	loop, err := reg.Get("fork-full")
	if err != nil {
		t.Fatalf("child loop: %v", err)
	}
	conv := loop.GetConversation(rec.ConversationID)
	if conv == nil {
		t.Fatal("child conversation was not seeded (adapter did not run)")
	}
	seeded := conv.GetMessages()
	if len(seeded) != len(rec.seed) {
		t.Fatalf("seeded conversation has %d messages, want %d", len(seeded), len(rec.seed))
	}
	if got := seeded[len(seeded)-1].Content; !strings.Contains(got, marker) {
		t.Fatalf("seeded conversation's last message missing marker: %q", got)
	}
}

// TestSpawn_ForkDegradesToFreshBriefOnDefaultChild: the same fork request on
// a DEFAULT (task-scope) child leaves the seed nil — the child is treated
// exactly like any meept agent given a task.
func TestSpawn_ForkDegradesToFreshBriefOnDefaultChild(t *testing.T) {
	mgr, reg := seedGateHarness(t)

	marker := "PARENT-MARKER-TASK"
	ctx := tools.ContextWithConversation(context.Background(), parentConvWithMarker(marker))
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "fork-task",
		Message: "do the task",
		Mode:    "fork",
	}, "sess-gate")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	rec, ok := mgr.Lookup(res.ChildID)
	if !ok {
		t.Fatalf("child %s not registered", res.ChildID)
	}
	if len(rec.seed) != 0 {
		t.Fatal("task-scope child must NOT inherit the parent transcript")
	}

	// And its conversation carries nothing inherited.
	loop, err := reg.Get("fork-task")
	if err != nil {
		t.Fatalf("child loop: %v", err)
	}
	if conv := loop.GetConversation(rec.ConversationID); conv != nil && len(conv.GetMessages()) != 0 {
		t.Fatalf("task-scope child conversation pre-stamped with %d messages", len(conv.GetMessages()))
	}
}

// TestSpawn_ForkWithoutParentConversationCannotSeed: even a full-context
// child cannot seed when the calling context carries no conversation (legacy
// callers, direct tool tests) — degrade to fresh, never fabricate.
func TestSpawn_ForkWithoutParentConversationCannotSeed(t *testing.T) {
	mgr, _ := seedGateHarness(t)

	res, err := mgr.Spawn(context.Background(), DelegateRequest{
		AgentID: "fork-full",
		Message: "continue",
		Mode:    "fork",
	}, "sess-gate")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	rec, _ := mgr.Lookup(res.ChildID)
	if len(rec.seed) != 0 {
		t.Fatalf("nil parent conversation must yield a fresh brief, got %d seed messages", len(rec.seed))
	}
}

// TestSpawn_SeedDoesNotRecomputeAfterParentAdvances (capture-once, B2): the
// seed snapshot is taken at Spawn; later parent appends never mutate it.
func TestSpawn_SeedDoesNotRecomputeAfterParentAdvances(t *testing.T) {
	mgr, _ := seedGateHarness(t)

	conv := NewConversation()
	conv.AddUserMessage("q1")
	conv.AddAssistantMessage("a1")
	ctx := tools.ContextWithConversation(context.Background(), conv)

	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "fork-full",
		Message: "continue",
		Mode:    "fork",
	}, "sess-gate")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	rec, _ := mgr.Lookup(res.ChildID)
	if len(rec.seed) != 2 {
		t.Fatalf("seed = %d messages, want 2", len(rec.seed))
	}

	// Parent advances AFTER spawn: the child's snapshot must be untouched.
	conv.AddUserMessage("q2-later")
	conv.AddAssistantMessage("a2-later")
	if len(rec.seed) != 2 || rec.seed[1].Content != "a1" {
		t.Fatal("seed mutated by later parent appends (snapshot contract broken)")
	}
}

// TestChildWantsForkSeed_FailSafe pins the knob predicate: only "full" opts
// in; unknown values fail safe to task.
func TestChildWantsForkSeed_FailSafe(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{{"", false}, {"task", false}, {"full", true}, {"FULL", false}, {"everything", false}} {
		spec := &AgentSpec{DelegateContext: tc.val}
		if got := childWantsForkSeed(spec); got != tc.want {
			t.Errorf("delegate_context %q -> %v, want %v", tc.val, got, tc.want)
		}
	}
	if childWantsForkSeed(nil) {
		t.Error("nil spec must fail safe to task")
	}
}

// TestChildPrompt_ForkSeedNotRecomputed pins childPrompt's capture-once ride:
// the seed comes from the record (Spawn), never a fresh slice at render time.
func TestChildPrompt_ForkSeedNotRecomputed(t *testing.T) {
	mgr := NewDelegateManager(nil, nil, nil)
	rec := ChildRecord{Mode: "fork", seed: []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "seeded turn"},
	}}
	full, err := mgr.childPrompt(rec, DelegateRequest{Message: "the new task"})
	if err != nil {
		t.Fatalf("childPrompt: %v", err)
	}
	if !strings.Contains(full, "the new task") {
		t.Fatalf("brief lost the task message: %q", full)
	}
}
