package rpc

// voice_link.go - the voice.link.* RPC surface (deadair-link plan leaf
// 03): voice.link.open / voice.link.close / voice.link.status.
//
// SEAM-BASED exactly like voice_local.go: internal/rpc must NEVER
// import internal/voice (voice -> activity -> rpc closes the cycle); the
// daemon adapts *voice.LinkManager onto VoiceLinkSeams in
// internal/daemon/voice_link_rpc.go. All fields optional: a nil seam
// degrades THAT method to the named disabled refusal (never a panic,
// never "method not found"). No audio ever rides RPC - like the local
// leg, the bridge lives entirely daemon-side; the open result carries
// only routing ids.
//
// Shapes (mirror of the CL8 local shape):
//
//	voice.link.open   req {employee_a, employee_b} -> {link_id, a_ccid, b_ccid}
//	voice.link.close  req {link_id}                 -> {ok}
//	voice.link.status req {link_id}   -> {link_id, ended,
//	                                       a: {call_id, employee_id, assistant_turns,
//	                                           user_turns, transcript:[{role,text,at}]},
//	                                       b: {...}}

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Named refusals (leaf 03). The daemon adapter maps voice.ErrLinkSameEmployee
// / voice.ErrLocalDisabled etc. onto these via errors.Is.
var (
	// ErrVoiceLinkDisabled: no link manager wired (the local leg is
	// disabled - a link is two local legs, so the gate is the same).
	ErrVoiceLinkDisabled = errors.New("voice: voice link is disabled")
	// ErrVoiceLinkSameEmployee: a == b (also caught voice-side by
	// voice.ErrLinkSameEmployee; the RPC gate makes the refusal
	// explicit before any seam call).
	ErrVoiceLinkSameEmployee = errors.New("voice: employee_a and employee_b must be different employees")
	// ErrVoiceLinkUnknownEmployee: an empty or non-existent id, either
	// side (checked BEFORE the open seam so a typo never opens a leg).
	ErrVoiceLinkUnknownEmployee = errors.New("voice: unknown employee")
	// ErrVoiceLinkUnknownLink: close/status on a link id this manager
	// never opened (double Close is the idempotent ok=true, never this).
	ErrVoiceLinkUnknownLink = errors.New("voice: unknown link")
)

// VoiceLinkOpen is the voice.link.open result. Routing ids only - the
// audio pair stays inside the daemon bridge.
type VoiceLinkOpen struct {
	LinkID string `json:"link_id"`
	ACCID  string `json:"a_ccid"`
	BCCID  string `json:"b_ccid"`
}

// VoiceLinkCloseResult is the voice.link.close result.
type VoiceLinkCloseResult struct {
	OK bool `json:"ok"`
}

// VoiceLinkTranscriptLine is one transcript row (role/text/at mirrors
// the calls.db JSON so the CLI prints it verbatim).
type VoiceLinkTranscriptLine struct {
	Role string `json:"role"`
	Text string `json:"text"`
	At   string `json:"at"` // RFC3339
}

// VoiceLinkSideStatus is one side's live status: routing ids plus turn
// counts read from the call store transcript. Transcript is never nil
// (empty list, not absent).
type VoiceLinkSideStatus struct {
	CallID         string                    `json:"call_id"`
	EmployeeID     string                    `json:"employee_id"`
	AssistantTurns int                       `json:"assistant_turns"`
	UserTurns      int                       `json:"user_turns"`
	Transcript     []VoiceLinkTranscriptLine `json:"transcript"`
}

// VoiceLinkStatusResult is the voice.link.status result.
type VoiceLinkStatusResult struct {
	LinkID string              `json:"link_id"`
	Ended  bool                `json:"ended"`
	A      VoiceLinkSideStatus `json:"a"`
	B      VoiceLinkSideStatus `json:"b"`
}

// VoiceLinkSeams carries the daemon-side operations the handler calls.
// The daemon wiring fills them from *voice.LinkManager + the call store +
// the employee store; tests fill them with fakes.
type VoiceLinkSeams struct {
	// Open cross-connects two local legs. Registry refusals surface
	// mapped by the adapter (busy/duplicate -> the local names,
	// same-employee -> ErrVoiceLinkSameEmployee).
	Open func(ctx context.Context, employeeA, employeeB string) (VoiceLinkOpen, error)
	// Close ends the link (both legs). A nil return from the seam-side
	// leg End is the idempotent success; an unknown link id maps to
	// ErrVoiceLinkUnknownLink.
	Close func(linkID, reason string) error
	// Status snapshots both sides. An unknown link maps to
	// ErrVoiceLinkUnknownLink (the seam returns the error, the handler
	// passes it through).
	Status func(ctx context.Context, linkID string) (VoiceLinkStatusResult, error)
	// EmployeeExists reports whether employeeID names a real employee
	// (same seam shape as VoiceLocalSeams.EmployeeExists).
	EmployeeExists func(ctx context.Context, employeeID string) (bool, error)
}

