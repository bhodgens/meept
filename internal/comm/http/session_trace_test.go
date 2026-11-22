package http

// 09-cli-affordances task 6 (GUI half): the GET
// /api/v1/sessions/{id}/trace bridge proxies session.dispatch_trace
// through rpcCall with the pinned response envelope. Pattern follows
// acp_status_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionTrace_BridgeForwardsRPC(t *testing.T) {
	var gotMethod string
	var gotParams map[string]any
	s := &Server{
		rpcCall: func(_ context.Context, method string, params json.RawMessage) (any, error) {
			gotMethod = method
			if err := json.Unmarshal(params, &gotParams); err != nil {
				t.Fatalf("decode params: %v", err)
			}
			return map[string]any{
				"entries": []any{
					map[string]any{
						"turn_id":           "turn-1",
						"input_summary":     "route me",
						"intent_type":       "plan",
						"agent_id":          "planner",
						"confidence":        0.9,
						"classifier_method": "llm",
						"handler_case":      "plan_route",
						"task_id":           "",
					},
				},
				"count": 1,
			}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-9/trace", http.NoBody)
	req.SetPathValue("id", "sess-9")
	w := httptest.NewRecorder()

	s.handleSessionTrace(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("09-gate: status = %d, want 200", resp.StatusCode)
	}
	if gotMethod != "session.dispatch_trace" {
		t.Errorf("09-gate: rpc method = %q, want session.dispatch_trace", gotMethod)
	}
	if id, _ := gotParams["session_id"].(string); id != "sess-9" {
		t.Errorf("09-gate: rpc session_id = %v, want sess-9", gotParams["session_id"])
	}

	var out struct {
		SessionID string           `json:"session_id"`
		Count     int              `json:"count"`
		Entries   []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("09-gate: decode envelope: %v", err)
	}
	if out.SessionID != "sess-9" {
		t.Errorf("09-gate: envelope session_id = %q, want sess-9", out.SessionID)
	}
	if out.Count != 1 || len(out.Entries) != 1 {
		t.Fatalf("09-gate: envelope count/entries = %d/%d, want 1/1", out.Count, len(out.Entries))
	}
	if intent, _ := out.Entries[0]["intent_type"].(string); intent != "plan" {
		t.Errorf("09-gate: entry intent_type = %v, want plan", out.Entries[0]["intent_type"])
	}
}

// TestSessionTrace_UnknownIDEmptyList pins the C5 contract at the bridge:
// an unknown id yields 200 + empty entries, never a 500.
func TestSessionTrace_UnknownIDEmptyList(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return map[string]any{"entries": []any{}, "count": 0}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session-nope/trace", http.NoBody)
	req.SetPathValue("id", "session-nope")
	w := httptest.NewRecorder()

	s.handleSessionTrace(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("09-gate: unknown id must be 200 (empty list), got %d", w.Result().StatusCode)
	}
	var out struct {
		Count   int              `json:"count"`
		Entries []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(w.Result().Body).Decode(&out); err != nil {
		t.Fatalf("09-gate: decode empty envelope: %v", err)
	}
	if out.Count != 0 || len(out.Entries) != 0 {
		t.Fatalf("09-gate: unknown id must yield an empty list, got count %d", out.Count)
	}
}

func TestSessionTrace_MissingID400(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			t.Error("rpcCall must not fire without an id")
			return nil, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//trace", http.NoBody)
	w := httptest.NewRecorder()
	s.handleSessionTrace(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("09-gate: missing id status = %d, want 400", w.Result().StatusCode)
	}
}

func TestSessionTrace_BadLimit400(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			t.Error("rpcCall must not fire for a bad limit")
			return nil, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/trace?limit=nope", http.NoBody)
	req.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()
	s.handleSessionTrace(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("09-gate: bad limit status = %d, want 400", w.Result().StatusCode)
	}
}

func TestSessionTrace_RPCError500(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, errors.New("rpc down")
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/trace", http.NoBody)
	req.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()
	s.handleSessionTrace(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("09-gate: rpc error status = %d, want 500", w.Result().StatusCode)
	}
}

func TestSessionTrace_NilRPCCall503(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-1/trace", http.NoBody)
	req.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()
	s.handleSessionTrace(w, req)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("09-gate: nil rpcCall status = %d, want 503", w.Result().StatusCode)
	}
}
