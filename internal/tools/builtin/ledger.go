package builtin

// ledger.go (plan 20261109-employee-ledger, leaf 02-surfaces/01): the
// employee-callable ledger tools — ledger_append and ledger_query. The
// tools are a THIN argument shim over internal/ledger.Store: kind/source
// closed-set validation, the AllowedTransitions state machine, the
// resolves check, the payload envelope, and the state upsert all live in
// Store.Append (internal/ledger/store.go); this file only validates
// argument shapes, stamps identity SERVER-SIDE, and shapes responses.
//
// Frozen contract highlights (02-surfaces/01-tools.md §Interface
// Contracts, mirrored by the gate tests in ledger_test.go):
//   - Tool names "ledger_append" / "ledger_query"; category "platform".
//   - Parameters() carry NO employee_id and NO source property: the
//     employee identity resolves from ctx via the EXISTING exported
//     helper agent.ACPTurnSourceFromContext (the voice_call precedent —
//     no second ctx key); source is fixed to ledger.SourceTool.
//   - Unattributed ctx (chat lane, or stamped with an empty employee id)
//     refuses with a named error and writes NO row.
//   - ledger_query response shape: {"records": [...], "open": [...],
//     "head": <max id seen>}.
//
// Registration goes through RegisterLedgerTools (called by the 03-01
// daemon wiring); there are no package-init side effects.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/callerid"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ledgerSourceTool is the fixed writer identity for every tool append —
// never an argument (frozen negative in the gate schema tests).
const ledgerSourceTool = ledger.SourceTool

// resolveLedgerEmployee returns the calling employee's id from the turn
// context. ok=false means the turn is not attributable to an employee
// (chat lane, or an employee-kind stamp with an empty id) and the tool
// must refuse — exactly the voice_call fail-closed rule.
func resolveLedgerEmployee(ctx context.Context) (string, bool) {
	kind, employeeID, ok := agent.ACPTurnSourceFromContext(ctx)
	if !ok || kind != agent.ACPTurnSourceEmployee || employeeID == "" {
		return "", false
	}
	return employeeID, true
}

// ledgerArgStr reads a string argument (absent or wrong type -> "").
func ledgerArgStr(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

// ledgerArgInt reads a numeric argument. JSON-decoded numbers arrive as
// float64 at the tool boundary; int/int64/json.Number are tolerated for
// direct Go callers. ok=false means the key is absent; a present-but-
// non-numeric value reports ok=false with seen=true so callers can
// refuse rather than silently drop an id.
func ledgerArgInt(args map[string]any, key string) (int64, bool, bool) {
	v, present := args[key]
	if !present {
		return 0, false, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true, false
	case int:
		return int64(n), true, false
	case int64:
		return n, true, false
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false, true
		}
		return i, true, true
	default:
		return 0, false, true
	}
}

// =========================================================================
// ledger_append
// =========================================================================

// LedgerAppendTool appends one immutable record (and optionally moves a
// subject's state) to the calling employee's ledger.
type LedgerAppendTool struct {
	tools.ToolDefaults
	store  *ledger.Store
	logger *slog.Logger
}

// NewLedgerAppendTool constructs the append tool over a live store.
func NewLedgerAppendTool(store *ledger.Store, logger *slog.Logger) *LedgerAppendTool {
	if logger == nil {
		logger = slog.Default()
	}
	return &LedgerAppendTool{store: store, logger: logger}
}

func (t *LedgerAppendTool) Name() string { return "ledger_append" }

func (t *LedgerAppendTool) Category() string { return "platform" }

func (t *LedgerAppendTool) Description() string {
	return "Append one record to YOUR OWN employee ledger (the append-only memory of what you did, observed, promised, and owe). kind: committal (what was said/promised), action (something done), observation (noted fact, no state implication), commitment (an obligation you owe — opens work), resolution (closes/answers a commitment — pass resolves_id = the commitment's ledger id). subject namespaces the record (e.g. \"acme-roof\"); payload is any JSON object describing the record. Optionally set_status (open|done|cancelled|waiting_on_me|waiting_on_them) plus summary (one-line current truth) to move the subject's state — these two must always travel together, and the transition is rejected if the ledger state machine forbids it. Your identity is stamped automatically; it is never an argument. Returns the new record id."
}

