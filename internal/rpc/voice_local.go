package rpc

// voice_local.go - the voice.local.* RPC surface (local-leg plan CL8,
// leaf 08): voice.local.open / voice.local.end / voice.local.status, plus
// voice.local.events (operator telemetry: poll a per-call event ring).
//
// The handler is SEAM-BASED (func fields, not voice types): internal/rpc
// must not import internal/voice - the voice package imports activity,
// which already imports rpc, so a direct edge closes the cycle. The
// daemon wiring adapts *voice.LocalLeg onto VoiceLocalSeams
// (registerVoiceLocalRPC); a nil/unwired leg registers the zero seams
// and every method refuses with the named disabled-leg error.
//
// CL8 shapes (updated by the contact-display tree, leaf 02 §4):
//
//	voice.local.open   req {employee_id, label} -> {call_id, in_path, out_path, session_id}
//	voice.local.end    req {call_id}            -> {ok}
//	voice.local.status req {}                   -> {calls:[{call_id, employee_id, target_name, target_display, since_rfc3339, leg}]}
//	voice.local.events req {call_id, cursor?, limit?}
//	                   -> {call_id, events:[{seq, kind, at, text?, words?, ms?, reason?}], cursor, truncated?, found}
//	voice.local.transcript req {call_id}
//	                   -> {call_id, employee_id, session_id, lines:[{role,text,at}], found}
//
// Named refusals: ErrVoiceLocalDisabled (leg unwired/disabled),
// ErrVoiceLocalUnknownEmployee (no such employee - checked BEFORE the
// open seam so a typo never opens a call), ErrVoiceLocalBusy /
// ErrVoiceLocalDuplicate (registry rules, surfaced from the seam).
// No audio over RPC (CL8): the open result carries the two pair socket
// PATHS the client dials; nothing else.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Named refusals (CL8). Wrapped with %w by the handlers; the daemon
// adapter maps voice.ErrLocalDisabled etc. onto these via errors.Is.
var (
	ErrVoiceLocalDisabled = errors.New("voice: local leg is disabled")
	// ErrVoiceLocalUnknownEmployee covers BOTH an empty employee_id and
	// an id with no employee row: the leg refuses before any side
	// effect either way.
	ErrVoiceLocalUnknownEmployee = errors.New("voice: unknown employee")
	// ErrVoiceLocalBusy / ErrVoiceLocalDuplicate mirror the registry's
	// shared cross-leg rules (CL6): over capacity / employee already
	// live, and a live call to the same target.
	ErrVoiceLocalBusy      = errors.New("voice: call registry busy (over capacity or employee already live)")
	ErrVoiceLocalDuplicate = errors.New("voice: a live call to this target already exists")
	// ErrVoiceLocalUnknownCall is End on a call this leg never opened
	// (double End is the idempotent ok=true, never this error).
	ErrVoiceLocalUnknownCall = errors.New("voice: unknown call")
)

// VoiceLocalOpen is the voice.local.open result (CL8 verbatim). The
// paths are the audio-pair unix socket files under $MEEPT_HOME/voice/sock
// the client dials (mic writes to in_path, speaker reads from out_path).
type VoiceLocalOpen struct {
	CallID    string `json:"call_id"`
	InPath    string `json:"in_path"`
	OutPath   string `json:"out_path"`
	SessionID string `json:"session_id"`
}

// VoiceLocalEndResult is the voice.local.end result (CL8).
type VoiceLocalEndResult struct {
	OK bool `json:"ok"`
}

