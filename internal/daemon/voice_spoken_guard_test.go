package daemon

// Defect A pins (2026-11-10, link recording /tmp/link-rec-1794333239
// transcript b): machine-shaped / evidence-template text reached the
// caller's SPEAKERS. The voice lane's TurnRunner adapter
// (loopTurnRunner.Run) must apply the spoken guard with the same bounded
// rewrite-retry posture the chat lane uses, exactly once per reply; and a
// tools-less employee's prompt must not carry the JSON evidence-format
// template it echoed.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

// leakShapedReply is the recorded spoken leak (machine-shaped parts
// verbatim). It passes the shared reply guard untouched (the chat lane
// ships the [unverified: label on purpose — pinned in internal/agent), so
// only the voice adapter's spoken guard can stop it before TTS.
const leakShapedReply = "[unverified: no tools were executed this turn] Correction: I did not create or modify any files this turn.\n\n" +
	"Verified evidence from an earlier real tool execution:\n" +
	"- memory_retain succeeded, saving the call state fact. Evidence: tool_call_id call_ad75ede0b24b4933aa30100b returned success=true and memory_id 3a0dae49-96c3-4f33-b888-285e57fd39f2."

const cleanSpokenReply = "sorry, I lost my train of thought there. could you tell me who's calling and what you need?"

// scriptedChatter returns canned replies in order (the last entry repeats
// forever) and records EVERY message of every Chat call, so both the
// reply-shape and the anchor-prompt pins ride one harness.
type scriptedChatter struct {
	mu       sync.Mutex
	replies  []string
	calls    int
	messages [][]llm.ChatMessage
}

func (c *scriptedChatter) Chat(_ context.Context, msgs []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.calls
	if i >= len(c.replies) {
		i = len(c.replies) - 1
	}
	c.calls++
	c.messages = append(c.messages, msgs)
	return &llm.Response{Content: c.replies[i], Usage: llm.TokenUsage{TotalTokens: 1}}, nil
}

func (c *scriptedChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *scriptedChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "spoken-guard-stub"}
}

