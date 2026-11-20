package agent

// 04-clutter TEST-GATE T1 (F11a): empty-reply synthesis pin.
//
// Frozen defect: F-ORCH-15 (CRITICAL, live-verified). A direct file-write
// turn completes with reply="" - the work ran (hello.txt on disk) but the
// user gets nothing. Frozen evidence: /tmp/usability-rig/probes/
// p1-naive-continuity.json turn 1: handler_case=route_to_agent,
// status=completed, reply="" (verbatim empty string below).
//
// Contract (01-server-reply-hygiene T1, master C1): a completed turn NEVER
// ships an empty reply. When the loop produced no final text but the tool
// ledger carries effects, the reply is synthesized FROM THE LEDGER (paths
// written, commands run) and marked reply_source="synthesized" (C1). Never
// canned when evidence exists; never fabricates effects absent from the
// ledger. One bounded summary re-prompt is the alternative, not a loop.
//
// RED today: the route_to_agent completion path ships the loop's empty
// finalResponse verbatim (handler.go ~1254 region; loop.go RunOnceWithParts
// terminal-blank-stop path returns "" with nil error after a tool-only
// turn). GREEN after 04-clutter/01.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// p1Turn1Message is the VERBATIM user message from the frozen rig probe
// p1-naive-continuity.json turn 1 (the empty-reply incident).
const p1Turn1Message = "Create a file named hello.txt in the project directory with the text 'hello world'. Tell me the full path of the file you created."

// p1Turn1Reply is the VERBATIM terminal reply from the same probe: the
// empty string. Frozen here so the gate documents the exact defect shape.
const p1Turn1Reply = ""