// VoiceCallStatus is one live call row (CL8). Leg is "phone" or
// "local"; status lists BOTH legs (the registry's live snapshot), so a
// client can show the whole voice state from one method.
//
// DISCLOSURE RULE (contact-display master O1/O4, leaf 02): a phone-leg
// row identifies the target through the boundary-resolved display pair —
// TargetName (contact name or "Unknown caller") and TargetDisplay
// (operator-safe, at most the last-4 form). The daemon fill NEVER
// populates the raw Target any more; the field survives ONLY as an
// omitempty legacy shim for old fake-driven tests and must not be read
// for routing (leaf 03 migrates the client renderers to the display
// pair). Local-leg rows carry the "local:<label>" string, which has no
// number in it.
type VoiceCallStatus struct {
	CallID        string `json:"call_id"`
	EmployeeID    string `json:"employee_id"`
	TargetName    string `json:"target_name"`
	TargetDisplay string `json:"target_display"`
	SinceRFC3339  string `json:"since_rfc3339"`
	Leg           string `json:"leg"`
	// Deprecated: never filled by the daemon since the leaf 02 swap
	// (O4 raw-number removal). Present so legacy test fixtures keep
	// compiling; empty values omit the key from the wire entirely.
	Target string `json:"target,omitempty"`
}

// VoiceLocalStatusResult is the voice.local.status result (CL8).
// Calls is never nil (empty list, not absent) so clients render "no
// live calls" from the same shape.
type VoiceLocalStatusResult struct {
	Calls []VoiceCallStatus `json:"calls"`
}