func (t *LedgerAppendTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"kind": {
				Type:        "string",
				Description: "Record kind (closed set).",
				Enum: []string{
					string(ledger.KindCommittal),
					string(ledger.KindAction),
					string(ledger.KindObservation),
					string(ledger.KindCommitment),
					string(ledger.KindResolution),
				},
			},
			"subject": {
				Type:        "string",
				Description: "Namespace key for what this record is about, e.g. \"acme-roof\".",
			},
			"payload": {
				Type:        "object",
				Description: "Free-form JSON object with the record's detail (stored verbatim when no set_status is used; wrapped in the ledger envelope when the append also moves state).",
			},
			"source_ref": {
				Type:        "string",
				Description: "Optional pointer to the originating artefact (call id, turn id, task id).",
			},
			"resolves_id": {
				Type:        "integer",
				Description: "Optional ledger id of a commitment this resolution closes. Must reference one of YOUR OWN records.",
			},
			"set_status": {
				Type:        "string",
				Description: "Optional: move the subject's state to this status. Requires summary. Rejected when the transition is illegal (e.g. done -> cancelled).",
				Enum: []string{
					string(ledger.StatusOpen),
					string(ledger.StatusDone),
					string(ledger.StatusCancelled),
					string(ledger.StatusWaitingMe),
					string(ledger.StatusWaitingThem),
				},
			},
			"summary": {
				Type:        "string",
				Description: "One-line current truth for the subject. Only valid together with set_status.",
			},
		},
		Required: []string{"kind"},
	}
}

// LedgerAppendResult is the success payload for ledger_append.
type LedgerAppendResult struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Subject    string `json:"subject"`
	Resolves   int64  `json:"resolves,omitempty"`
	Status     string `json:"status,omitempty"`
	Summary    string `json:"summary,omitempty"`
	RecordedAt string `json:"recorded_at"`
}

func (t *LedgerAppendTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	employeeID, ok := resolveLedgerEmployee(ctx)
	if !ok {
		return tools.NewErrorResult("ledger_append: no employee identity on this turn — the ledger is attributable only to an employee"), nil
	}
	if t.store == nil {
		return tools.NewErrorResult("ledger_append: ledger store not wired"), nil
	}

	kindStr := ledgerArgStr(args, "kind")
	subject := ledgerArgStr(args, "subject")

	// payload: any JSON value; the contract nominates an object. Absent ->
	// no payload (the store allows a nil one); present but non-marshalable
	// -> refusal, zero rows.
	var payload []byte
	if raw, present := args["payload"]; present && raw != nil {
		b, err := json.Marshal(raw)
		if err != nil {
			return tools.NewErrorResult(fmt.Sprintf("ledger_append: payload must be a JSON object: %v", err)), nil
		}
		payload = b
	}

	resolves, hasResolves, badResolves := ledgerArgInt(args, "resolves_id")
	if badResolves {
		return tools.NewErrorResult("ledger_append: resolves_id must be a ledger record id (integer)"), nil
	}

	setStatus := ledgerArgStr(args, "set_status")
	summary := ledgerArgStr(args, "summary")

	// Q1 stranger-lane gate (owner decision, plan 20261110-caller-authz):
	// an UNAPPROVED voice caller may RECORD on the message lane — that is
	// exactly what step 1c was widened for — but must never move a
	// subject's STATE. Step 1c adjudicates on the tool NAME only, so the
	// payload-aware check lives here, at the tool boundary, where the
	// executor's callerid stamp (callerid.With at the daemon's per-turn
	// site) is already visible. Absent stamp (ok=false) means a non-voice
	// turn: the gate is inert, behavior byte-identical to before. A set
	// stamp with Approved=false (unknown or unapproved number) plus a
	// non-empty set_status refuses the WHOLE call — nothing is written,
	// the model is told to re-append without set_status so the note still
	// records. Provenance of the refused attempt is this WARN only.
	if caller, hasCaller := callerid.From(ctx); hasCaller && !caller.Approved && setStatus != "" {
		t.logger.Warn("ledger_append refused set_status from unapproved voice caller",
			"employee_id", employeeID,
			"caller_known", caller.Known,
			"subject", subject,
			"set_status", setStatus,
		)
		return tools.NewErrorResult("ledger_append: caller is not approved to change subject state; drop set_status/summary and re-append — message recorded instead, subject state not changed"), nil
	}

	// Frozen pairing rule: the two travel together or not at all.
	if setStatus != "" && summary == "" {
		return tools.NewErrorResult("ledger_append: set_status requires a summary (one-line current truth)"), nil
	}
	if summary != "" && setStatus == "" {
		return tools.NewErrorResult("ledger_append: summary is only valid together with set_status"), nil
	}

	d := ledger.DraftRecord{
		EmployeeID: employeeID,
		Kind:       ledger.Kind(kindStr),
		Subject:    subject,
		Payload:    payload,
		Source:     ledgerSourceTool,
		SourceRef:  ledgerArgStr(args, "source_ref"),
	}
	if hasResolves {
		d.Resolves = resolves
	}
	if setStatus != "" {
		d.SetStatus = ledger.Status(setStatus)
		d.Summary = summary
	}

	rec, err := t.store.Append(ctx, d)
	if err != nil {
		// Kind/source/transition/resolves closed-set violations all ride
		// this path — policy stays in the store, the tool only reports.
		return tools.NewErrorResultErr(fmt.Errorf("ledger_append refused: %w", err)), nil
	}

	out := LedgerAppendResult{
		ID:         rec.ID,
		Kind:       string(rec.Kind),
		Subject:    rec.Subject,
		Resolves:   rec.Resolves,
		RecordedAt: rec.At.Format("2006-01-02T15:04:05Z07:00"),
	}
	if d.SetStatus != "" {
		out.Status = string(d.SetStatus)
		out.Summary = d.Summary
	}
	return tools.NewSuccessResult(out), nil
}

