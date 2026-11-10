package builtin

// ledger_test.go — branch 02-surfaces TEST GATE (§1), leaf 02-surfaces/00T.
// Gates impl leaf 02-01 (internal/tools/builtin/ledger.go). Tests ONLY;
// RED until the implementation lands. Pinned shapes per the frozen
// contract (plan docs/plans/20261109-employee-ledger):
//
//   - LedgerAppendTool / LedgerQueryTool implement tools.Tool;
//     constructors NewLedgerAppendTool(*ledger.Store, *slog.Logger) and
//     NewLedgerQueryTool(*ledger.Store, *slog.Logger).
//   - RegisterLedgerTools(reg *tools.Registry, store *ledger.Store,
//     logger *slog.Logger) registers both; no package-init side effects.
//   - Tool names frozen: "ledger_append", "ledger_query". Category
//     "platform" (via tools.Categorizer).
//   - Parameters() carry NO employee_id and NO source property — the
//     employee identity is stamped SERVER-SIDE from ctx; source is fixed
//     to ledger.SourceTool for tool appends.
//   - Employee resolution mirrors the voice_call precedent via the
//     EXISTING exported ctx helper agent.ACPTurnSourceFromContext
//     (internal/agent/acp_executor.go:147) — no second ctx key.
//     Unattributed ctx refuses with a named error and writes NO row.
//   - ledger_query response shape: {"records": [...], "open": [...],
//     "head": <max id seen>} (00T §2 shares the shape; RPC leaf owns its
//     own tests).

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/tools"
)

