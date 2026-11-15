package daemon

// Reasoning-TOKEN RETENTION (voice path, part b) — end-to-end pins through
// loopTurnRunner.Run.
//
//   - TestVoiceReasoning_RetainedAndReusedNextTurn_NotSpoken: a voice turn
//     whose model reply carries separate-channel Reasoning (a) returns the
//     thinking-stripped text to the talk loop (that text is what is sent to
//     qwen3-TTS), (b) captures the reasoning into the per-session retention
//     slot, and (c) on the NEXT turn injects the retained reasoning as
//     optional bounded scaffolding into the model's SYSTEM prompt (never
//     the user/spoken text, never history).
//   - TestVoiceReasoning_RetentionBounded: a reasoning longer than the cap
//     is truncated in the slot and in the injected block.
//   - TestVoiceReasoning_RetentionPerSessionScoped: the slot is keyed per
//     session — a turn on a different session neither reads nor injects
//     another session's retention.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

// reasoningChatter records every Chat call and answers with a fixed
// Content + separate-channel Reasoning.
type reasoningChatter struct {
	mu        sync.Mutex
	messages  [][]llm.ChatMessage
	reply     string
	reasoning string
}

func (c *reasoningChatter) Chat(_ context.Context, msgs []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, msgs)
	return &llm.Response{Content: c.reply, Reasoning: c.reasoning, FinishReason: "stop"}, nil
}

func (c *reasoningChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *reasoningChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "reasoning-stub"}
}

func (c *reasoningChatter) set(reply, reasoning string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reply = reply
	c.reasoning = reasoning
}

// systemPrompts returns every system-role message content across all Chat
// calls, in call order.
func (c *reasoningChatter) systemPrompts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, msgs := range c.messages {
		for _, m := range msgs {
			if m.Role == llm.RoleSystem {
				out = append(out, m.Content)
			}
		}
	}
	return out
}

// userPrompts returns every user-role message content across all Chat
// calls, in call order.
func (c *reasoningChatter) userPrompts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, msgs := range c.messages {
		for _, m := range msgs {
			if m.Role == llm.RoleUser {
				out = append(out, m.Content)
			}
		}
	}
	return out
}

func reasonRunner(t *testing.T, reply, reasoning string) (*loopTurnRunner, *reasoningChatter, *session.MemoryStore) {
	t.Helper()
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	chatter := &reasoningChatter{reply: reply, reasoning: reasoning}
	template := agent.NewAgentLoop("template-reasoning", t.TempDir(),
		agent.WithLLMChatter(chatter))
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	r := &loopTurnRunner{
		mgr:        mgr,
		template:   template,
		workingDir: t.TempDir(),
		sessions:   sessions,
		logger:     logger,
	}
	return r, chatter, sessions
}

