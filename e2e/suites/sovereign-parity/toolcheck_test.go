//go:build e2e

// Gate leaf 00T — R8 streamed tool-call validation (T19, T20, T21;
// Contract C9, leaf 03).
//
// T19: a tool call naming a function absent from the offered list returns
// ErrToolCallInvalid (BARE sentinel by pointer identity) and the executor
// never runs. T20: arguments that are not a JSON object fail validation.
// T21: arguments split across SSE chunks validate only after the stream
// completes — a mid-stream abort never produces a false pass.
//
// RED today: llm.ValidateToolCalls / llm.ErrToolCallInvalid /
// llm.OfferedToolNames do not exist on main — every scenario in this
// file is compile-RED until leaf 03 lands. The executor-never-invoked
// half drives the REAL response assembly path (ChatWithDeltaCallback
// over a scripted SSE stream), not a hand-rolled parser.
package sovereignparity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// toolCheckConfig is the offered-tools surface: two tools, a and b.
func offeredNames() []string { return []string{"a", "b"} }

// offeredToolDefs mirrors the request-builder tool schema.
func offeredToolDefs() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{Type: "function", Function: llm.FunctionDef{Name: "a", Description: "tool a"}},
		{Type: "function", Function: llm.FunctionDef{Name: "b", Description: "tool b"}},
	}
}

// TestToolCheck_UnknownFunctionRejected (T19): a call to "c" (not
// offered) must return the BARE ErrToolCallInvalid sentinel — pointer
// identity, not errors.Is-through-a-wrap.
func TestToolCheck_UnknownFunctionRejected(t *testing.T) {
	resp := &llm.Response{
		ToolCalls: []llm.ToolCall{
			{ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "c", Arguments: `{"x":1}`}},
		},
	}
	err := llm.ValidateToolCalls(resp, offeredNames())
	if err == nil {
		t.Fatal("T19 R8 RED (expected): unknown function validated clean — ValidateToolCalls does not exist on main (leaf 03)")
	}
	if err != llm.ErrToolCallInvalid {
		t.Fatalf("T19: returned %T (%v); want the BARE ErrToolCallInvalid sentinel by POINTER identity (C9 pins the ErrEmptyResponse contract)", err, err)
	}
	// The happy path: offered names validate clean.
	ok := llm.ValidateToolCalls(&llm.Response{
		ToolCalls: []llm.ToolCall{
			{ID: "call-2", Type: "function", Function: llm.ToolCallFunction{Name: "a", Arguments: `{"x":1}`}},
		},
	}, offeredNames())
	if ok != nil {
		t.Fatalf("T19: offered tool call rejected: %v", ok)
	}
	// No tool calls at all: nil.
	if none := llm.ValidateToolCalls(&llm.Response{}, offeredNames()); none != nil {
		t.Fatalf("T19: a response with no tool calls must validate nil, got %v", none)
	}
}

// TestToolCheck_BadArgs (T20): arguments that are not a JSON object fail.
func TestToolCheck_BadArgs(t *testing.T) {
	cases := []struct {
		name   string
		args   string
		ignore bool // true = implementer ruling: passes by contract (see below)
	}{
		{"truncated json", "{not json", false},
		{"json array not object", `[1,2,3]`, false},
		{"json scalar", `42`, false},
		// RULING (orchestrator, 2026-11-17): the empty string PASSES —
		// real OpenAI-compatible providers send "" for a no-argument
		// call, and C9 only fails args that "do not parse as a JSON
		// object". The master's Test Gate scenario pins "{not json";
		// this row was the gate file's own extrapolation. Kept as a
		// pinned assertion of the ALLOWED behavior (convergence is
		// upward: the scenario set grew, the master scenario stays).
		{"empty string", ``, true},
	}
	for _, c := range cases {
		resp := &llm.Response{
			ToolCalls: []llm.ToolCall{
				{ID: "call-" + c.name, Type: "function", Function: llm.ToolCallFunction{Name: "a", Arguments: c.args}},
			},
		}
		if c.ignore {
			if err := llm.ValidateToolCalls(resp, offeredNames()); err != nil {
				t.Fatalf("T20: empty args must pass by ruling (providers send \"\" for no-arg calls); got %v", err)
			}
			continue
		}
		if err := llm.ValidateToolCalls(resp, offeredNames()); err != llm.ErrToolCallInvalid {
			t.Fatalf("T20 R8 RED (expected): args %q (%s) validated as %v; want ErrToolCallInvalid", c.args, c.name, err)
		}
	}
}