// =========================================================================
// ledger_query
// =========================================================================

// LedgerQueryTool reads the calling employee's ledger: recent log rows
// (mode=log, optional subject / since_id / text filters) or the currently
// open subject states (mode=open).
type LedgerQueryTool struct {
	tools.ToolDefaults
	store  *ledger.Store
	logger *slog.Logger
}

// NewLedgerQueryTool constructs the query tool over a live store.
func NewLedgerQueryTool(store *ledger.Store, logger *slog.Logger) *LedgerQueryTool {
	if logger == nil {
		logger = slog.Default()
	}
	return &LedgerQueryTool{store: store, logger: logger}
}

func (t *LedgerQueryTool) Name() string { return "ledger_query" }

func (t *LedgerQueryTool) Category() string { return "platform" }

func (t *LedgerQueryTool) Description() string {
	return "Read YOUR OWN employee ledger. mode=\"log\" returns matching append-only records in id order — optionally narrowed to one subject, to records after since_id (for incremental tailing), or by a text phrase matched against subject and payload. mode=\"open\" returns the subjects whose state is open / waiting_on_me / waiting_on_them, newest first. The response always carries {\"records\": [...], \"open\": [...], \"head\": <max ledger id seen>}; head is your watermark for future since_id calls. Your identity is stamped automatically; you can only ever see your own rows."
}

func (t *LedgerQueryTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"mode": {
				Type:        "string",
				Description: "\"log\" for append-only records, \"open\" for currently-open subject states.",
				Enum:        []string{"log", "open"},
			},
			"subject": {
				Type:        "string",
				Description: "Optional (mode=log): restrict to one subject namespace key.",
			},
			"since_id": {
				Type:        "integer",
				Description: "Optional (mode=log): only records with id greater than this watermark (use the head from a previous query).",
			},
			"text": {
				Type:        "string",
				Description: "Optional (mode=log): phrase matched against subject and payload snippet via the ledger FTS index.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional (mode=log): max records to return (default 200, max 1000).",
			},
		},
		Required: []string{"mode"},
	}
}

// ledgerQueryResult is the frozen response envelope. Keys are always
// present (empty slices serialize as [], never null/absent).
type ledgerQueryResult struct {
	Records []ledgerRecordView `json:"records"`
	Open    []ledgerStateView  `json:"open"`
	Head    int64              `json:"head"`
}

