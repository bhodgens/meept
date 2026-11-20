package session

// 03-context TEST-GATE T3 (docs/plans/20261119-usability-remediation/
// 03-context/00T-gate.md, master contract C4; W11 tool-call read surface).
//
// session.messages.get gains include_tool_calls (bool, default false):
//   - true: each message row carries tool_calls joined by message id
//     (GetToolCallsForMessages, store.go:134 - engine-side only today),
//     wire shape additive: []{id, name, args_summary, result_summary, at}.
//   - false/absent: the response is BYTE-IDENTICAL to the legacy shape
//     (no tool_calls key anywhere) - the frozen default pin.
//
// RED today: handleGetMessages (session.go:2152) parses only
// session_id/offset/limit and returns messages+total; Message has no
// ToolCalls field (store.go:12-27), so include_tool_calls=true returns
// rows with no tool-call surface. GREEN after 03-context/01. The legacy
// default arm passes today and must keep passing byte-for-byte.

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// newToolCallsGateFixture persists one exchange with two tool calls keyed
// to the assistant row through the real SQLite store, and returns the
// handler plus the assigned message ids.
func newToolCallsGateFixture(t *testing.T) (*Handler, *SQLiteStore, int64, int64) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sessions.db"), slog.Default())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	sess, err := store.Create("gate-toolcalls")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC()
	msgs := []Message{
		{SessionID: sess.ID, Role: "user", Content: "list the directory", Timestamp: now, EntryType: "message", BranchID: "main"},
		{SessionID: sess.ID, Role: "assistant", Content: "done listing", Timestamp: now, EntryType: "message", BranchID: "main"},
	}
	toolCalls := map[int][]ToolCall{
		1: {
			{ToolName: "shell", ToolCallID: "gate-call-1", Arguments: `{"command":"ls"}`, Result: "a.txt b.txt", Seq: 0},
			{ToolName: "shell", ToolCallID: "gate-call-2", Arguments: `{"command":"pwd"}`, Result: "/tmp/gate", Seq: 1},
		},
	}
	if err := store.SaveMessagesWithToolCalls(sess.ID, msgs, toolCalls); err != nil {
		t.Fatalf("SaveMessagesWithToolCalls: %v", err)
	}
	saved, err := store.GetMessages(sess.ID, 0, 10)
	if err != nil || len(saved) != 2 {
		t.Fatalf("fixture readback: %v (n=%d)", err, len(saved))
	}
	h := NewHandler(store, nil, slog.Default())
	return h, store, saved[0].ID, saved[1].ID
}

// gateGetMessages drives the real session.messages.get handler closure.
func gateGetMessages(t *testing.T, h *Handler, params string) map[string]any {
	t.Helper()
	resp, err := h.handleGetMessages(&models.BusMessage{Payload: []byte(params)})
	if err != nil {
		t.Fatalf("handleGetMessages(%s): %v", params, err)
	}
	m, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("response type %T, want map[string]any", resp)
	}
	return m
}