// ledgerTestStore opens a throwaway real ledger store and registers
// Close on cleanup.
func ledgerTestStore(t *testing.T) *ledger.Store {
	t.Helper()
	st, err := ledger.Open(filepath.Join(t.TempDir(), "ledger.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// employeeCtx stamps the calling employee the way the daemon turn path
// does (agent.WithACPTurnSource), so tools resolve identity from ctx.
func employeeCtx(employeeID string) context.Context {
	return agent.WithACPTurnSource(context.Background(), agent.ACPTurnSourceEmployee, employeeID)
}

// --- compile-time interface satisfaction (pinned constructors) ---------

var _ tools.Tool = (*LedgerAppendTool)(nil)
var _ tools.Tool = (*LedgerQueryTool)(nil)

func TestLedgerToolConstructorsAndNames(t *testing.T) {
	st := ledgerTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	appendTool := NewLedgerAppendTool(st, logger)
	if appendTool.Name() != "ledger_append" {
		t.Errorf("LedgerAppendTool.Name() = %q, want %q", appendTool.Name(), "ledger_append")
	}
	if appendTool.Description() == "" {
		t.Error("ledger_append Description() must not be empty")
	}
	queryTool := NewLedgerQueryTool(st, logger)
	if queryTool.Name() != "ledger_query" {
		t.Errorf("LedgerQueryTool.Name() = %q, want %q", queryTool.Name(), "ledger_query")
	}
	if queryTool.Description() == "" {
		t.Error("ledger_query Description() must not be empty")
	}

	// Category "platform" for both (frozen in the contract block).
	if got := tools.GetCategory(appendTool); got != "platform" {
		t.Errorf("ledger_append category = %q, want %q", got, "platform")
	}
	if got := tools.GetCategory(queryTool); got != "platform" {
		t.Errorf("ledger_query category = %q, want %q", got, "platform")
	}
}

// --- §1 matrix: Parameters() schemas ---------------------------------

func TestLedgerAppendParametersSchema(t *testing.T) {
	st := ledgerTestStore(t)
	params := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler)).Parameters()
	if params.Type != "object" {
		t.Fatalf("Parameters Type = %q, want %q", params.Type, "object")
	}
	// Every pinned arg present.
	for _, key := range []string{"kind", "subject", "payload", "source_ref", "resolves_id", "set_status", "summary"} {
		if _, ok := params.Properties[key]; !ok {
			t.Errorf("ledger_append Parameters missing %q property", key)
		}
	}
	// FROZEN NEGATIVE: identity is server-stamped, never an argument.
	for _, banned := range []string{"employee_id", "source"} {
		if _, ok := params.Properties[banned]; ok {
			t.Errorf("ledger_append Parameters MUST NOT expose %q (server-stamped)", banned)
		}
	}
	// kind is the closed set (mirrors the schema CHECK).
	if kind, ok := params.Properties["kind"]; ok {
		want := map[string]bool{"committal": true, "action": true, "observation": true, "commitment": true, "resolution": true}
		if len(kind.Enum) != len(want) {
			t.Errorf("kind enum = %v, want the 5-member closed set", kind.Enum)
		}
		for _, e := range kind.Enum {
			if !want[e] {
				t.Errorf("kind enum member %q outside the closed set", e)
			}
		}
	}
}

func TestLedgerQueryParametersSchema(t *testing.T) {
	st := ledgerTestStore(t)
	params := NewLedgerQueryTool(st, slog.New(slog.DiscardHandler)).Parameters()
	if params.Type != "object" {
		t.Fatalf("Parameters Type = %q, want %q", params.Type, "object")
	}
	for _, key := range []string{"mode", "subject", "since_id", "limit"} {
		if _, ok := params.Properties[key]; !ok {
			t.Errorf("ledger_query Parameters missing %q property", key)
		}
	}
	for _, banned := range []string{"employee_id", "source"} {
		if _, ok := params.Properties[banned]; ok {
			t.Errorf("ledger_query Parameters MUST NOT expose %q (server-stamped)", banned)
		}
	}
	if mode, ok := params.Properties["mode"]; ok {
		if len(mode.Enum) != 2 || mode.Enum[0] != "log" || mode.Enum[1] != "open" {
			// set-order tolerant:
			joined := strings.Join(mode.Enum, ",")
			if !strings.Contains(joined, "log") || !strings.Contains(joined, "open") || len(mode.Enum) != 2 {
				t.Errorf("mode enum = %v, want [log open]", mode.Enum)
			}
		}
	}
}

// --- §1 matrix: Execute ctx resolution ---------------------------------

func TestLedgerAppendExecute_UnattributedCtxRefusesNoRow(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))

	// No turn stamp at all (chat lane): named refusal, no write.
	res, err := tool.Execute(context.Background(), map[string]any{
		"kind":    "observation",
		"subject": "acme-roof",
		"payload": map[string]any{"note": "sneaky"},
	})
	if err != nil {
		t.Fatalf("Execute returned Go error (want ToolResult refusal): %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok {
		t.Fatalf("Execute result type = %T, want *tools.ToolResult", res)
	}
	if tr.Success {
		t.Fatalf("unattributed ctx must refuse, got success: %+v", tr.Result)
	}
	if !strings.Contains(tr.Error, "ledger_append") {
		t.Errorf("refusal must be nameable: Error = %q, want it to name ledger_append", tr.Error)
	}
	// stamped-but-empty employee id: same refusal (attributable only to an
	// employee).
	res2, err := tool.Execute(employeeCtx(""), map[string]any{
		"kind":    "observation",
		"subject": "acme-roof",
		"payload": map[string]any{"note": "x"},
	})
	if err != nil {
		t.Fatalf("Execute(empty emp id) error = %v", err)
	}
	if tr2, ok := res2.(*tools.ToolResult); !ok || tr2.Success {
		t.Fatalf("empty employee id must refuse: %+v", res2)
	}
}

func TestLedgerAppendExecute_HappyPathServerStampsSourceAndEmployee(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))

	res, err := tool.Execute(employeeCtx("eve"), map[string]any{
		"kind":    "action",
		"subject": "acme-roof",
		"payload": map[string]any{"did": "ordered shingles"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || !tr.Success {
		t.Fatalf("want success ToolResult, got %+v (%T)", res, res)
	}

	rows, err := st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 stored row, got %d", len(rows))
	}
	rec := rows[0]
	// Identity + source are SERVER-assigned, never client-supplied.
	if rec.EmployeeID != "eve" {
		t.Errorf("EmployeeID = %q, want server stamp %q", rec.EmployeeID, "eve")
	}
	if rec.Source != ledger.SourceTool {
		t.Errorf("Source = %q, want fixed %q", rec.Source, ledger.SourceTool)
	}
	// The payload object is stored JSON-encoded.
	var got map[string]any
	if err := json.Unmarshal(rec.Payload, &got); err != nil {
		t.Fatalf("stored payload not valid JSON: %v (raw %q)", err, rec.Payload)
	}
	if got["did"] != "ordered shingles" {
		t.Errorf("payload round-trip = %v, want did=ordered shingles", got)
	}
}

func TestLedgerAppendExecute_KindClosedSetRejected(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))

	res, err := tool.Execute(employeeCtx("eve"), map[string]any{
		"kind":    "gossip",
		"subject": "acme-roof",
		"payload": map[string]any{"a": 1},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || tr.Success {
		t.Fatalf("invalid kind must refuse: %+v", res)
	}
	rows, err := st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rejected append must write ZERO rows, got %d", len(rows))
	}
}