// VoiceLinkHandler serves voice.link.open / .close / .status.
type VoiceLinkHandler struct {
	seams VoiceLinkSeams
}

// NewVoiceLinkHandler constructs the handler. The zero VoiceLinkSeams
// (all nil) is the disabled posture: every method refuses with the named
// disabled error.
func NewVoiceLinkHandler(seams VoiceLinkSeams) *VoiceLinkHandler {
	return &VoiceLinkHandler{seams: seams}
}

// RegisterVoiceLinkMethods registers the three methods on the server.
// Always registers (even with zero seams) so clients get the named
// disabled error instead of "method not found" - the voice.local.*/
// acp.list posture.
func (h *VoiceLinkHandler) RegisterVoiceLinkMethods(server *Server) {
	if server == nil || h == nil {
		return
	}
	server.RegisterHandler("voice.link.open", h.handleOpen)
	server.RegisterHandler("voice.link.close", h.handleClose)
	server.RegisterHandler("voice.link.status", h.handleStatus)
}

// handleOpen implements voice.link.open. Order: parse -> gates (wired
// seam, ids present, a != b, employees exist) -> open seam. Zero side
// effects before every named refusal passes.
func (h *VoiceLinkHandler) handleOpen(ctx context.Context, params json.RawMessage) (any, error) {
	if h.seams.Open == nil {
		return nil, ErrVoiceLinkDisabled
	}
	var req struct {
		EmployeeA string `json:"employee_a"`
		EmployeeB string `json:"employee_b"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("voice.link.open: invalid parameters: %w", err)
		}
	}
	if req.EmployeeA == "" || req.EmployeeB == "" {
		return nil, fmt.Errorf("%w: employee_a and employee_b are required", ErrVoiceLinkUnknownEmployee)
	}
	if req.EmployeeA == req.EmployeeB {
		return nil, fmt.Errorf("%w: %s", ErrVoiceLinkSameEmployee, req.EmployeeA)
	}
	if h.seams.EmployeeExists != nil {
		for _, emp := range []string{req.EmployeeA, req.EmployeeB} {
			ok, err := h.seams.EmployeeExists(ctx, emp)
			if err != nil {
				return nil, fmt.Errorf("voice.link.open: employee lookup: %w", err)
			}
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrVoiceLinkUnknownEmployee, emp)
			}
		}
	}
	return h.seams.Open(ctx, req.EmployeeA, req.EmployeeB)
}

// handleClose implements voice.link.close. A missing link_id refuses
// before the seam; the seam's idempotent success is ok=true.
func (h *VoiceLinkHandler) handleClose(_ context.Context, params json.RawMessage) (any, error) {
	if h.seams.Close == nil {
		return nil, ErrVoiceLinkDisabled
	}
	var req struct {
		LinkID string `json:"link_id"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("voice.link.close: invalid parameters: %w", err)
		}
	}
	if req.LinkID == "" {
		return nil, fmt.Errorf("%w: link_id is required", ErrVoiceLinkUnknownLink)
	}
	if err := h.seams.Close(req.LinkID, "rpc voice.link.close"); err != nil {
		return nil, err
	}
	return VoiceLinkCloseResult{OK: true}, nil
}

// handleStatus implements voice.link.status: both sides' turn counts and
// transcripts.
func (h *VoiceLinkHandler) handleStatus(ctx context.Context, params json.RawMessage) (any, error) {
	if h.seams.Status == nil {
		return nil, ErrVoiceLinkDisabled
	}
	var req struct {
		LinkID string `json:"link_id"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("voice.link.status: invalid parameters: %w", err)
		}
	}
	if req.LinkID == "" {
		return nil, fmt.Errorf("%w: link_id is required", ErrVoiceLinkUnknownLink)
	}
	res, err := h.seams.Status(ctx, req.LinkID)
	if err != nil {
		return nil, err
	}
	// Clients render "no lines" from the same shape (never null).
	if res.A.Transcript == nil {
		res.A.Transcript = []VoiceLinkTranscriptLine{}
	}
	if res.B.Transcript == nil {
		res.B.Transcript = []VoiceLinkTranscriptLine{}
	}
	return res, nil
}