// ledgerRecordView is the wire shape of one log row. payload is
// json.RawMessage so the stored JSON rides through unquoted.
type ledgerRecordView struct {
	ID         int64           `json:"id"`
	At         string          `json:"at"`
	Kind       string          `json:"kind"`
	Subject    string          `json:"subject"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Source     string          `json:"source"`
	SourceRef  string          `json:"source_ref,omitempty"`
	Resolves   int64           `json:"resolves,omitempty"`
	EmployeeID string          `json:"-"` // ctx-stamped scope; never echoed
}

// ledgerStateView is the wire shape of one ledger_state row.
type ledgerStateView struct {
	Subject   string `json:"subject"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	UpdatedAt string `json:"updated_at"`
	AsOf      int64  `json:"as_of"`
}

func (t *LedgerQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	employeeID, ok := resolveLedgerEmployee(ctx)
	if !ok {
		return tools.NewErrorResult("ledger_query: no employee identity on this turn — the ledger is readable only by its employee"), nil
	}
	if t.store == nil {
		return tools.NewErrorResult("ledger_query: ledger store not wired"), nil
	}

	mode := ledgerArgStr(args, "mode")
	if mode == "" {
		mode = "log"
	}
	if mode != "log" && mode != "open" {
		return tools.NewErrorResult(`ledger_query: mode must be "log" or "open"`), nil
	}

	subject := ledgerArgStr(args, "subject")
	sinceID, _, badSince := ledgerArgInt(args, "since_id")
	if badSince {
		return tools.NewErrorResult("ledger_query: since_id must be a ledger record id (integer)"), nil
	}
	limit64, _, badLimit := ledgerArgInt(args, "limit")
	if badLimit {
		return tools.NewErrorResult("ledger_query: limit must be an integer"), nil
	}

	res := ledgerQueryResult{
		Records: []ledgerRecordView{},
		Open:    []ledgerStateView{},
	}

	switch mode {
	case "open":
		states, err := t.store.QueryOpen(ctx, employeeID)
		if err != nil {
			return tools.NewErrorResultErr(fmt.Errorf("ledger_query: %w", err)), nil
		}
		for _, st := range states {
			res.Open = append(res.Open, ledgerStateView{
				Subject:   st.Subject,
				Status:    string(st.Status),
				Summary:   st.Summary,
				UpdatedAt: st.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
				AsOf:      st.AsOf,
			})
			if st.AsOf > res.Head {
				res.Head = st.AsOf
			}
		}

	default: // mode == "log"
		q := ledger.LogQuery{
			EmployeeID: employeeID,
			Subject:    subject,
			SinceID:    sinceID,
			Limit:      int(limit64),
		}
		var (
			records []ledger.Record
			err     error
		)
		if text := ledgerArgStr(args, "text"); text != "" {
			records, err = t.store.SearchLog(ctx, q, text)
		} else {
			records, err = t.store.QueryLog(ctx, q)
		}
		if err != nil {
			return tools.NewErrorResultErr(fmt.Errorf("ledger_query: %w", err)), nil
		}
		for _, rec := range records {
			view := ledgerRecordView{
				ID:        rec.ID,
				At:        rec.At.Format("2006-01-02T15:04:05Z07:00"),
				Kind:      string(rec.Kind),
				Subject:   rec.Subject,
				Source:    string(rec.Source),
				SourceRef: rec.SourceRef,
				Resolves:  rec.Resolves,
			}
			// Deliver the stored payload as decoded JSON where possible;
			// a non-JSON (raw-writer) payload rides as a quoted string so
			// the response always marshals.
			if len(rec.Payload) > 0 {
				if json.Valid(rec.Payload) {
					view.Payload = json.RawMessage(rec.Payload)
				} else {
					b, _ := json.Marshal(string(rec.Payload))
					view.Payload = json.RawMessage(b)
				}
			}
			res.Records = append(res.Records, view)
			if rec.ID > res.Head {
				res.Head = rec.ID
			}
		}
	}

	return tools.NewSuccessResult(res), nil
}

// =========================================================================
// registration
// =========================================================================

// RegisterLedgerTools registers ledger_append and ledger_query on reg over
// the given store. Called by the daemon wiring (leaf 03-01); no
// package-init side effects.
func RegisterLedgerTools(reg *tools.Registry, store *ledger.Store, logger *slog.Logger) {
	if reg == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	reg.Register(NewLedgerAppendTool(store, logger))
	reg.Register(NewLedgerQueryTool(store, logger))
}