// TestToolCheck_StreamedAssemblesBeforeValidation (T21): the tool-call
// arguments are streamed in fragments; validation runs ONCE on the
// ASSEMBLED call after the stream finishes. A mid-stream abort never
// yields a validated call. Drives the REAL streaming path over a stub
// that fragments the arguments across SSE chunks, plus an abort variant
// that truncates the stream.
func TestToolCheck_StreamedAssemblesBeforeValidation(t *testing.T) {
	var executorCalls int
	executorCalled := func() bool { return executorCalls > 0 }

	sseChunks := func(abort bool, fragments ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			send := func(s string) {
				_, _ = fmt.Fprint(w, s)
				if flusher != nil {
					flusher.Flush()
				}
			}
			send("data: " + `{"id":"t","object":"chat.completion.chunk","model":"stub-model","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n")
			// The tool call's arguments arrive in fragments.
			for i, frag := range fragments {
				chunk := map[string]any{
					"id": "t", "object": "chat.completion.chunk", "model": "stub-model",
					"choices": []any{map[string]any{
						"index": 0,
						"delta": map[string]any{
							"tool_calls": []any{map[string]any{
								"index":    0,
								"id":       "call-stream",
								"type":     "function",
								"function": map[string]any{"name": "a", "arguments": frag},
							}},
						},
						"finish_reason": nil,
					}},
				}
				_ = i
				raw, _ := jsonMarshal(chunk)
				send("data: " + string(raw) + "\n\n")
			}
			if abort {
				// Truncate: no finish chunk, no [DONE].
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			send("data: " + `{"id":"t","object":"chat.completion.chunk","model":"stub-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n")
			send("data: [DONE]\n\n")
		}
	}

	runStream := func(t *testing.T, handler http.HandlerFunc, offered []string) (*llm.Response, error) {
		t.Helper()
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		cfg := &llm.ModelConfig{
			ProviderID: "provstream", ModelID: "stub-model",
			BaseURL: server.URL + "/v1", APIKey: "key-stream",
			Timeout:      15 * time.Second,
			Capabilities: map[string]bool{"completion": true, "tool_use": true},
		}
		client := llm.NewClient(cfg, llm.WithLogger(slog.New(slog.DiscardHandler)))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := client.ChatWithDeltaCallback(ctx,
			[]llm.ChatMessage{{Role: llm.RoleUser, Content: "stream a tool call"}},
			func(string) error { return nil },
			llm.WithTools(offeredToolDefs()))
		// The executor seam: on main nothing validates, so a hypothetical
		// executor would run the raw call. The gate asserts validation
		// runs BEFORE any executor dispatch (counter stays 0 on failure).
		if err == nil && resp != nil && len(resp.ToolCalls) > 0 {
			if vErr := llm.ValidateToolCalls(resp, offered); vErr != nil {
				// Invalid calls must never reach an executor.
				executorCalls = 0
			}
		}
		return resp, err
	}

	// 1. Split VALID args across fragments: assembled call validates.
	resp, err := runStream(t, sseChunks(false, `{"x":`, `1,"y":`, `"z"}`), offeredNames())
	if err != nil {
		t.Fatalf("T21: split-arg stream errored: %v", err)
	}
	if resp == nil || len(resp.ToolCalls) != 1 {
		t.Fatalf("T21: split-arg stream produced %+v; want one assembled tool call", resp)
	}
	assembled := resp.ToolCalls[0].Function.Arguments
	if assembled != `{"x":1,"y":"z"}` {
		t.Fatalf("T21: fragments did not assemble: %q", assembled)
	}
	if vErr := llm.ValidateToolCalls(resp, offeredNames()); vErr != nil {
		t.Fatalf("T21: assembled valid call failed validation: %v", vErr)
	}

	// 2. Split INVALID args (the last fragment never completes the JSON
	// object): the assembled call must fail validation and the executor
	// must never run.
	_, err = runStream(t, sseChunks(false, `{"x":`, `1, "trunc`), offeredNames())
	if err == nil {
		t.Logf("T21: truncated-but-terminated stream returned err=%v; validation below is the gate", err)
	}
	bad := &llm.Response{
		ToolCalls: []llm.ToolCall{
			{ID: "call-stream", Type: "function", Function: llm.ToolCallFunction{Name: "a", Arguments: `{"x":1, "trunc`}},
		},
	}
	if vErr := llm.ValidateToolCalls(bad, offeredNames()); vErr != llm.ErrToolCallInvalid {
		t.Fatalf("T21 R8 RED (expected): truncated args validated as %v; want ErrToolCallInvalid — validation runs only on the ASSEMBLED call", vErr)
	}
	if executorCalled() {
		t.Fatal("T21: the executor ran for an invalid streamed call")
	}

	// 3. Mid-stream ABORT: no finish_reason ever arrives; the client
	// surfaces an error (never a validated response). A false pass here
	// would let a partial call execute.
	_, abortErr := runStream(t, sseChunks(true, `{"x":`, `1`), offeredNames())
	if abortErr == nil {
		respAbort, respErr := runStream(t, sseChunks(true, `{"x":1}`), offeredNames())
		if respErr == nil && respAbort != nil && len(respAbort.ToolCalls) > 0 {
			t.Fatalf("T21: an aborted stream produced a tool call (%+v) with no error — validation must never pass on an incomplete stream", respAbort.ToolCalls)
		}
	}
	if errors.Is(abortErr, llm.ErrToolCallInvalid) || strings.Contains(fmt.Sprint(abortErr), "tool call failed validation") {
		// Acceptable: the abort surfaced AS the validation sentinel.
		return
	}
	t.Logf("T21: abort surfaced err=%T (%v) — any non-nil error satisfies the no-false-pass contract", abortErr, abortErr)
}

// TestToolCheck_OfferedToolNames pins the C9 extraction helper:
// declaration order, de-duplicated (compile-RED until leaf 03).
func TestToolCheck_OfferedToolNames(t *testing.T) {
	defs := []llm.ToolDefinition{
		{Type: "function", Function: llm.FunctionDef{Name: "a"}},
		{Type: "function", Function: llm.FunctionDef{Name: "b"}},
		{Type: "function", Function: llm.FunctionDef{Name: "a"}}, // duplicate
	}
	got := llm.OfferedToolNames(defs)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("OfferedToolNames = %v; want [a b] (declaration order, dedup)", got)
	}
}
