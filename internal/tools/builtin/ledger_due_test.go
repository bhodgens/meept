package builtin

// 02-daystate/01 — tool-level due arg tests (NEW file; ledger_test.go's
// existing pins stay untouched per the leaf contract). Covers the gate's
// §C tool posture: ledger_append's optional `due` string (full RFC3339
// used as-is, date-only normalized to 12:00 UTC same day, invalid
// refused with zero rows), and ledger_query mode=open surfacing the
// state row's due when set (omitted when unset — pre-extension wire
// shapes stay byte-identical).

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/tools"
)

func execAppendOK(t *testing.T, tool *LedgerAppendTool, ctx context.Context, args map[string]any) LedgerAppendResult {
	t.Helper()
	res, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("append must succeed: %+v", res)
	}
	out, ok := tr.Result.(LedgerAppendResult)
	if !ok {
		t.Fatalf("result = %T, want LedgerAppendResult", tr.Result)
	}
	return out
}

func TestLedgerAppendDue_RFC3339UsedAsIs(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	out := execAppendOK(t, tool, ctx, map[string]any{
		"kind": "commitment", "subject": "roof-quote",
		"set_status": "open", "summary": "owe estimate",
		"due": due.Format(time.RFC3339),
	})
	if out.Due != due.Format(time.RFC3339) {
		t.Errorf("result due = %q, want echo %q", out.Due, due.Format(time.RFC3339))
	}
	stRow, found, err := st.GetState(context.Background(), "eve", "roof-quote")
	if err != nil || !found || !stRow.Due.Equal(due) {
		t.Fatalf("state Due = %v (found=%v err=%v); want %v", stRow.Due, found, err, due)
	}
}

func TestLedgerAppendDue_DateOnlyNormalizesToNoonUTC(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	out := execAppendOK(t, tool, ctx, map[string]any{
		"kind": "commitment", "subject": "fence",
		"set_status": "open", "summary": "build fence",
		"due": "2026-11-15",
	})
	want := "2026-11-15T12:00:00Z"
	if out.Due != want {
		t.Errorf("date-only due echo = %q, want %q (12:00 UTC same day)", out.Due, want)
	}
	stRow, _, err := st.GetState(context.Background(), "eve", "fence")
	if err != nil || !stRow.Due.Equal(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("state Due = %v err=%v; want 2026-11-15T12:00:00Z", stRow.Due, err)
	}
}

func TestLedgerAppendDue_InvalidRefusesNoRow(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	res, err := tool.Execute(ctx, map[string]any{
		"kind": "commitment", "subject": "broken",
		"set_status": "open", "summary": "s",
		"due": "friday-ish",
	})
	if err != nil {
		t.Fatalf("Execute returned Go error (want refusal): %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || tr.Success {
		t.Fatalf("invalid due must refuse: %+v", res)
	}
	rows, err := st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	if err != nil || len(rows) != 0 {
		t.Errorf("refused append must write zero rows: %d rows err=%v", len(rows), err)
	}
}

func TestLedgerAppendDue_WithoutSetStatusRefuses(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	res, err := tool.Execute(ctx, map[string]any{
		"kind": "observation", "subject": "noted",
		"due": "2026-11-15",
	})
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || tr.Success {
		t.Fatalf("due without set_status must refuse: %+v", res)
	}
	rows, err := st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	if err != nil || len(rows) != 0 {
		t.Errorf("refused append must write zero rows: %d rows err=%v", len(rows), err)
	}
}

func TestLedgerQueryOpen_SurfacesDueAndOmitsWhenUnset(t *testing.T) {
	st := ledgerTestStore(t)
	appendTool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	queryTool := NewLedgerQueryTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	due := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	execAppendOK(t, appendTool, ctx, map[string]any{
		"kind": "commitment", "subject": "with-due",
		"set_status": "open", "summary": "owed",
		"due": due.Format(time.RFC3339),
	})
	execAppendOK(t, appendTool, ctx, map[string]any{
		"kind": "commitment", "subject": "no-due",
		"set_status": "open", "summary": "owed plain",
	})

	res, err := queryTool.Execute(ctx, map[string]any{"mode": "open"})
	if err != nil {
		t.Fatalf("Execute mode=open: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if !tr.Success {
		t.Fatalf("mode=open must succeed: %+v", res)
	}
	b, err := json.Marshal(tr.Result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var shape queryShape
	if err := json.Unmarshal(b, &shape); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(shape.Open) != 2 {
		t.Fatalf("open rows = %d, want 2: %s", len(shape.Open), b)
	}
	var dueRow, plainRow string
	for _, raw := range shape.Open {
		s := string(raw)
		if strings.Contains(s, "with-due") {
			dueRow = s
		} else {
			plainRow = s
		}
	}
	if !strings.Contains(dueRow, due.Format(time.RFC3339)) {
		t.Errorf("with-due row missing due in open output: %s", dueRow)
	}
	if strings.Contains(plainRow, `"due"`) {
		t.Errorf("no-due row must omit the due key (omitempty posture): %s", plainRow)
	}
}
