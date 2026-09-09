package agent

// Wire-level tests for leaf 02 of session-aware-intent-gate.
//
// IntentAnalyzer stores a concrete *llm.Client, which cannot be backed by a
// stub, so instead of stubbing the client these tests point a REAL
// *llm.Client at an httptest server that captures the outgoing chat
// completion request and decodes the messages array. This proves the exact
// bytes on the wire, including byte-identical contextless behavior.
//
// The dispatcher tests reuse the same capture server and assert that both
// AnalyzeTrueIntent call sites (ClassifyAndRoute and
// ResumeAfterClarification) build a non-empty session digest via
// buildSessionContextDigest and pass it through to the analyzer.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// ambiguousAnalysisJSON is a chat completion whose content the intent
// analyzer parses as a high-ambiguity analysis, so ClassifyAndRoute returns
// at the clarification gate (deterministic, no downstream classification).
const ambiguousAnalysisJSON = `{"goal":"clarify follow-up","ambiguity":0.85,"scope":"narrow","category":"clarification","suggested_questions":["Which change do you mean?"],"confidence":0.9}`

// captureServer records every chat completion request body it receives and
// replies with a fixed assistant content string.
type captureServer struct {
	Server *httptest.Server

	mu     sync.Mutex
	bodies [][]byte
}

func newCaptureServer(t *testing.T, content string) *captureServer {
	t.Helper()

	cs := &captureServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		cs.mu.Lock()
		cs.bodies = append(cs.bodies, body)
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		resp := `{"choices":[{"message":{"role":"assistant","content":` + strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(cs.Server.Close)
	return cs
}

type capturedChatRequest struct {
	Messages []llm.ChatMessage `json:"messages"`
}

// lastMessages decodes the messages array from the most recent captured
// request.
func (cs *captureServer) lastMessages(t *testing.T) []llm.ChatMessage {
	t.Helper()
	cs.mu.Lock()
	if len(cs.bodies) == 0 {
		cs.mu.Unlock()
		t.Fatal("no chat requests captured")
	}
	// Collect under lock; decode outside (mutexio: no Unmarshal under lock).
	body := cs.bodies[len(cs.bodies)-1]
	cs.mu.Unlock()
	var req capturedChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode captured request: %v", err)
	}
	return req.Messages
}

// analyzerMessages decodes the messages array from the FIRST captured
// request — the intent-analyzer call. With the A5 ambiguity gate (2026-09-09)
// a context-bearing session no longer stops at the clarification prompt, so
// the classifier's own LLM call may be the LAST captured body; tests that
// assert on the analyzer's wire shape must target the analyzer's request,
// not whichever request happens to be last.
func (cs *captureServer) analyzerMessages(t *testing.T) []llm.ChatMessage {
	t.Helper()
	cs.mu.Lock()
	if len(cs.bodies) == 0 {
		cs.mu.Unlock()
		t.Fatal("no chat requests captured")
	}
	// Collect under lock; decode outside (mutexio: no Unmarshal under lock).
	body := cs.bodies[0]
	cs.mu.Unlock()
	var req capturedChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode captured request: %v", err)
	}
	return req.Messages
}

// newDigestCaptureDispatcher builds a dispatcher wired to a real task
// registry (for buildSessionContextDigest) and a real llm.Client pointed at
// the capture server, mirroring the NewDispatcher classifier-client wiring.
func newDigestCaptureDispatcher(t *testing.T, cs *captureServer) *Dispatcher {
	t.Helper()

	logger := digestTestLogger()
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("create task registry: %v", err)
	}
	t.Cleanup(func() {
		if err := reg.Close(); err != nil {
			t.Errorf("close task registry: %v", err)
		}
	})

	return NewDispatcher(DispatcherConfig{
		Registry:     NewAgentRegistry(RegistryConfig{Logger: logger}),
		TaskStore:    reg.Store(),
		TaskRegistry: reg,
		ClassifierClient: llm.NewClient(&llm.ModelConfig{
			BaseURL: cs.Server.URL,
			ModelID: "capture",
		}),
		Logger: logger,
	})
}