// VoiceCallEvent is one operator-telemetry ring entry (voice.local.events).
// The daemon's internal/voice telemetry ring is the producer; the RPC
// mirror stays seam-typed (internal/rpc never imports internal/voice).
// Kind is one of: heard, prompt_dispatch, prompt_held, prompt_released,
// prompt_dropped, turn_final, barge_in, call_ended. Text is always a
// truncated preview; Words/Ms/Reason are kind-dependent (0/"" omitted).
type VoiceCallEvent struct {
	Seq  uint64 `json:"seq"`
	Kind string `json:"kind"`
	// At is RFC3339Nano (daemon clock); clients render the time part.
	At     string `json:"at"`
	Text   string `json:"text,omitempty"`
	Words  int    `json:"words,omitempty"`
	Ms     int64  `json:"ms,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// VoiceLocalEventsResult is the voice.local.events result. Events is
// never nil (empty list, not absent). Cursor is the highest Seq returned,
// ECHOING the request cursor when nothing new was returned, so a client
// advances by assignment (cursor = res.Cursor) in both directions.
// Truncated reports ONLY genuine eviction (events between the request
// cursor and the ring's retained window were overwritten by newer rows -
// a slow poller lost them): the poller should note the gap, not treat it
// as an error. HasMore reports a limit-capped backlog (rows exist beyond
// the returned window; read again with the returned cursor).
type VoiceLocalEventsResult struct {
	CallID    string           `json:"call_id"`
	Events    []VoiceCallEvent `json:"events"`
	Cursor    uint64           `json:"cursor"`
	Truncated bool             `json:"truncated,omitempty"`
	HasMore   bool             `json:"has_more,omitempty"`
	Found     bool             `json:"found"`
}

// VoiceCallTranscriptLine is one stored call-transcript row
// (voice.local.transcript). Role is "user" (caller speech) or "assistant"
// (the employee's spoken reply); At is RFC3339 (store clock).
type VoiceCallTranscriptLine struct {
	Role string `json:"role"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// VoiceLocalTranscriptResult is the voice.local.transcript result (the
// end-of-call transcript read the CLI prints). Lines is never nil (empty
// list, not absent). Found reports whether the call id exists in the
// store at all; an unknown/never-stored call is the empty envelope, never
// an error - the CLI print is best-effort by contract.
type VoiceLocalTranscriptResult struct {
	CallID     string                    `json:"call_id"`
	EmployeeID string                    `json:"employee_id"`
	SessionID  string                    `json:"session_id"`
	Lines      []VoiceCallTranscriptLine `json:"lines"`
	Found      bool                      `json:"found"`
}

// VoiceLocalSeams carries the daemon-side operations the handler calls.
// ALL fields optional: a nil field degrades THAT operation to the named
// refusal (never a panic, never "method not found"). The daemon wiring
// fills them from *voice.LocalLeg + the registry + the employee store;
// tests fill them with fakes.
type VoiceLocalSeams struct {
	// Open opens a local call. Registry refusals surface mapped:
	// voice.ErrBusy -> ErrVoiceLocalBusy, voice.ErrDuplicateTarget ->
	// ErrVoiceLocalDuplicate, voice.ErrLocalDisabled ->
	// ErrVoiceLocalDisabled (the adapter wraps; errors.Is walks it).
	Open func(ctx context.Context, employeeID, label string) (VoiceLocalOpen, error)
	// End ends one call. nil return = idempotent success (double End).
	// A foreign call id maps to ErrVoiceLocalUnknownCall.
	End func(callID, reason string) error
	// Status snapshots the live calls across BOTH legs (registry rows;
	// the local leg's live calls carry leg "local", phone rows "phone").
	Status func() []VoiceCallStatus
	// EmployeeExists reports whether employeeID names a real employee.
	// The concrete employee.Manager.GetEmployee returns
	// (nil, employee.ErrEmployeeNotFound) for unknown ids; the adapter
	// folds that onto (false, nil). An error here fails the open with
	// a wrapped plain error (infrastructure, not a named refusal).
	EmployeeExists func(ctx context.Context, employeeID string) (bool, error)
	// Events reads the operator-telemetry ring for one call: events with
	// Seq > cursor, capped at limit (limit <= 0 = server default).
	// truncated reports ONLY evicted backlog (rows the cursor points at
	// no longer exist); hasMore reports a limit-capped backlog; found
	// reports whether the call id has a ring (a ring-less read is an
	// empty list, never an error - the poller must stay quiet on
	// teardown races instead of erroring every tick).
	Events func(callID string, cursor uint64, limit int) (events []VoiceCallEvent, truncated bool, hasMore bool, found bool)
	// Transcript reads ONE call's stored transcript from the call store
	// (voice.local.transcript - the end-of-call print). Returns the
	// call's employee/session ids and its role-tagged lines; found=false
	// for an unknown call id (empty envelope, never an error). Nil seam
	// (no store wired) answers the same empty envelope.
	Transcript func(ctx context.Context, callID string) (employeeID, sessionID string, lines []VoiceCallTranscriptLine, found bool)
}

// VoiceLocalHandler serves voice.local.open / .end / .status.
type VoiceLocalHandler struct {
	seams VoiceLocalSeams
}

// NewVoiceLocalHandler constructs the handler. The zero VoiceLocalSeams
// (all nil) is the disabled-leg posture: every mutating method refuses
// with ErrVoiceLocalDisabled; status answers an empty list.
func NewVoiceLocalHandler(seams VoiceLocalSeams) *VoiceLocalHandler {
	return &VoiceLocalHandler{seams: seams}
}

// RegisterVoiceLocalMethods registers the methods on the server.
// Always registers (even with zero seams) so clients get the named
// disabled error instead of "method not found" - the acp.list posture.
func (h *VoiceLocalHandler) RegisterVoiceLocalMethods(server *Server) {
	if server == nil || h == nil {
		return
	}
	server.RegisterHandler("voice.local.open", h.handleOpen)
	server.RegisterHandler("voice.local.end", h.handleEnd)
	server.RegisterHandler("voice.local.status", h.handleStatus)
	server.RegisterHandler("voice.local.events", h.handleEvents)
	server.RegisterHandler("voice.local.transcript", h.handleTranscript)
}

// handleOpen implements voice.local.open (CL8). Order: parse -> gates
// (wired leg, employee exists) -> open seam. Zero side effects before
// every named refusal passes.
func (h *VoiceLocalHandler) handleOpen(ctx context.Context, params json.RawMessage) (any, error) {
	if h.seams.Open == nil {
		return nil, ErrVoiceLocalDisabled
	}
	var req struct {
		EmployeeID string `json:"employee_id"`
		Label      string `json:"label"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("voice.local.open: invalid parameters: %w", err)
		}
	}
	if req.EmployeeID == "" {
		return nil, fmt.Errorf("%w: employee_id is required", ErrVoiceLocalUnknownEmployee)
	}
	if h.seams.EmployeeExists != nil {
		ok, err := h.seams.EmployeeExists(ctx, req.EmployeeID)
		if err != nil {
			return nil, fmt.Errorf("voice.local.open: employee lookup: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrVoiceLocalUnknownEmployee, req.EmployeeID)
		}
	}
	return h.seams.Open(ctx, req.EmployeeID, req.Label)
}

// handleEnd implements voice.local.end (CL8). A missing call_id refuses
// before the seam; the seam's nil return is the idempotent ok=true.
func (h *VoiceLocalHandler) handleEnd(_ context.Context, params json.RawMessage) (any, error) {
	if h.seams.End == nil {
		return nil, ErrVoiceLocalDisabled
	}
	var req struct {
		CallID string `json:"call_id"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("voice.local.end: invalid parameters: %w", err)
		}
	}
	if req.CallID == "" {
		return nil, fmt.Errorf("%w: call_id is required", ErrVoiceLocalUnknownCall)
	}
	if err := h.seams.End(req.CallID, "rpc voice.local.end"); err != nil {
		return nil, err
	}
	return VoiceLocalEndResult{OK: true}, nil
}

