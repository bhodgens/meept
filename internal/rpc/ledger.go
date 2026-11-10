package rpc

// ledger.go — the ledger.query / ledger.state RPC surface (plan
// 20261109-employee-ledger, branch 02-surfaces, leaf 02-surfaces/02).
//
// SEAM POSTURE (pinned by internal/rpc/ledger_test.go, copied exactly):
// internal/rpc does NOT import internal/ledger — the same light edge the
// voice_local.go precedent establishes. The daemon adapts *ledger.Store
// (QueryLog / QueryOpen / max-id head) onto the narrow LedgerReader
// below and maps ledger row types onto the small LOCAL wire types here;
// store types never cross this boundary.
//
// Methods (00T §2, frozen):
//
//	ledger.query  req {employee, mode: "log"|"open", subject?, since_id?, limit?}
//	              resp {records:[...], open:[...], head:int64}
//	ledger.state  req {employee, subject}
//	              resp the one current-truth row (or found:false)
//
// IDENTITY DIRECTION (D1): unlike the ctx-stamped tools (internal/tools/
// builtin/ledger.go, where employee id is stamped server-side), here the
// employee id IS a request field — this is a client-facing read surface
// and reads are unpermissioned in v1.
//
// RAW-ECHO BOUNDARY (D6): payloads are returned verbatim from the seam.
// No masking happens here — redaction is surface-side (TUI/GUI). The
// ledger payload convention says DO NOT store phone digits; in v1 that
// is enforced socially at the append side, never retro-fixed here.
//
// Empty-result posture (VoiceLocalStatusResult "never nil" precedent):
// unknown employee / no rows serializes records and open as [] — never
// null, never an error. Named refusals fire BEFORE the seam is touched.
//
// WIRING NOTE: the daemon registers this at the same construction site
// as voice.local.* (see RegisterLedgerMethods) once the ledger Store is
// brought up in internal/daemon (branch 03 ledger_wiring.go); until then
// the surface stays unregistered and clients get "method not found" —
// the gate tests pin only the handler/constructor surface.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Named refusals for the ledger RPC surface. Wrapped with %w by the
// handlers; the daemon adapter maps store errors onto these where
// relevant (errors.Is walks the chain).
var (
	// ErrLedgerDisabled is the unwired-store posture: a nil LedgerReader.
	ErrLedgerDisabled = errors.New("ledger: store is not wired")
	// ErrLedgerInvalidMode refuses mode outside the closed set log|open.
	ErrLedgerInvalidMode = errors.New("ledger: mode must be \"log\" or \"open\"")
	// ErrLedgerEmployeeRequired refuses an empty employee request field.
	ErrLedgerEmployeeRequired = errors.New("ledger: employee is required")
	// ErrLedgerSubjectRequired refuses ledger.state without a subject.
	ErrLedgerSubjectRequired = errors.New("ledger: subject is required")
)

// LedgerReader is the narrow read seam the daemon adapts from
// *ledger.Store (QueryLog / QueryOpen / max-id head).
type LedgerReader interface {
	QueryLog(ctx context.Context, employeeID, subject string, sinceID int64, limit int) ([]LedgerLogEntry, error)
	QueryOpen(ctx context.Context, employeeID string) ([]LedgerOpenState, error)
	Head(ctx context.Context) (int64, error)
}

// Local wire types. JSON tags are frozen to ledger.State / ledger.Record
// field names (see the §2 contract); employee id IS a request field here
// — client-facing read, unpermissioned in v1 per D1, unlike the
// ctx-stamped tools.

// LedgerLogEntry is one appended ledger row on the wire (mirrors
// ledger.Record; At is RFC3339 TEXT, Payload is the verbatim stored JSON).
type LedgerLogEntry struct {
	ID         int64           `json:"id"`
	EmployeeID string          `json:"employee_id"`
	At         string          `json:"at"`
	Kind       string          `json:"kind"`
	Subject    string          `json:"subject"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Source     string          `json:"source"`
	SourceRef  string          `json:"source_ref"`
	Resolves   int64           `json:"resolves"`
}

// LedgerOpenState is one current-truth row on the wire (mirrors
// ledger.State).
type LedgerOpenState struct {
	EmployeeID string `json:"employee_id"`
	Subject    string `json:"subject"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	UpdatedAt  string `json:"updated_at"`
	AsOf       int64  `json:"as_of"`
}

// LedgerQueryResult is the frozen ledger.query response: the full
// {records, open, head} triple for BOTH modes, with the mode's list
// populated and the other always an empty list (never null).
type LedgerQueryResult struct {
	Records []LedgerLogEntry  `json:"records"`
	Open    []LedgerOpenState `json:"open"`
	Head    int64             `json:"head"`
}