// emptyReplyClassifierServer answers the analyzer, multi-intent detector,
// and single-intent classifier with a pinned tooluse verdict - the live
// p1 classification (intent=tooluse agent=coder handler=route_to_agent).
// tooluse dispatches synchronously through RouteToAgent (no task row).
func emptyReplyClassifierServer(t *testing.T) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []llm.ChatMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("classifier request decode: %v", err)
		}
		var prompt strings.Builder
		for _, m := range req.Messages {
			prompt.WriteString(m.Content)
			prompt.WriteString("\n")
		}
		var content string
		switch {
		case strings.Contains(prompt.String(), "identify ALL distinct intents"):
			content = `[]`
		case strings.Contains(prompt.String(), "Classify this user input"):
			content = `{"intent":"tooluse","confidence":0.95,"reasoning":"explicit file_write request"}`
		default:
			content = `{"goal":"create hello.txt and report its path","ambiguity":0.1,"scope":"narrow","category":"tooluse","suggested_questions":[],"confidence":0.9}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "empty-reply-classifier"})
}

// emptyReplyRig wires the full route_to_agent path: registry with a coder
// spec whose template loop carries the scripted chatter and a file_write
// tool that REALLY writes the requested path (the ledger evidence), a
// session store binding the conversation to a project dir, and a loop
// manager so resolveAgent builds the session-scoped loop.
func emptyReplyRig(t *testing.T, responses []*llm.Response) (*ChatHandler, *Dispatcher, string) {
	t.Helper()
	logger := slogDiscardLogger()
	projectDir := t.TempDir()

	toolReg := NewPlaceholderToolRegistry()
	toolReg.Register(NewMockTool("file_write", "write a file", func(ctx context.Context, args map[string]any) (any, error) {
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		if path == "" {
			return nil, fmt.Errorf("file_write: path required")
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, err
		}
		return &tools.ToolResult{Success: true, Result: "wrote " + path}, nil
	}))

	registry := NewAgentRegistry(RegistryConfig{Logger: logger})
	if err := registry.RegisterSpec(&AgentSpec{
		ID:      config.AgentIDCoder,
		Name:    "coder",
		Role:    RoleExecutor,
		Enabled: true,
		Purpose: "writes files",
		Constraints: AgentConstraints{
			MaxIterations: 6,
			Timeout:       30 * time.Second,
		},
	}); err != nil {
		t.Fatalf("register coder spec: %v", err)
	}
	t.Cleanup(func() { registry.Close() })

	template, err := registry.Get(config.AgentIDCoder)
	if err != nil || template == nil {
		t.Fatalf("coder template loop: %v", err)
	}
	template.llm = newMockChatter(responses...)
	template.registry = toolReg
	template.security = security.NewPermissionChecker(security.Config{})

	const convID = "conv-empty-reply-t1"
	d := NewDispatcher(DispatcherConfig{
		Registry:         registry,
		ClassifierClient: emptyReplyClassifierServer(t),
		Logger:           logger,
	})
	d.SetAgentLoopManager(NewManager(ManagerConfig{}))
	d.sessionStore = &stubSessionStore{sessions: map[string]*session.Session{
		convID: {ID: convID, ConversationID: convID, ProjectPath: projectDir},
	}}

	msgBus := bus.New(nil, logger)
	t.Cleanup(func() { msgBus.Close() })
	h := NewChatHandler(nil, d, msgBus, logger)
	return h, d, projectDir
}

// emptyReplyTerminal drives one handleRequest turn and returns the single
// turn.terminal event it emits.
func emptyReplyTerminal(t *testing.T, h *ChatHandler, message, convID string) TurnTerminalEvent {
	t.Helper()
	sub := h.bus.Subscribe("empty-reply-t1-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	payload, err := json.Marshal(ChatRequest{
		Message:        message,
		ConversationID: convID,
		SessionID:      convID,
	})
	if err != nil {
		t.Fatalf("marshal chat request: %v", err)
	}
	h.handleRequest(context.Background(), &models.BusMessage{
		ID:        "req-empty-reply-t1",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	})

	select {
	case msg := <-sub.Channel:
		var ev TurnTerminalEvent
		if err := json.Unmarshal(msg.Payload, &ev); err != nil {
			t.Fatalf("unmarshal turn.terminal: %v", err)
		}
		return ev
	case <-time.After(20 * time.Second):
		t.Fatal("no turn.terminal event within 20s")
		return TurnTerminalEvent{}
	}
}

// TestEmptyReplySynthesis pins F-ORCH-15: a tool-only completed turn must
// ship a non-empty reply naming the ledger's effects, marked
// reply_source="synthesized" (C1). RED today: reply="" ships verbatim.
func TestEmptyReplySynthesis(t *testing.T) {
	t.Run("tool_only_completed_turn_names_written_path", func(t *testing.T) {
		helloPath := filepath.Join(t.TempDir(), "hello.txt")
		// Iteration 1: the model calls file_write (the work happens).
		// Iteration 2: blank stop - the loop ends with NO final text,
		// the exact p1 shape ("has_report=false", empty finalResponse).
		h, _, _ := emptyReplyRig(t, []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-t1-write",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "file_write",
						Arguments: `{"path":"` + helloPath + `","content":"hello world","direct":true}`,
					},
				}},
				FinishReason: "tool_calls",
			},
			{Content: p1Turn1Reply, FinishReason: "stop"},
		})

		ev := emptyReplyTerminal(t, h, p1Turn1Message, "conv-empty-reply-t1")
		if ev.Status != "completed" {
			t.Fatalf("status = %q, want completed (the work DID run); error=%q", ev.Status, ev.Error)
		}
		if ev.HandlerCase != "route_to_agent" {
			t.Fatalf("handler_case = %q, want route_to_agent (the frozen p1 lane)", ev.HandlerCase)
		}
		// The artifact must exist: synthesis has real ledger evidence.
		if _, err := os.Stat(helloPath); err != nil {
			t.Fatalf("file_write did not run (rig precondition): %v", err)
		}
		// RED today: the completed terminal ships reply="" (p1 verbatim).
		if strings.TrimSpace(ev.Reply) == "" {
			t.Fatalf("F-ORCH-15: completed turn shipped an empty reply (frozen p1 shape: reply=%q); "+
				"the tool ledger recorded a write of %s - synthesize from it", p1Turn1Reply, helloPath)
		}
		if !strings.Contains(ev.Reply, "hello.txt") {
			t.Errorf("synthesized reply does not name the written artifact hello.txt: %q", ev.Reply)
		}
		if !strings.Contains(ev.Reply, helloPath) {
			t.Errorf("synthesized reply does not name the full written path %s (the user asked for it): %q", helloPath, ev.Reply)
		}
	})

	t.Run("reply_source_marks_synthesized", func(t *testing.T) {
		// C1 additive key reply_source ("loop"|"task_relay"|"recall"|
		// "synthesized"). Asserted through reflection so the gate compiles
		// before 01-safety/01 lands the field; RED until it exists AND is
		// set to "synthesized" on a ledger-derived reply.
		helloPath := filepath.Join(t.TempDir(), "hello.txt")
		h, _, _ := emptyReplyRig(t, []*llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call-t1-src",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "file_write",
						Arguments: `{"path":"` + helloPath + `","content":"hello world","direct":true}`,
					},
				}},
				FinishReason: "tool_calls",
			},
			{Content: "", FinishReason: "stop"},
		})
		ev := emptyReplyTerminal(t, h, p1Turn1Message, "conv-empty-reply-t1")

		field := reflect.ValueOf(ev).FieldByName("ReplySource")
		if !field.IsValid() || field.Kind() != reflect.String {
			t.Fatalf("C1 reply_source missing: TurnTerminalEvent has no string ReplySource field " +
				"(additive key owned by 01-safety/01; this gate flips GREEN when it lands and is set)")
		}
		if got := field.String(); got != "synthesized" {
			t.Errorf("reply_source = %q, want \"synthesized\" for a ledger-derived reply", got)
		}
	})

	t.Run("no_ledger_evidence_never_fabricates_effects", func(t *testing.T) {
		// A completed turn with NO tool calls and no final text must still
		// not ship empty - but the honest fallback/re-prompt must NOT name
		// file effects that never happened (synthesis is ledger-bound).
		h, _, _ := emptyReplyRig(t, []*llm.Response{
			{Content: "", FinishReason: "stop"},
		})
		ev := emptyReplyTerminal(t, h, p1Turn1Message, "conv-empty-reply-t1")
		if ev.Status != "completed" {
			t.Fatalf("status = %q, want completed", ev.Status)
		}
		if strings.TrimSpace(ev.Reply) == "" {
			t.Fatalf("completed turn shipped an empty reply even with no ledger evidence; " +
				"an honest bounded fallback or one summary re-prompt is required")
		}
		if strings.Contains(strings.ToLower(ev.Reply), "created") && strings.Contains(ev.Reply, "hello.txt") {
			t.Errorf("reply fabricates a file effect absent from the (empty) tool ledger: %q", ev.Reply)
		}
	})

	t.Run("prose_reply_passes_through_unchanged", func(t *testing.T) {
		// Regression guard (passes today, must survive the fix): when the
		// loop DID produce a final answer, synthesis never touches it.
		// Neutral prose: a file-creation claim with zero tool calls trips
		// the anti-hallucination nudge (a different, existing contract).
		const prose = "The answer to your question is four."
		h, _, _ := emptyReplyRig(t, []*llm.Response{
			{Content: prose, FinishReason: "stop"},
		})
		ev := emptyReplyTerminal(t, h, p1Turn1Message, "conv-empty-reply-t1")
		if ev.Status != "completed" {
			t.Fatalf("status = %q, want completed; error=%q", ev.Status, ev.Error)
		}
		if ev.Reply != prose {
			t.Errorf("prose reply altered: got %q, want %q", ev.Reply, prose)
		}
	})
}