// handleStatus implements voice.local.status (CL8): the live snapshot
// across both legs. Unwired status seam = empty envelope (the same
// disabled posture acp.list pins, query-shaped because status is safe).
func (h *VoiceLocalHandler) handleStatus(_ context.Context, _ json.RawMessage) (any, error) {
	if h.seams.Status == nil {
		return VoiceLocalStatusResult{Calls: []VoiceCallStatus{}}, nil
	}
	rows := h.seams.Status()
	if rows == nil {
		rows = []VoiceCallStatus{}
	}
	return VoiceLocalStatusResult{Calls: rows}, nil
}

// handleEvents implements voice.local.events (operator telemetry): one
// ring read for a single call since a cursor. Params
// {call_id, cursor?, limit?}; a missing call_id refuses with the named
// unknown-call error. Unwired seam / ring-less call = the empty envelope
// (found:false), never "method not found" and never an error - the CLI
// poller ticks every ~500 ms and a teardown race must not spam failures.
func (h *VoiceLocalHandler) handleEvents(_ context.Context, params json.RawMessage) (any, error) {
	var req struct {
		CallID string `json:"call_id"`
		Cursor uint64 `json:"cursor"`
		Limit  int    `json:"limit"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("voice.local.events: invalid parameters: %w", err)
		}
	}
	if req.CallID == "" {
		return nil, fmt.Errorf("%w: call_id is required", ErrVoiceLocalUnknownCall)
	}
	res := VoiceLocalEventsResult{CallID: req.CallID, Events: []VoiceCallEvent{}, Cursor: req.Cursor}
	if h.seams.Events == nil {
		return res, nil
	}
	events, truncated, hasMore, found := h.seams.Events(req.CallID, req.Cursor, req.Limit)
	if events == nil {
		events = []VoiceCallEvent{}
	}
	res.Events = events
	res.Truncated = truncated
	res.HasMore = hasMore
	res.Found = found
	for _, ev := range events {
		if ev.Seq > res.Cursor {
			res.Cursor = ev.Seq
		}
	}
	return res, nil
}

// handleTranscript implements voice.local.transcript: one call's stored
// transcript (role-tagged lines) for the CLI's end-of-call print. Params
// {call_id}; a missing call_id refuses with the named unknown-call error.
// Unwired seam / unknown call = the empty envelope (found:false), never an
// error - the CLI read is best-effort like the telemetry poller.
func (h *VoiceLocalHandler) handleTranscript(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		CallID string `json:"call_id"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("voice.local.transcript: invalid parameters: %w", err)
		}
	}
	if req.CallID == "" {
		return nil, fmt.Errorf("%w: call_id is required", ErrVoiceLocalUnknownCall)
	}
	res := VoiceLocalTranscriptResult{CallID: req.CallID, Lines: []VoiceCallTranscriptLine{}}
	if h.seams.Transcript == nil {
		return res, nil
	}
	emp, sess, lines, found := h.seams.Transcript(ctx, req.CallID)
	if lines == nil {
		lines = []VoiceCallTranscriptLine{}
	}
	res.EmployeeID = emp
	res.SessionID = sess
	res.Lines = lines
	res.Found = found
	return res, nil
}