// LedgerStateResult is the ledger.state response: the one subject row
// when found; Found=false with a zero State otherwise (never an error —
// reads of unknown employee/subject are empty answers).
type LedgerStateResult struct {
	Found bool            `json:"found"`
	State LedgerOpenState `json:"state"`
}

// LedgerHandler serves ledger.query and ledger.state.
type LedgerHandler struct {
	reader LedgerReader
}

// NewLedgerHandler constructs the handler over the read seam. A nil
// reader is the disabled-store posture: every method refuses with
// ErrLedgerDisabled (named error, never a panic).
func NewLedgerHandler(reader LedgerReader) *LedgerHandler {
	return &LedgerHandler{reader: reader}
}

// RegisterLedgerMethods registers ledger.query / ledger.state on the
// server. Call this at the daemon's voice.local.* construction site
// (registerVoiceLocalRPC) once the ledger Store is wired.
func (h *LedgerHandler) RegisterLedgerMethods(server *Server) {
	if server == nil || h == nil {
		return
	}
	server.RegisterHandler("ledger.query", h.handleQuery)
	server.RegisterHandler("ledger.state", h.handleState)
}

// handleQuery implements ledger.query. Order: parse -> named refusals
// (wired reader, employee present, mode in the closed set) -> seam.
// mode=log fills Records via QueryLog; mode=open fills Open via
// QueryOpen; Head always answers the store-wide max id. Unknown
// employee -> empty lists, not an error.
func (h *LedgerHandler) handleQuery(ctx context.Context, params json.RawMessage) (any, error) {
	if h.reader == nil {
		return nil, ErrLedgerDisabled
	}
	var req struct {
		Employee string `json:"employee"`
		Mode     string `json:"mode"`
		Subject  string `json:"subject"`
		SinceID  int64  `json:"since_id"`
		Limit    int    `json:"limit"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("ledger.query: invalid parameters: %w", err)
		}
	}
	if req.Employee == "" {
		return nil, fmt.Errorf("%w: employee is required", ErrLedgerEmployeeRequired)
	}
	// Refuse an invalid mode BEFORE the seam is touched.
	if req.Mode != "log" && req.Mode != "open" {
		return nil, fmt.Errorf("%w: got %q", ErrLedgerInvalidMode, req.Mode)
	}

	res := LedgerQueryResult{
		Records: []LedgerLogEntry{},
		Open:    []LedgerOpenState{},
	}
	var err error
	switch req.Mode {
	case "log":
		var rows []LedgerLogEntry
		rows, err = h.reader.QueryLog(ctx, req.Employee, req.Subject, req.SinceID, req.Limit)
		if rows != nil {
			res.Records = rows
		}
	case "open":
		var rows []LedgerOpenState
		rows, err = h.reader.QueryOpen(ctx, req.Employee)
		if rows != nil {
			res.Open = rows
		}
	}
	if err != nil {
		return nil, fmt.Errorf("ledger.query: %w", err)
	}
	head, err := h.reader.Head(ctx)
	if err != nil {
		return nil, fmt.Errorf("ledger.query: head: %w", err)
	}
	res.Head = head
	return res, nil
}

// handleState implements ledger.state (frozen name + request keys):
// {employee, subject} -> the current-truth row for that pair. Answered
// from QueryOpen rows filtered to the subject (the seam's only state
// path); a subject with no open row reports found:false — unknown
// employee/subject is never an error (D1 empty-answer posture).
func (h *LedgerHandler) handleState(ctx context.Context, params json.RawMessage) (any, error) {
	if h.reader == nil {
		return nil, ErrLedgerDisabled
	}
	var req struct {
		Employee string `json:"employee"`
		Subject  string `json:"subject"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("ledger.state: invalid parameters: %w", err)
		}
	}
	if req.Employee == "" {
		return nil, fmt.Errorf("%w: employee is required", ErrLedgerEmployeeRequired)
	}
	if req.Subject == "" {
		return nil, fmt.Errorf("%w: subject is required", ErrLedgerSubjectRequired)
	}
	rows, err := h.reader.QueryOpen(ctx, req.Employee)
	if err != nil {
		return nil, fmt.Errorf("ledger.state: %w", err)
	}
	for _, row := range rows {
		if row.Subject == req.Subject {
			return LedgerStateResult{Found: true, State: row}, nil
		}
	}
	return LedgerStateResult{Found: false}, nil
}