// TestVoiceReasoning_RetainedAndReusedNextTurn_NotSpoken covers (a) strip,
// (b) retention, (c) next-turn reuse.
func TestVoiceReasoning_RetainedAndReusedNextTurn_NotSpoken(t *testing.T) {
	const reasoning = "the caller explicitly said they want the time as words, not digits"
	r, chatter, sessions := reasonRunner(t, "it's just past two o'clock", reasoning)
	sess, err := sessions.Create("voice reasoning call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	handle := voice.SessionHandle{ID: sess.ID, ConversationID: sess.ConversationID}

	// Turn 1: the reply text returned to the talk loop (and thus to
	// qwen3-TTS) is the thinking-stripped Content, NOT the reasoning.
	reply, err := r.Run(context.Background(), handle, "what time is it")
	if err != nil {
		t.Fatalf("Run turn 1: %v", err)
	}
	if reply != "it's just past two o'clock" {
		t.Errorf("turn 1 reply = %q, want clean spoken text", reply)
	}
	if strings.Contains(reply, reasoning) || strings.Contains(reply, "thinking") {
		t.Errorf("reasoning leaked into the spoken text on turn 1: %q", reply)
	}
	// The turn isolated no field for TTS: only `reply` is spoken, and it
	// is the stripped Content.

	// (b) The reasoning is captured into the per-session retention slot.
	r.reasonMu.Lock()
	retained := r.retainedReason[sess.ID]
	r.reasonMu.Unlock()
	if retained != reasoning {
		t.Errorf("retained reasoning = %q, want %q", retained, reasoning)
	}

	// (c) Turn 2 sees no prior reasoning in its own system prompt yet (turn
	// 1 injected nothing — the slot only filled AFTER turn 1), then turn 2
	// itself fills the slot and the NEXT turn would inject it. Verify the
	// retention is reused on turn 2's model context: because the slot was
	// populated by turn 1, turn 2's Run MUST inject it.
	chatter.set("and the date is visible too", "turn two reasoning")
	if _, err := r.Run(context.Background(), handle, "and the date?"); err != nil {
		t.Fatalf("Run turn 2: %v", err)
	}

	sys := chatter.systemPrompts()
	var injected bool
	for _, s := range sys {
		if strings.Contains(s, "Prior reasoning") && strings.Contains(s, reasoning) {
			injected = true
			break
		}
	}
	if !injected {
		t.Errorf("turn 2 system prompt did not inject the retained reasoning %q; system prompts: %#v", reasoning, sys)
	}
	// The retained reasoning is model-scaffolding only: it must never
	// appear in the USER (spoken) prompt.
	for _, u := range chatter.userPrompts() {
		if strings.Contains(u, reasoning) {
			t.Errorf("retained reasoning leaked into the spoken user prompt: %.120q", u)
		}
	}
	// And it must never be spoken: the reply text stays the clean Content.
	if len(chatter.messages) == 0 {
		t.Fatal("no chat calls recorded")
	}
}

// TestVoiceReasoning_RetentionBounded: a reasoning beyond the cap is
// truncated both in the slot and in the injected block.
func TestVoiceReasoning_RetentionBounded(t *testing.T) {
	big := strings.Repeat("x", maxPriorReasoningBytes+500) + "UNIQUE-TAIL"
	r, chatter, sessions := reasonRunner(t, "ok", big)
	sess, err := sessions.Create("voice reasoning bounded")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	handle := voice.SessionHandle{ID: sess.ID, ConversationID: sess.ConversationID}

	if _, err := r.Run(context.Background(), handle, "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	r.reasonMu.Lock()
	retained := r.retainedReason[sess.ID]
	r.reasonMu.Unlock()
	if retained == "" {
		t.Fatal("expected a bounded retention after the run")
	}
	if len(retained) > maxPriorReasoningBytes+len("\n[…retained reasoning truncated]") {
		t.Errorf("retained reasoning length = %d, not bounded by the cap", len(retained))
	}
	if strings.Contains(retained, "UNIQUE-TAIL") {
		t.Error("reasoning past the cap must be truncated")
	}

	// The truncated block is what the next turn injects (bounded).
	if _, err := r.Run(context.Background(), handle, "again"); err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	for _, s := range chatter.systemPrompts() {
		if strings.Contains(s, "UNIQUE-TAIL") {
			t.Error("injected prior reasoning must be bounded (tail must not appear)")
		}
	}
}

// TestVoiceReasoning_RetentionPerSessionScoped: sessions are isolated in
// the retention slot — one session's reasoning never leaks into another.
func TestVoiceReasoning_RetentionPerSessionScoped(t *testing.T) {
	r, chatter, sessions := reasonRunner(t, "answer a", "reasoning for session A")
	sessA, err := sessions.Create("voice session A")
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	sessB, err := sessions.Create("voice session B")
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}
	handleA := voice.SessionHandle{ID: sessA.ID, ConversationID: sessA.ConversationID}
	handleB := voice.SessionHandle{ID: sessB.ID, ConversationID: sessB.ConversationID}

	if _, err := r.Run(context.Background(), handleA, "first on A"); err != nil {
		t.Fatalf("Run A1: %v", err)
	}

	// B's first turn must NOT see A's retention.
	chatter.set("answer b", "reasoning for session B")
	if _, err := r.Run(context.Background(), handleB, "first on B"); err != nil {
		t.Fatalf("Run B1: %v", err)
	}
	// The LAST Chat call is B1; its system prompt must not carry A's
	// reasoning (per-session isolation of the retention slot).
	if sys := chatter.systemPrompts(); len(sys) == 0 {
		t.Fatal("expected at least one system prompt")
	} else if strings.Contains(sys[len(sys)-1], "reasoning for session A") {
		t.Errorf("session B's turn leaked session A's retained reasoning into its system prompt: %.160q", sys[len(sys)-1])
	}
	// Assert the slot maps are distinct.
	r.reasonMu.Lock()
	ra := r.retainedReason[sessA.ID]
	rb := r.retainedReason[sessB.ID]
	r.reasonMu.Unlock()
	if ra == "" || rb == "" {
		t.Fatalf("expected both sessions retained (A=%q B=%q)", ra, rb)
	}
	if ra == rb {
		t.Errorf("sessions share a retention slot (A=%q B=%q) — must be per-session", ra, rb)
	}
}