// TestIntentAnalyzer_SessionRules_WireCapture proves, at the HTTP wire
// level through a real *llm.Client, that (a) a nil digest yields a user
// message byte-identical to the raw input, (b) a populated digest appends
// the exact [Recent session activity] block from the contract, and (c) the
// two session rules are present in the system prompt in both cases.
func TestIntentAnalyzer_SessionRules_WireCapture(t *testing.T) {
	cs := newCaptureServer(t, `{"goal":"fix bug","ambiguity":0.2,"scope":"narrow","category":"fix","suggested_questions":[],"confidence":0.9}`)
	ia := NewIntentAnalyzer(llm.NewClient(&llm.ModelConfig{
		BaseURL: cs.Server.URL,
		ModelID: "capture",
	}), digestTestLogger())

	const input = "did the change get made?"

	// (a) nil digest: byte-identical contextless behavior.
	if _, err := ia.AnalyzeTrueIntent(context.Background(), input, nil); err != nil {
		t.Fatalf("AnalyzeTrueIntent(nil digest): %v", err)
	}
	msgs := cs.lastMessages(t)
	if len(msgs) != 2 {
		t.Fatalf("captured %d messages, want 2", len(msgs))
	}
	if msgs[1].Content != input {
		t.Errorf("nil-digest user message not byte-identical to input:\n got: %q\nwant: %q", msgs[1].Content, input)
	}
	if strings.Contains(msgs[1].Content, testActivitySubstr) {
		t.Errorf("nil-digest user message contains activity block: %q", msgs[1].Content)
	}
	requireSessionRules(t, msgs[0].Content)

	// (b) populated digest: exact contract block.
	digest := &SessionContextDigest{
		LastTaskName:      "Fix the login bug",
		LastTaskState:     "completed",
		LastTaskAgent:     "coder",
		LastResultSummary: "Fixed the login bug.",
	}
	if _, err := ia.AnalyzeTrueIntent(context.Background(), input, digest); err != nil {
		t.Fatalf("AnalyzeTrueIntent(populated digest): %v", err)
	}
	msgs = cs.lastMessages(t)
	if len(msgs) != 2 {
		t.Fatalf("captured %d messages, want 2", len(msgs))
	}
	wantUser := input +
		"\n\n[Recent session activity]\n" +
		"Last task: Fix the login bug (state: completed, agent: coder)\n" +
		"Result summary: Fixed the login bug."
	if msgs[1].Content != wantUser {
		t.Errorf("populated-digest user message:\n got: %q\nwant: %q", msgs[1].Content, wantUser)
	}
	requireSessionRules(t, msgs[0].Content)
}

