// Package harness provides the hermetic e2e test harness for meept:
// a scratch-daemon manager (fresh binaries, sandboxed HOME/MEEPT_HOME,
// probed-free ports), a fake OpenAI-compatible LLM server, client helpers
// (CLI, HTTP API, chat submit + terminal await), and a sqlite reader for
// the daemon's tasks.db.
//
// Everything runs inside temp-dir worlds — never ~/.meept, never the
// user's live daemon or runtimes. Modeled on scripts/e2e-naive-user-chat.sh
// (the proven bash rig), ported to Go for `go test -tags e2e ./e2e/...`.
package harness

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"
)

// ChatCompletionResponse is the OpenAI-compatible completion envelope the
// meept LLM client parses (internal/llm/client.go parseResponseWithTools):
// choices[0].message.content as a JSON string plus optional structured
// tool_calls with function.arguments as a JSON-encoded string.
type ChatCompletionResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []Choice       `json:"choices"`
	Usage   map[string]int `json:"usage"`
}

// Choice is one completion choice.
type Choice struct {
	Index        int         `json:"index"`
	Message      ResponseMsg `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ResponseMsg mirrors choices[0].message. Content is a plain JSON string;
// ToolCalls carries OpenAI-shaped tool calls.
type ResponseMsg struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolCalls []RawCall `json:"tool_calls,omitempty"`
}

// RawCall is one OpenAI tool call.
type RawCall struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Function RawFunc `json:"function"`
}

// RawFunc is the function name + JSON-arguments of one tool call.
type RawFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall is one scripted executor tool call: name + JSON arguments.
type ToolCall struct {
	Name      string
	Arguments string
}

// FakeLLM is a scripted OpenAI-compatible LLM server. It answers
// POST /v1/chat/completions and GET /health (the runtime health endpoint
// shape). Requests are routed by SHAPE, not arrival order — the daemon
// fires classifier/planner/executor/chat calls concurrently and in
// varying order, so arrival-order scripting is inherently racy:
//
//   - a request carrying a non-empty "tools" array is an EXECUTOR turn:
//     it pops the next scripted tool call, or — when the tool-call queue
//     is empty — gets the post-tool text (the final user-facing reply);
//   - a request whose system prompt is the intent classifier gets a
//     classification JSON reply;
//   - a request whose system prompt is the task planner gets a plan JSON
//     reply;
//   - everything else (chat agents, reviewers, summarizers) gets the
//     chat text.
type FakeLLM struct {
	mu sync.Mutex

	server *httptest.Server
	url    string

	toolCalls     []ToolCall // consumed by executor turns, in order
	postToolText  string     // executor reply once the queue is empty
	chatText      string     // non-classifier/planner/executor replies
	classifierOut string     // classification JSON (empty = heuristic)
	plannerText   string     // planner decompose reply (empty = hard-coded plan)

	requests []map[string]any

	// debugPath, when set, receives one JSON line per request body.
	debugPath string

	// scripts holds predicate-keyed responses, evaluated in order BEFORE
	// the shape heuristics. Conversations bind to responses via
	// request-shape predicates instead of FIFO arrival order.
	scripts []scriptEntry
}

// RequestPredicate evaluates one completion request body. Predicates run
// under the FakeLLM mutex during response routing — they must not call
// back into the FakeLLM.
type RequestPredicate func(body map[string]any) bool

// ScriptedResponse is one scripted reply served when its predicate
// matches. Exactly one reply mode wins, checked in this order: Status
// (raw HTTP error injection), Raw (fully canned envelope), ToolCalls
// (tool_calls finish), Text (content + Finish). Usage, when set,
// overrides the canned usage counters on Text/ToolCalls envelopes.
type ScriptedResponse struct {
	Text      string     // completion content
	Finish    string     // finish_reason for Text (default "stop"; "content_filter" triggers DetectRefusal)
	ToolCalls []ToolCall // OpenAI tool calls (finish "tool_calls")
	Status    int        // non-zero: reply with this HTTP status + Body (error injection)
	Body      string     // error body for Status
	Raw       *ChatCompletionResponse
	Usage     map[string]int

	MaxServes int // 0 = unlimited; otherwise consumed after N matches
}

// TextResponse scripts a plain completion reply.
func TextResponse(text string) ScriptedResponse { return ScriptedResponse{Text: text} }

// ToolCallResponse scripts a tool_calls reply.
func ToolCallResponse(calls ...ToolCall) ScriptedResponse {
	return ScriptedResponse{ToolCalls: calls}
}

// QuotaResponse scripts the wire shape that internal/llm's client parses
// into *QuotaResetError: HTTP 429 + {"error":{"type":code,"resets_at":...}}
// (see parseQuotaBody / classifyQuotaDecision in internal/llm/errors_quota.go
// and the 429 branch of internal/llm/client.go).
func QuotaResponse(code string, resetsAt time.Time) ScriptedResponse {
	errObj := map[string]any{"type": code, "message": "usage window exhausted (fake-llm)"}
	if !resetsAt.IsZero() {
		errObj["resets_at"] = resetsAt.Unix()
	}
	raw, _ := json.Marshal(map[string]any{"error": errObj})
	return ScriptedResponse{Status: http.StatusTooManyRequests, Body: string(raw)}
}

// HTTPStatusResponse scripts a raw HTTP error (e.g. 500 for a retryable
// APIError, or a refusal-marker body — see DetectRefusalFromBody's
// conservative marker list in internal/llm/errors_refusal.go).
func HTTPStatusResponse(status int, body string) ScriptedResponse {
	return ScriptedResponse{Status: status, Body: body}
}

// RefusalFinishResponse scripts a 200 completion whose finish_reason is
// "content_filter" — the OpenAI-compatible refusal signal DetectRefusal
// maps onto *RefusalError (Source "finish_reason").
func RefusalFinishResponse() ScriptedResponse {
	return ScriptedResponse{Text: "", Finish: "content_filter"}
}

// PlannerPlanResponse scripts a planner decompose reply (raw plan JSON).
func PlannerPlanResponse(planJSON string) ScriptedResponse {
	return ScriptedResponse{Text: planJSON}
}

// ClassifierJSONResponse scripts an intent-classifier reply.
func ClassifierJSONResponse(classifierJSON string) ScriptedResponse {
	return ScriptedResponse{Text: classifierJSON}
}

// scriptEntry is one registered predicate/response pair.
type scriptEntry struct {
	pred   RequestPredicate
	resp   ScriptedResponse
	served int
}

func (e *scriptEntry) maxServes() int {
	if e.resp.MaxServes > 0 {
		return e.resp.MaxServes
	}
	return 0
}

// Script registers a predicate-keyed response. Scripts are evaluated in
// registration order BEFORE the legacy shape heuristics (planner →
// executor → classifier → chat), so a matching script always wins and
// non-scripting suites keep their exact legacy behavior. A script whose
// MaxServes is exhausted is skipped (later scripts, then heuristics).
func (f *FakeLLM) Script(pred RequestPredicate, resp ScriptedResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, scriptEntry{pred: pred, resp: resp})
}

// ScriptOnce registers a predicate-keyed response consumed on first match.
func (f *FakeLLM) ScriptOnce(pred RequestPredicate, resp ScriptedResponse) {
	resp.MaxServes = 1
	f.Script(pred, resp)
}

// ScriptN registers a predicate-keyed response served at most n times.
func (f *FakeLLM) ScriptN(n int, pred RequestPredicate, resp ScriptedResponse) {
	resp.MaxServes = n
	f.Script(pred, resp)
}

// ResetScripts drops every scripted predicate/response pair (legacy
// fields — tool calls, chat text, classifier/planner overrides — survive;
// use Reset for a full wipe).
func (f *FakeLLM) ResetScripts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = nil
}

// --- Request predicates -------------------------------------------------

// MessageContains matches when ANY message's text contains substr
// (same concatenation the shape heuristics match against).
func MessageContains(substr string) RequestPredicate {
	return func(body map[string]any) bool { return strings.Contains(msgTexts(body), substr) }
}

// LastUserMessageContains matches the final user message's text.
func LastUserMessageContains(substr string) RequestPredicate {
	return func(body map[string]any) bool {
		msgs, _ := body["messages"].([]any)
		var last string
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "user" {
				last, _ = msg["content"].(string)
			}
		}
		return strings.Contains(last, substr)
	}
}

// SystemPromptContains matches the system prompt text.
func SystemPromptContains(substr string) RequestPredicate {
	return func(body map[string]any) bool { return strings.Contains(systemText(body), substr) }
}

// HasToolNamed matches when the request's tools array declares a tool
// with the given function name.
func HasToolNamed(name string) RequestPredicate {
	return func(body map[string]any) bool {
		tools, ok := body["tools"].([]any)
		if !ok {
			return false
		}
		for _, raw := range tools {
			fn, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if f2, ok := fn["function"].(map[string]any); ok {
				fn = f2
			}
			if n, _ := fn["name"].(string); n == name {
				return true
			}
		}
		return false
	}
}

// IsPlannerRequest matches the planner decompose shape (same detection the
// legacy heuristic uses).
func IsPlannerRequest() RequestPredicate {
	return func(body map[string]any) bool {
		all := msgTexts(body)
		return strings.Contains(all, "task planner") && strings.Contains(all, "Decompose")
	}
}

// IsClassifierRequest matches the intent-classifier system-prompt shape.
func IsClassifierRequest() RequestPredicate {
	return func(body map[string]any) bool {
		sys := systemText(body)
		return strings.Contains(sys, "intent classifier") || strings.Contains(sys, "multi-intent detector")
	}
}

// IsExecutorRequest matches tool-bearing executor turns.
func IsExecutorRequest() RequestPredicate {
	return func(body map[string]any) bool { return hasTools(body) }
}

// SessionMentions matches when the serialized request body contains the
// given id anywhere (session ids travel in message context text, not a
// dedicated wire field — this is deliberately broad).
func SessionMentions(id string) RequestPredicate {
	return func(body map[string]any) bool {
		raw, err := json.Marshal(body)
		return err == nil && strings.Contains(string(raw), id)
	}
}

// And joins predicates conjunctively.
func And(preds ...RequestPredicate) RequestPredicate {
	return func(body map[string]any) bool {
		for _, p := range preds {
			if !p(body) {
				return false
			}
		}
		return true
	}
}

// Not negates a predicate.
func Not(p RequestPredicate) RequestPredicate {
	return func(body map[string]any) bool { return !p(body) }
}

// OnCallNumber matches on the nth (1-based) request that satisfies inner —
// "the second executor turn of this step" style scripting. Each
// OnCallNumber invocation carries its own counter (thread-safe: routing
// runs under the FakeLLM mutex).
func OnCallNumber(n int, inner RequestPredicate) RequestPredicate {
	count := 0
	return func(body map[string]any) bool {
		if !inner(body) {
			return false
		}
		count++
		return count == n
	}
}

// SetDebugPath enables per-request body dumping (diagnostics).
func (f *FakeLLM) SetDebugPath(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.debugPath = path
}

// NewFakeLLM starts the fake server on an ephemeral loopback port.
func NewFakeLLM() *FakeLLM {
	f := &FakeLLM{
		postToolText: "done",
		chatText:     "ok",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/chat/completions", f.handleCompletions)
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{}})
	})
	f.server = httptest.NewServer(mux)
	f.url = f.server.URL
	return f
}

// URL returns the fake server base URL (no trailing slash) for models.json5
// baseURL values: URL() + "/v1".
func (f *FakeLLM) URL() string { return f.url }

// Close shuts the server down.
func (f *FakeLLM) Close() { f.server.Close() }

// EnqueueToolCalls appends executor tool calls, served in order to the
// first requests that carry a "tools" array (the agent loop's tool-call
// round-trips).
func (f *FakeLLM) EnqueueToolCalls(calls ...ToolCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toolCalls = append(f.toolCalls, calls...)
}

// EnqueueFileWrite appends a scripted file_write tool call.
func (f *FakeLLM) EnqueueFileWrite(id, path, content string) {
	f.EnqueueToolCalls(ToolCall{
		Name: "file_write",
		Arguments: fmt.Sprintf(
			`{"path":%q,"content":%q,"direct":true}`, path, content),
	})
}

// SetPostToolText sets the executor turn's final reply, served once the
// scripted tool-call queue is empty.
func (f *FakeLLM) SetPostToolText(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.postToolText = text
}

// SetChatText sets the reply for generic (non-classifier/planner/executor)
// requests.
func (f *FakeLLM) SetChatText(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatText = text
}

// SetClassifierOutput pins the classification JSON. When empty (the
// default), the classifier is answered heuristically: imperative +
// artifact-noun inputs classify as "code" @0.92, everything else as
// "chat" @0.9. Per-prompt classifier outputs are scriptable via
// Script(IsClassifierRequest(), ClassifierJSONResponse(...)) and win
// over this global pin.
func (f *FakeLLM) SetClassifierOutput(jsonReply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.classifierOut = jsonReply
}

// SetPlannerResponse pins the planner decompose reply (a plan JSON
// document). When empty (the default), planner turns get the hard-coded
// one-step plan. Per-conversation plans are scriptable via
// Script(IsPlannerRequest(), PlannerPlanResponse(...)).
func (f *FakeLLM) SetPlannerResponse(planJSON string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plannerText = planJSON
}

// Reset clears all scripts and the request log (per-test setup).
func (f *FakeLLM) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toolCalls = nil
	f.postToolText = "done"
	f.chatText = "ok"
	f.classifierOut = ""
	f.plannerText = ""
	f.scripts = nil
	f.requests = nil
}

// Requests returns a copy of every completion request body received so far.
func (f *FakeLLM) Requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]map[string]any, len(f.requests))
	copy(out, f.requests)
	return out
}

// RequestCount reports how many completion requests were received.
func (f *FakeLLM) RequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// ToolCallCount reports how many executor tool-call turns were served.
func (f *FakeLLM) ToolCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	served := 0
	for _, body := range f.requests {
		if hasTools(body) {
			served++
		}
	}
	return served
}

func (f *FakeLLM) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.requests = append(f.requests, body)
	resp, rawStatus := f.classifyLocked(body)
	f.mu.Unlock()

	// Raw HTTP error injection (ScriptedResponse.Status).
	if rawStatus != nil {
		if f.debugPath != "" {
			if raw, err := json.Marshal(map[string]any{"injected_status": rawStatus.Status, "request": body}); err == nil {
				f.appendDebugLocked(raw)
			}
		}
		http.Error(w, rawStatus.Body, rawStatus.Status)
		return
	}

	// Tool-bearing executor turns go through the client's STREAMING path
	// ("stream": true). Emit the canned response as SSE deltas.
	if stream, _ := body["stream"].(bool); stream {
		f.writeSSE(w, resp)
		return
	}

	if f.debugPath != "" {
		if raw, err := json.Marshal(body); err == nil {
			f.appendDebugLocked(raw)
		}
		if rawResp, err := json.Marshal(resp); err == nil {
			f.appendDebugLocked(rawResp)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("fake-llm: encode response: %v", err)
	}
}

// injectedStatus is a scripted raw HTTP error reply.
type injectedStatus struct {
	Status int
	Body   string
}

// scriptReplyLocked renders a ScriptedResponse into the wire reply
// (result, isRawHTTPError). Caller holds f.mu.
func scriptReplyLocked(s ScriptedResponse) (any, bool) {
	if s.Status != 0 {
		return &injectedStatus{Status: s.Status, Body: s.Body}, true
	}
	if s.Raw != nil {
		return *s.Raw, false
	}
	msg := ResponseMsg{Role: "assistant"}
	finish := s.Finish
	if finish == "" {
		finish = "stop"
	}
	if len(s.ToolCalls) > 0 {
		finish = "tool_calls"
		for i, tc := range s.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, RawCall{
				ID:   fmt.Sprintf("call-scripted-%d-%d", time.Now().UnixNano(), i),
				Type: "function",
				Function: RawFunc{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
	} else {
		msg.Content = s.Text
	}
	env := envelope(nil, msg, finish)
	if s.Usage != nil {
		env.Usage = s.Usage
	}
	return env, false
}

// matchScriptLocked finds the first script whose predicate matches and
// still has serve budget, consuming one serve. Caller holds f.mu.
func (f *FakeLLM) matchScriptLocked(body map[string]any) (ScriptedResponse, bool) {
	for i := range f.scripts {
		entry := &f.scripts[i]
		if entry.maxServes() > 0 && entry.served >= entry.maxServes() {
			continue
		}
		if entry.pred(body) {
			entry.served++
			return entry.resp, true
		}
	}
	return ScriptedResponse{}, false
}

// appendDebugLocked appends one JSON line to the debug dump (best-effort).
// Callers hold no lock: http.Error paths in handleCompletions race freely.
func (f *FakeLLM) appendDebugLocked(raw []byte) {
	fh, err := os.OpenFile(f.debugPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = fh.Write(append(raw, '\n'))
	_ = fh.Close()
}

// classifyLocked routes one request by shape. Caller holds f.mu.
//
// Routing order:
//  0. Predicate scripts (Script/ScriptOnce/ScriptN), registration order —
//     conversation-bound responses and error injection live here.
//  1. Legacy overrides (planner text, executor queue, classifier text).
//  2. Shape heuristics (planner → executor → classifier → chat), unchanged.
//
// Order matters: the planner's decompose prompt is rendered into the USER
// message of a planner-loop request that ALSO carries the base tool
// schemas, so planner detection must run before the executor branch —
// keyed on the rendered prompt text, not the system prompt.
func (f *FakeLLM) classifyLocked(body map[string]any) (ChatCompletionResponse, *injectedStatus) {
	all := msgTexts(body)
	sys := systemText(body)

	// 0. Predicate scripts win over everything.
	if resp, ok := f.matchScriptLocked(body); ok {
		out, injected := scriptReplyLocked(resp)
		if injected {
			return ChatCompletionResponse{}, out.(*injectedStatus)
		}
		return out.(ChatCompletionResponse), nil
	}

	// 1. Legacy planner override.
	if f.plannerText != "" && strings.Contains(all, "task planner") && strings.Contains(all, "Decompose") {
		return envelope(body, ResponseMsg{Role: "assistant", Content: f.plannerText}, "stop"), nil
	}

	// 1. Legacy classifier override (planner prompts never carry the
	// classifier system prompt, so the plan-JSON heuristic below still
	// routes planner shapes even with a global classifier pin).
	if f.classifierOut != "" && (strings.Contains(sys, "intent classifier") || strings.Contains(sys, "multi-intent detector")) {
		return envelope(body, ResponseMsg{Role: "assistant", Content: f.classifierOut}, "stop"), nil
	}

	// 2. Planner turns: the rendered decompose prompt (user message).
	if strings.Contains(all, "task planner") && strings.Contains(all, "Decompose") {
		plan := f.plannerText
		if plan == "" {
			plan = `{"steps":[{"description":"do the scripted step","tool_hint":"code","depends_on":[]}]}`
		}
		return envelope(body, ResponseMsg{Role: "assistant", Content: plan}, "stop"), nil
	}

	// 2. Executor turns: the request carries tools.
	if hasTools(body) {
		if len(f.toolCalls) > 0 {
			next := f.toolCalls[0]
			f.toolCalls = f.toolCalls[1:]
			return envelope(body, ResponseMsg{
				Role: "assistant",
				ToolCalls: []RawCall{{
					ID:   fmt.Sprintf("call-%d", time.Now().UnixNano()),
					Type: "function",
					Function: RawFunc{
						Name:      next.Name,
						Arguments: next.Arguments,
					},
				}},
			}, "tool_calls"), nil
		}
		return envelope(body, ResponseMsg{
			Role: "assistant", Content: f.postToolText,
		}, "stop"), nil
	}

	// 3. Classifier turns.
	if strings.Contains(sys, "intent classifier") || strings.Contains(sys, "multi-intent detector") {
		content := f.classifierOut
		if content == "" {
			content = `{"intent":"chat","confidence":0.9,"reasoning":"fake-llm default"}`
			if isImperativeWorkRequest(body) {
				content = `{"intent":"code","confidence":0.92,"reasoning":"fake-llm imperative"}`
			}
		}
		return envelope(body, ResponseMsg{Role: "assistant", Content: content}, "stop"), nil
	}

	// 4. Everything else: chat agents, reviewers, summarizers.
	return envelope(body, ResponseMsg{Role: "assistant", Content: f.chatText}, "stop"), nil
}

// msgTexts concatenates every message's content. Content may be a plain
// string or an array of content blocks; anything else is appended as raw
// JSON so keyword matching still sees it.
func msgTexts(body map[string]any) string {
	var sb strings.Builder
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		switch c := msg["content"].(type) {
		case string:
			sb.WriteString(c)
			sb.WriteString("\n")
		case []any:
			for _, block := range c {
				b, ok := block.(map[string]any)
				if !ok {
					continue
				}
				if text, _ := b["text"].(string); text != "" {
					sb.WriteString(text)
					sb.WriteString("\n")
				}
			}
		default:
			if c != nil {
				if raw, err := json.Marshal(c); err == nil {
					sb.Write(raw)
					sb.WriteString("\n")
				}
			}
		}
	}
	return sb.String()
}

// hasTools reports whether the request payload carries a non-empty tools
// array (the agent loop's executor-turn marker).
func hasTools(body map[string]any) bool {
	raw, ok := body["tools"]
	if !ok || raw == nil {
		return false
	}
	tools, ok := raw.([]any)
	return ok && len(tools) > 0
}

// systemText returns the first system message's content, or "".
func systemText(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "system" {
			if c, _ := msg["content"].(string); c != "" {
				return c
			}
		}
	}
	return ""
}

// isImperativeWorkRequest reports whether the last user message asks for
// concrete file work (the classifier heuristic mirror of the dispatcher's
// imperative arbitration).
func isImperativeWorkRequest(body map[string]any) bool {
	msgs, _ := body["messages"].([]any)
	var lastUser string
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "user" {
			lastUser, _ = msg["content"].(string)
		}
	}
	lower := strings.ToLower(lastUser)
	for _, verb := range []string{"create", "write", "make", "fix", "add", "implement"} {
		if strings.Contains(lower, verb) {
			for _, noun := range []string{"file", "doc", "config", "script", "report"} {
				if strings.Contains(lower, noun) {
					return true
				}
			}
		}
	}
	return false
}

// envelope builds the OpenAI completion response.
func envelope(body map[string]any, msg ResponseMsg, finish string) ChatCompletionResponse {
	model, _ := body["model"].(string)
	return ChatCompletionResponse{
		ID:      "chatcmpl-fake",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{Index: 0, Message: msg, FinishReason: finish}},
		Usage: map[string]int{
			"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20,
		},
	}
}

// writeSSE emits one completion as an OpenAI-compatible SSE stream: a
// role chunk, a content chunk, optional tool-call chunks, a finish chunk
// with usage, then [DONE].
func (f *FakeLLM) writeSSE(w http.ResponseWriter, resp ChatCompletionResponse) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	send := func(chunk map[string]any) {
		raw, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		if flusher != nil {
			flusher.Flush()
		}
	}

	msg := resp.Choices[0].Message
	// Chunk 1: role.
	send(map[string]any{
		"id": resp.ID, "object": "chat.completion.chunk", "model": resp.Model,
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{"role": "assistant"},
		}},
	})
	// Content chunk.
	if msg.Content != "" {
		send(map[string]any{
			"id": resp.ID, "object": "chat.completion.chunk", "model": resp.Model,
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{"content": msg.Content},
				"finish_reason": nil,
			}},
		})
	}
	// Tool-call chunks.
	for i, tc := range msg.ToolCalls {
		send(map[string]any{
			"id": resp.ID, "object": "chat.completion.chunk", "model": resp.Model,
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index":    i,
						"id":       tc.ID,
						"type":     "function",
						"function": map[string]any{"name": tc.Function.Name, "arguments": tc.Function.Arguments},
					}},
				},
				"finish_reason": nil,
			}},
		})
	}
	// Finish chunk with usage.
	send(map[string]any{
		"id": resp.ID, "object": "chat.completion.chunk", "model": resp.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": resp.Choices[0].FinishReason,
		}},
		"usage": resp.Usage,
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