func TestLedgerAppendExecute_ResolvesIDPassthrough(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	first, err := tool.Execute(ctx, map[string]any{
		"kind":    "commitment",
		"subject": "acme-roof",
		"payload": map[string]any{"owe": "estimate"},
	})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if tr, ok := first.(*tools.ToolResult); !ok || !tr.Success {
		t.Fatalf("first append must succeed: %+v", first)
	}
	rows, err := st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("want 1 row, got %d (err %v)", len(rows), err)
	}
	targetID := rows[0].ID

	// JSON-decoded numbers arrive as float64 at the tool boundary.
	res, err := tool.Execute(ctx, map[string]any{
		"kind":        "resolution",
		"subject":     "acme-roof",
		"payload":     map[string]any{"sent": "estimate"},
		"resolves_id": float64(targetID),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || !tr.Success {
		t.Fatalf("resolves append must succeed: %+v", res)
	}
	rows, err = st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d (err %v)", len(rows), err)
	}
	if rows[1].Resolves != targetID {
		t.Errorf("Resolves = %d, want numeric passthrough %d", rows[1].Resolves, targetID)
	}
}

func TestLedgerAppendExecute_SetStatusSummaryPaired(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	base := map[string]any{
		"kind":    "action",
		"subject": "acme-roof",
		"payload": map[string]any{"a": 1},
	}
	// set_status WITHOUT summary -> validation error.
	withStatus := map[string]any{"set_status": "done"}
	for k, v := range base {
		withStatus[k] = v
	}
	res, err := tool.Execute(ctx, withStatus)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || tr.Success {
		t.Fatalf("set_status without summary must refuse: %+v", res)
	}

	// summary WITHOUT set_status -> validation error.
	withSummary := map[string]any{"summary": "roof replaced"}
	for k, v := range base {
		withSummary[k] = v
	}
	res, err = tool.Execute(ctx, withSummary)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || tr.Success {
		t.Fatalf("summary without set_status must refuse: %+v", res)
	}

	rows, err := st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("validation refusals must write ZERO rows, got %d", len(rows))
	}

	// Both together -> success AND the state row lands.
	both := map[string]any{"set_status": "open", "summary": "quote requested"}
	for k, v := range base {
		both[k] = v
	}
	res, err = tool.Execute(ctx, both)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || !tr.Success {
		t.Fatalf("set_status+summary must succeed: %+v", res)
	}
	stRow, ok, err := st.GetState(context.Background(), "eve", "acme-roof")
	if err != nil || !ok {
		t.Fatalf("state row expected after set_status append (found=%v err=%v)", ok, err)
	}
	if stRow.Status != ledger.StatusOpen || stRow.Summary != "quote requested" {
		t.Errorf("state = %+v, want status=open summary=%q", stRow, "quote requested")
	}
}