// TestIncludeToolCalls pins the C4 read surface: include_tool_calls=true
// joins the persisted tool calls onto their message rows; the default
// stays byte-identical to the legacy shape.
func TestIncludeToolCalls(t *testing.T) {
	h, store, userID, assistantID := newToolCallsGateFixture(t)
	// Resolve the session id through the store's own read path.
	sessions, err := store.List()
	if err != nil || len(sessions) != 1 {
		t.Fatalf("list sessions: %v (n=%d)", err, len(sessions))
	}
	sessID := sessions[0].ID

	// Engine-side precondition (passes today): the tool calls ARE
	// persisted and readable by message id - the gap is the RPC surface.
	byID, err := store.GetToolCallsForMessages([]int64{assistantID})
	if err != nil || len(byID[assistantID]) != 2 {
		t.Fatalf("engine-side tool calls missing: %v (n=%d)", err, len(byID[assistantID]))
	}

	t.Run("include_tool_calls=true joins rows by message id", func(t *testing.T) {
		resp := gateGetMessages(t, h, `{"session_id":"`+sessID+`","include_tool_calls":true}`)
		raw, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		var decoded struct {
			Messages []struct {
				ID        int64  `json:"id"`
				Role      string `json:"role"`
				ToolCalls []struct {
					ID            any    `json:"id"`
					Name          string `json:"name"`
					ArgsSummary   string `json:"args_summary"`
					ResultSummary string `json:"result_summary"`
					At            any    `json:"at"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(decoded.Messages) != 2 {
			t.Fatalf("want 2 message rows, got %d", len(decoded.Messages))
		}
		var assistantRow *struct {
			ID        int64  `json:"id"`
			Role      string `json:"role"`
			ToolCalls []struct {
				ID            any    `json:"id"`
				Name          string `json:"name"`
				ArgsSummary   string `json:"args_summary"`
				ResultSummary string `json:"result_summary"`
				At            any    `json:"at"`
			} `json:"tool_calls"`
		}
		for i := range decoded.Messages {
			if decoded.Messages[i].ID == assistantID {
				assistantRow = &decoded.Messages[i]
			}
			if decoded.Messages[i].ID == userID && len(decoded.Messages[i].ToolCalls) != 0 {
				t.Errorf("user row carries tool calls; calls must join their OWN message id only")
			}
		}
		if assistantRow == nil {
			t.Fatalf("assistant row %d absent from response: %s", assistantID, raw)
		}
		if len(assistantRow.ToolCalls) != 2 {
			t.Fatalf("RED: include_tool_calls=true returned %d tool calls on the assistant row, want 2 (messages.get omits tool calls today); response: %s",
				len(assistantRow.ToolCalls), raw)
		}
		for i, tc := range assistantRow.ToolCalls {
			if tc.Name != "shell" {
				t.Errorf("tool call %d name = %q, want shell (C4 wire key 'name')", i, tc.Name)
			}
			if tc.ArgsSummary == "" || tc.ResultSummary == "" {
				t.Errorf("tool call %d missing args_summary/result_summary (C4 wire shape): %+v", i, tc)
			}
			if tc.ID == nil || tc.At == nil {
				t.Errorf("tool call %d missing id/at (C4 wire shape): %+v", i, tc)
			}
		}
	})

	t.Run("default false is byte-identical legacy", func(t *testing.T) {
		// The legacy shape: exactly messages+total, no tool_calls key on
		// any row. Absent param and explicit false must be identical.
		absent := gateGetMessages(t, h, `{"session_id":"`+sessID+`"}`)
		explicit := gateGetMessages(t, h, `{"session_id":"`+sessID+`","include_tool_calls":false}`)
		absentRaw, err := json.Marshal(absent)
		if err != nil {
			t.Fatalf("marshal absent: %v", err)
		}
		explicitRaw, err := json.Marshal(explicit)
		if err != nil {
			t.Fatalf("marshal explicit: %v", err)
		}
		if string(absentRaw) != string(explicitRaw) {
			t.Fatalf("explicit include_tool_calls=false changed the legacy bytes:\nabsent:   %s\nexplicit: %s", absentRaw, explicitRaw)
		}
		if len(absent) != 2 {
			t.Fatalf("legacy response key set changed: %v", absent)
		}
		for _, key := range []string{"messages", "total"} {
			if _, ok := absent[key]; !ok {
				t.Fatalf("legacy response missing %q", key)
			}
		}
		// No tool_calls key anywhere in the default response (additive
		// contract: the key appears ONLY when requested).
		var rows []map[string]any
		msgsRaw, err := json.Marshal(absent["messages"])
		if err != nil {
			t.Fatalf("marshal messages: %v", err)
		}
		if err := json.Unmarshal(msgsRaw, &rows); err != nil {
			t.Fatalf("decode messages: %v", err)
		}
		for i, row := range rows {
			if _, ok := row["tool_calls"]; ok {
				t.Fatalf("legacy default leaked tool_calls on row %d (must be byte-identical pre-change): %s", i, absentRaw)
			}
		}
	})
}