func (c *scriptedChatter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// allPromptText joins every recorded message content (system prompt slot
// + anchored validation instructions live on separate messages, so a
// msgs[0] peek would miss the anchor).
func (c *scriptedChatter) allPromptText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sb strings.Builder
	for _, msgs := range c.messages {
		for _, m := range msgs {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func spokenGuardRunner(t *testing.T, chatter llm.Chatter, resolver EmployeeDefinitionResolver) (*loopTurnRunner, *session.MemoryStore) {
	t.Helper()
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	template := agent.NewAgentLoop("template-spoken-guard", t.TempDir(),
		agent.WithLLMChatter(chatter))
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	return &loopTurnRunner{
		mgr:        mgr,
		template:   template,
		workingDir: t.TempDir(),
		sessions:   sessions,
		employees:  resolver,
		logger:     logger,
	}, sessions
}

// secretaryDefinition mirrors config/employees/secretary.json5's load
// shape: tools: [] (nothing produces tool evidence), spoken-word persona.
func secretaryDefinition() *employee.Employee {
	def := &employee.Employee{}
	def.ID = "secretary"
	def.Prompt = "You are Secretary, the operator's receptionist. Speak for the ear: short sentences, plain words. Never use markdown, code blocks, or identifiers."
	def.Tools = []string{}
	def.Constitution = employee.Constitution{
		Purpose:      "Answer calls, take messages, relay them to the operator",
		Role:         "Secretary",
		Charter:      "Answer inbound calls. Never dial out. Output is spoken aloud.",
		AutonomyTier: employee.Tier2Propose,
		ApprovedAt:   time.Now(),
	}
	return def
}

// toolsEmployeeDefinition has a read tool in its definition: the evidence
// template stays (it documents real tool results the employee can produce).
func toolsEmployeeDefinition() *employee.Employee {
	def := secretaryDefinition()
	def.ID = "reporter"
	def.Tools = []string{"file_read"}
	return def
}

// fakeEmployeeResolver is the in-test EmployeeDefinitionResolver:
// byID maps employee ids to definitions; unknown ids error.
type fakeEmployeeResolver struct {
	byID map[string]*employee.Employee
}

func (f *fakeEmployeeResolver) GetEmployee(_ context.Context, id string) (*employee.Employee, error) {
	if def, ok := f.byID[id]; ok {
		return def, nil
	}
	return nil, errors.New("unknown employee " + id)
}

// (a) The recorded leak shape must never reach the caller: the adapter
// rewrites it once, the recovered prose ships, and the machine-shaped text
// never persists as the assistant line.
func TestVoiceSpokenGuard_RewritesLeakBeforeSay(t *testing.T) {
	chatter := &scriptedChatter{replies: []string{leakShapedReply, cleanSpokenReply}}
	r, sessions := spokenGuardRunner(t, chatter, &fakeEmployeeResolver{byID: map[string]*employee.Employee{
		"secretary": secretaryDefinition(),
	}})
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	reply, err := r.Run(context.Background(), voice.SessionHandle{
		ID: sess.ID, ConversationID: sess.ConversationID, EmployeeID: "secretary",
	}, "so did you write that down?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(reply, "[unverified:") || strings.Contains(reply, "tool_call_id") || strings.Contains(reply, "memory_id") {
		t.Fatalf("machine-shaped text shipped to the speaker: %q", reply)
	}
	if !strings.Contains(reply, "who's calling") {
		t.Fatalf("reply = %q, want the rewrite-recovered prose", reply)
	}
	msgs, err := sessions.GetMessages(sess.ID, 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 || msgs[1].Role != "assistant" || msgs[1].Content != reply {
		t.Fatalf("persisted exchange = %+v, want the guarded reply", msgs)
	}
}

// (b) The guard runs exactly once on the voice path: one rewrite, never a
// second (the chat lane's bounded-retry posture — a model that leaks BOTH
// times gets the canned speakable line and no third call), and a clean
// first reply is never rewritten at all.
func TestVoiceSpokenGuard_BoundedExactlyOnce(t *testing.T) {
	chatter := &scriptedChatter{replies: []string{leakShapedReply}} // leaks forever
	r, sessions := spokenGuardRunner(t, chatter, nil)
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	reply, err := r.Run(context.Background(), voice.SessionHandle{
		ID: sess.ID, ConversationID: sess.ConversationID,
	}, "did you note my message?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != agent.VoiceSpokenFallbackLine {
		t.Fatalf("second trip = %q, want the canned spoken line", reply)
	}
	// Exactly 2 LLM calls: the turn + ONE rewrite. A third call would mean
	// the guard re-ran its rewrite on the rewrite (double-rewrite).
	if got := chatter.callCount(); got != 2 {
		t.Fatalf("LLM calls = %d, want exactly 2 (original + one bounded rewrite)", got)
	}
}

func TestVoiceSpokenGuard_CleanReplyUntouched(t *testing.T) {
	chatter := &scriptedChatter{replies: []string{cleanSpokenReply}}
	r, sessions := spokenGuardRunner(t, chatter, nil)
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	reply, err := r.Run(context.Background(), voice.SessionHandle{
		ID: sess.ID, ConversationID: sess.ConversationID,
	}, "hello, I'm calling about the invoice")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != cleanSpokenReply {
		t.Fatalf("clean prose was altered: %q", reply)
	}
	if got := chatter.callCount(); got != 1 {
		t.Fatalf("LLM calls = %d, want exactly 1 — the guard must not rewrite a clean reply", got)
	}
}

// (c) Persona template slim: a tools-LESS voice employee's prompt must not
// carry the JSON evidence-format template (the echo source of the leak),
// while an employee WITH tools keeps the full anchor. Chat lanes never
// route through applyEmployeeDefinition, so their anchor is byte-identical
// (pinned in internal/agent TestValidationAnchor_ChatLaneByteIdentical).
func TestVoicePersona_ToolsLessEmployeeOmitsEvidenceTemplate(t *testing.T) {
	chatter := &scriptedChatter{replies: []string{cleanSpokenReply}}
	r, sessions := spokenGuardRunner(t, chatter, &fakeEmployeeResolver{byID: map[string]*employee.Employee{
		"secretary": secretaryDefinition(),
	}})
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := r.Run(context.Background(), voice.SessionHandle{
		ID: sess.ID, ConversationID: sess.ConversationID, EmployeeID: "secretary",
	}, "hi, I'd like to leave a message"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompt := chatter.allPromptText()
	if !strings.Contains(prompt, "You are Secretary") {
		t.Fatal("persona override missing — test harness broken")
	}
	if strings.Contains(prompt, "**Evidence format**") || strings.Contains(prompt, `"claims"`) {
		t.Fatalf("tools-less voice employee's prompt carries the JSON evidence template:\n%.400s", prompt)
	}
	if !strings.Contains(prompt, "## Validation & Escalation Instructions") {
		t.Error("slim dropped the whole anchor — the prose validation rules must stay")
	}
}

func TestVoicePersona_ToolsEmployeeKeepsEvidenceTemplate(t *testing.T) {
	chatter := &scriptedChatter{replies: []string{cleanSpokenReply}}
	r, sessions := spokenGuardRunner(t, chatter, &fakeEmployeeResolver{byID: map[string]*employee.Employee{
		"reporter": toolsEmployeeDefinition(),
	}})
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := r.Run(context.Background(), voice.SessionHandle{
		ID: sess.ID, ConversationID: sess.ConversationID, EmployeeID: "reporter",
	}, "did the last run produce the report file?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompt := chatter.allPromptText()
	if !strings.Contains(prompt, "**Evidence format**") {
		t.Fatal("employee WITH tools lost the evidence template — slim must be tools-scoped")
	}
}