// TestDispatcher_ClassifyAndRoute_BuildsSessionDigest asserts that call
// site 1 (ClassifyAndRoute) builds a non-empty digest when the session has
// a task and passes it to the analyzer — visible in the captured user
// message as the activity block.
func TestDispatcher_ClassifyAndRoute_BuildsSessionDigest(t *testing.T) {
	cs := newCaptureServer(t, ambiguousAnalysisJSON)
	d := newDigestCaptureDispatcher(t, cs)

	base := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	seedDigestTask(t, d, "task-w1", "digest wiring task", "sess-wire", task.StateCompleted, base, "coder")
	seedDigestStep(t, d.taskRegistry, "task-w1", 0, task.StepCompleted, "Implemented the fix.")

	result, err := d.ClassifyAndRoute(context.Background(), "did the change get made?", "sess-wire", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	// A5 ambiguity gate: with session context present (non-empty digest),
	// an ambiguous analysis must NOT clarification-gate — history-aware
	// classification stands. (Pre-2026-09-09 this result was a
	// clarification; the gate now requires an EMPTY digest.)
	if result == nil {
		t.Fatal("nil result")
	}
	if result.ClarificationNeeded {
		t.Fatalf("context-bearing session clarification-gated an ambiguous input; A5 gate missing")
	}

	// The analyzer call still happened with the digest: the captured
	// intent-analyzer user message must carry the activity block. Target
	// the analyzer's request (first captured), not the latest — with the
	// A5 gate the dispatcher now proceeds to the classifier's own call.
	msgs := cs.analyzerMessages(t)
	if len(msgs) != 2 {
		t.Fatalf("captured %d messages, want 2", len(msgs))
	}
	if !strings.HasPrefix(msgs[1].Content, "did the change get made?") {
		t.Errorf("user message does not start with input: %q", msgs[1].Content)
	}
	wantSuffix := "\n\n[Recent session activity]\n" +
		"Last task: digest wiring task (state: completed, agent: coder)\n" +
		"Result summary: Implemented the fix."
	if !strings.HasSuffix(msgs[1].Content, wantSuffix) {
		t.Errorf("user message missing activity block suffix:\n got: %q\nwant suffix: %q", msgs[1].Content, wantSuffix)
	}
	requireSessionRules(t, msgs[0].Content)
}

// TestDispatcher_ClassifyAndRoute_EmptyDigestOmitsBlock asserts the
// contextless byte-compatibility guarantee through the dispatcher: with no
// task for the session, the captured user message is exactly the raw input.
func TestDispatcher_ClassifyAndRoute_EmptyDigestOmitsBlock(t *testing.T) {
	cs := newCaptureServer(t, ambiguousAnalysisJSON)
	d := newDigestCaptureDispatcher(t, cs)

	const input = "did the change get made?"
	result, err := d.ClassifyAndRoute(context.Background(), input, "sess-empty", nil, "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if result == nil || !result.ClarificationNeeded {
		t.Fatalf("expected clarification gate result, got %+v", result)
	}

	msgs := cs.lastMessages(t)
	if len(msgs) != 2 {
		t.Fatalf("captured %d messages, want 2", len(msgs))
	}
	if msgs[1].Content != input {
		t.Errorf("empty-digest user message not byte-identical to input:\n got: %q\nwant: %q", msgs[1].Content, input)
	}
	if strings.Contains(msgs[1].Content, testActivitySubstr) {
		t.Errorf("empty-digest user message contains activity block: %q", msgs[1].Content)
	}
	requireSessionRules(t, msgs[0].Content)
}

// TestDispatcher_ResumeAfterClarification_BuildsSessionDigest asserts that
// call site 2 (ResumeAfterClarification) also builds and passes the digest:
// the captured user message must start with the combined
// original+clarification input and carry the activity block.
func TestDispatcher_ResumeAfterClarification_BuildsSessionDigest(t *testing.T) {
	cs := newCaptureServer(t, ambiguousAnalysisJSON)
	d := newDigestCaptureDispatcher(t, cs)

	base := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	seedDigestTask(t, d, "task-w2", "clarified task", "sess-wire2", task.StateCompleted, base, "coder")
	seedDigestStep(t, d.taskRegistry, "task-w2", 0, task.StepCompleted, "Shipped the change.")

	// Stage the pending clarification: last recorded intent for the session
	// is a clarification whose summary holds the original input.
	d.sessionTracker.RecordIntent("sess-wire2", &Intent{
		Type:       string(IntentClarify),
		Confidence: 1.0,
		AgentType:  config.AgentIDChat,
		Summary:    "Fix the login bug",
	}, config.AgentIDChat)

	result, err := d.ResumeAfterClarification(context.Background(), "Fix the login bug", "the one you just finished", "sess-wire2")
	if err != nil {
		t.Fatalf("ResumeAfterClarification: %v", err)
	}
	// A5 gate (site 2): with session context present, an ambiguous
	// re-analysis must NOT ask a follow-up clarification — it proceeds
	// with history-aware routing. (Pre-2026-09-09 this clarified again.)
	if result != nil && result.ClarificationNeeded {
		t.Fatalf("context-bearing session re-clarified; A5 gate missing at ResumeAfterClarification")
	}

	// The re-analysis call still happened with the digest: the captured
	// intent-analyzer user message must start with the combined
	// original+clarification input and carry the activity block. Target
	// the analyzer's request (first captured) — post-gate, downstream
	// classification may add its own LLM call.
	msgs := cs.analyzerMessages(t)
	if len(msgs) != 2 {
		t.Fatalf("captured %d messages, want 2", len(msgs))
	}
	wantPrefix := "Fix the login bug\n\nUser clarification: the one you just finished"
	if !strings.HasPrefix(msgs[1].Content, wantPrefix) {
		t.Errorf("user message does not start with combined input:\n got: %q\nwant prefix: %q", msgs[1].Content, wantPrefix)
	}
	if !strings.Contains(msgs[1].Content, testActivitySubstr) {
		t.Errorf("user message missing activity block: %q", msgs[1].Content)
	}
	if !strings.Contains(msgs[1].Content, "Last task: clarified task") {
		t.Errorf("user message missing seeded task name: %q", msgs[1].Content)
	}
	requireSessionRules(t, msgs[0].Content)
}

// Leaf 03 Task 2: buildActivityBlock appends "Working directory: <path>"
// exactly when WorkingDirectory is set; the line is absent otherwise, with
// no dangling newline. buildAnalysisMessages is called directly because the
// block formatting (not the transport) is under test.
func TestIntentAnalyzer_ActivityBlock_WorkingDirectory(t *testing.T) {
	ia := &IntentAnalyzer{}
	const input = "create a file here"

	// (a) WorkingDirectory set: line appended after the existing block.
	digest := &SessionContextDigest{
		LastTaskName:     "Fix the login bug",
		WorkingDirectory: "/tmp/x",
	}
	msgs := ia.buildAnalysisMessages(input, digest)
	want := input +
		"\n\n[Recent session activity]\n" +
		"Last task: Fix the login bug\n" +
		"Working directory: /tmp/x"
	if msgs[1].Content != want {
		t.Errorf("user message with WorkingDirectory:\n got: %q\nwant: %q", msgs[1].Content, want)
	}

	// (b) WorkingDirectory empty: line absent, no dangling newline.
	digest = &SessionContextDigest{
		LastTaskName:      "Fix the login bug",
		LastResultSummary: "Fixed it.",
	}
	msgs = ia.buildAnalysisMessages(input, digest)
	want = input +
		"\n\n[Recent session activity]\n" +
		"Last task: Fix the login bug\n" +
		"Result summary: Fixed it."
	if msgs[1].Content != want {
		t.Errorf("user message without WorkingDirectory:\n got: %q\nwant: %q", msgs[1].Content, want)
	}
	if strings.Contains(msgs[1].Content, "Working directory") {
		t.Errorf("user message contains working directory line despite empty WorkingDirectory: %q", msgs[1].Content)
	}
	if strings.HasSuffix(msgs[1].Content, "\n") {
		t.Errorf("user message ends with dangling newline: %q", msgs[1].Content)
	}
}