func TestLedgerAppendExecute_LegalTransitionRejectionPropagates(t *testing.T) {
	st := ledgerTestStore(t)
	tool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	if _, err := tool.Execute(ctx, map[string]any{
		"kind": "action", "subject": "acme-roof", "payload": map[string]any{},
		"set_status": "done", "summary": "done",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// done -> cancelled is outside AllowedTransitions.
	res, err := tool.Execute(ctx, map[string]any{
		"kind": "action", "subject": "acme-roof", "payload": map[string]any{},
		"set_status": "cancelled", "summary": "nope",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tr, ok := res.(*tools.ToolResult); !ok || tr.Success {
		t.Fatalf("illegal transition must refuse: %+v", res)
	}
	if st2, ok2, _ := st.GetState(context.Background(), "eve", "acme-roof"); !ok2 || st2.Status != ledger.StatusDone {
		t.Errorf("rejected transition must leave state untouched, got %+v (found=%v)", st2, ok2)
	}
}

// --- §1 matrix: ledger_query Execute -----------------------------------

func TestLedgerQueryExecute_LogAndOpenModes(t *testing.T) {
	st := ledgerTestStore(t)
	appendTool := NewLedgerAppendTool(st, slog.New(slog.DiscardHandler))
	queryTool := NewLedgerQueryTool(st, slog.New(slog.DiscardHandler))
	ctx := employeeCtx("eve")

	if _, err := appendTool.Execute(ctx, map[string]any{
		"kind": "commitment", "subject": "roof-quote", "payload": map[string]any{},
		"set_status": "open", "summary": "owe estimate",
	}); err != nil {
		t.Fatalf("seed append: %v", err)
	}
	if _, err := appendTool.Execute(ctx, map[string]any{
		"kind": "observation", "subject": "weather", "payload": map[string]any{},
	}); err != nil {
		t.Fatalf("log-only append: %v", err)
	}

	// mode=log -> records ride the store's QueryLog.
	res, err := queryTool.Execute(ctx, map[string]any{"mode": "log"})
	if err != nil {
		t.Fatalf("Execute mode=log: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("mode=log must succeed: %+v", res)
	}
	shape := decodeQueryShape(t, tr.Result)
	if len(shape.Records) != 2 {
		t.Errorf("mode=log records = %d, want 2", len(shape.Records))
	}
	if shape.Head < 2 {
		t.Errorf("head = %d, want the max id seen (>=2)", shape.Head)
	}

	// mode=open -> open state rows (log-only row created no state).
	res, err = queryTool.Execute(ctx, map[string]any{"mode": "open"})
	if err != nil {
		t.Fatalf("Execute mode=open: %v", err)
	}
	tr, ok = res.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("mode=open must succeed: %+v", res)
	}
	shape = decodeQueryShape(t, tr.Result)
	if len(shape.Open) != 1 || !strings.Contains(string(shape.Open[0]), "roof-quote") {
		t.Errorf("mode=open rows = %v, want exactly the open roof-quote subject", shape.Open)
	}

	// since_id passthrough.
	rows, _ := st.QueryLog(context.Background(), ledger.LogQuery{EmployeeID: "eve"})
	res, err = queryTool.Execute(ctx, map[string]any{"mode": "log", "since_id": float64(rows[0].ID)})
	if err != nil {
		t.Fatalf("Execute since_id: %v", err)
	}
	tr, ok = res.(*tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("since_id query must succeed: %+v", res)
	}
	shape = decodeQueryShape(t, tr.Result)
	if len(shape.Records) != 1 {
		t.Errorf("since_id=%d: records = %d, want 1", rows[0].ID, len(shape.Records))
	}
}

// queryShape mirrors the frozen response envelope
// {"records":[...],"open":[...],"head":int64} loosely — arrays decoded as
// raw JSON, head as number — so the gate asserts the wire shape, not the
// Go type the impl picks.
type queryShape struct {
	Records []json.RawMessage `json:"records"`
	Open    []json.RawMessage `json:"open"`
	Head    int64             `json:"head"`
}

func decodeQueryShape(t *testing.T, result any) queryShape {
	t.Helper()
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal tool result: %v", err)
	}
	var q queryShape
	if err := json.Unmarshal(b, &q); err != nil {
		t.Fatalf("result %s does not decode as {records,open,head}: %v", b, err)
	}
	// keys must be PRESENT (nil arrays decode to absent -> explicit check).
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	for _, k := range []string{"records", "open", "head"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("response missing key %q: %s", k, b)
		}
	}
	return q
}

// --- §1 matrix: registration smoke --------------------------------------

func TestRegisterLedgerTools(t *testing.T) {
	st := ledgerTestStore(t)
	reg := tools.NewRegistry(slog.New(slog.DiscardHandler))

	RegisterLedgerTools(reg, st, slog.New(slog.DiscardHandler))

	for _, name := range []string{"ledger_append", "ledger_query"} {
		tool := reg.Get(name)
		if tool == nil {
			t.Fatalf("RegisterLedgerTools must register %q", name)
		}
		if got := tools.GetCategory(tool); got != "platform" {
			t.Errorf("%q category = %q, want platform", name, got)
		}
	}
	// LLM definitions carry both (schema surface the model sees).
	defs := reg.ToLLMDefinitions()
	found := map[string]bool{}
	for _, d := range defs {
		found[d.Function.Name] = true
	}
	for _, name := range []string{"ledger_append", "ledger_query"} {
		if !found[name] {
			t.Errorf("ToLLMDefinitions missing %q", name)
		}
	}
}
