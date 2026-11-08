package rpc

// voice_local.go - the voice.local.* RPC surface (local-leg plan CL8,
// leaf 08): voice.local.open / voice.local.end / voice.local.status.
//
// The handler is SEAM-BASED (func fields, not voice types): internal/rpc
// must not import internal/voice - the voice package imports activity,
// which already imports rpc, so a direct edge closes the cycle. The
// daemon wiring adapts *voice.LocalLeg onto VoiceLocalSeams
// (registerVoiceLocalRPC); a nil/unwired leg registers the zero seams
// and every method refuses with the named disabled-leg error.
//
// CL8 shapes (verbatim JSON):
//
//	voice.local.open   req {employee_id, label} -> {call_id, in_path, out_path, session_id}
//	voice.local.end    req {call_id}            -> {ok}
//	voice.local.status req {}                   -> {calls:[{call_id, employee_id, target, since_rfc3339, leg}]}
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
type VoiceCallStatus struct {
	CallID       string `json:"call_id"`
	EmployeeID   string `json:"employee_id"`
	Target       string `json:"target"`
	SinceRFC3339 string `json:"since_rfc3339"`
	Leg          string `json:"leg"`
}

// VoiceLocalStatusResult is the voice.local.status result (CL8).
// Calls is never nil (empty list, not absent) so clients render "no
// live calls" from the same shape.
type VoiceLocalStatusResult struct {
	Calls []VoiceCallStatus `json:"calls"`
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

// RegisterVoiceLocalMethods registers the three methods on the server.
// Always registers (even with zero seams) so clients get the named
// disabled error instead of "method not found" - the acp.list posture.
func (h *VoiceLocalHandler) RegisterVoiceLocalMethods(server *Server) {
	if server == nil || h == nil {
		return
	}
	server.RegisterHandler("voice.local.open", h.handleOpen)
	server.RegisterHandler("voice.local.end", h.handleEnd)
	server.RegisterHandler("voice.local.status", h.handleStatus)
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
