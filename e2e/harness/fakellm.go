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

	requests []map[string]any

	// debugPath, when set, receives one JSON line per request body.
	debugPath string
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
// "chat" @0.9.
func (f *FakeLLM) SetClassifierOutput(jsonReply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.classifierOut = jsonReply
}

// Reset clears all scripts and the request log (per-test setup).
func (f *FakeLLM) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toolCalls = nil
	f.postToolText = "done"
	f.chatText = "ok"
	f.classifierOut = ""
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
	resp := f.classifyLocked(body)
	f.mu.Unlock()

	// Tool-bearing executor turns go through the client's STREAMING path
	// ("stream": true). Emit the canned response as SSE deltas.
	if stream, _ := body["stream"].(bool); stream {
		f.writeSSE(w, resp)
		return
	}

	if f.debugPath != "" {
		if raw, err := json.Marshal(body); err == nil {
			fh, err := os.OpenFile(f.debugPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err == nil {
				_, _ = fh.Write(append(raw, '\n'))
				_ = fh.Close()
			}
		}
		if rawResp, err := json.Marshal(resp); err == nil {
			fh, err := os.OpenFile(f.debugPath+".resp", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err == nil {
				_, _ = fh.Write(append(rawResp, '\n'))
				_ = fh.Close()
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("fake-llm: encode response: %v", err)
	}
}

// classifyLocked routes one request by shape. Caller holds f.mu.
//
// Order matters: the planner's decompose prompt is rendered into the USER
// message of a planner-loop request that ALSO carries the base tool
// schemas, so planner detection must run before the executor branch —
// keyed on the rendered prompt text, not the system prompt.
func (f *FakeLLM) classifyLocked(body map[string]any) ChatCompletionResponse {
	// 1. Planner turns: the rendered decompose prompt (user message).
	all := msgTexts(body)
	if strings.Contains(all, "task planner") && strings.Contains(all, "Decompose") {
		plan := `{"steps":[{"description":"do the scripted step","tool_hint":"code","depends_on":[]}]}`
		return envelope(body, ResponseMsg{Role: "assistant", Content: plan}, "stop")
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
			}, "tool_calls")
		}
		return envelope(body, ResponseMsg{
			Role: "assistant", Content: f.postToolText,
		}, "stop")
	}

	// 3. Classifier turns.
	sys := systemText(body)
	if strings.Contains(sys, "intent classifier") || strings.Contains(sys, "multi-intent detector") {
		content := f.classifierOut
		if content == "" {
			content = `{"intent":"chat","confidence":0.9,"reasoning":"fake-llm default"}`
			if isImperativeWorkRequest(body) {
				content = `{"intent":"code","confidence":0.92,"reasoning":"fake-llm imperative"}`
			}
		}
		return envelope(body, ResponseMsg{Role: "assistant", Content: content}, "stop")
	}

	// 4. Everything else: chat agents, reviewers, summarizers.
	return envelope(body, ResponseMsg{Role: "assistant", Content: f.chatText}, "stop")
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
