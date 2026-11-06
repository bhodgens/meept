package rpc

// intake_status.go exposes the GitHub ticket intake ([intake] block) status
// over RPC for the TUI/GUI parity surface (issue #109). Report-only mirror
// of `meept intake status` (cmd/meept/intake.go): it NEVER touches token
// material (presence-only posture, same as github status).

import (
	"context"
	"encoding/json"
)

// IntakeStatusView is the flat intake.status response and the config view
// the daemon handler renders. Field names are the frozen cross-surface
// contract (TUI internal/tui/intake_view.go, GUI, CLI): enabled,
// watch_repo, poll_interval, required_labels, done_label, default_employee,
// gate_command, project_number, poller_scheduled, note.
type IntakeStatusView struct {
	Enabled         bool     `json:"enabled"`
	WatchRepo       string   `json:"watch_repo"`
	PollInterval    string   `json:"poll_interval"`
	RequiredLabels  []string `json:"required_labels"`
	DoneLabel       string   `json:"done_label"`
	DefaultEmployee string   `json:"default_employee"`
	GateCommand     string   `json:"gate_command"`
	ProjectNumber   int64    `json:"project_number"`
	PollerScheduled bool     `json:"poller_scheduled"`
	Note            string   `json:"note"`
}

// IntakeStatusHandler serves intake.status. Config is a snapshot function
// provided by the daemon (it may change across config reloads);
// pollerScheduled reports whether the daemon actually scheduled the
// "intake_poller" job (nil = unknown, reported as false). Either may be
// nil: a daemon that never wired [intake] still answers the method with
// the disabled envelope, never "method not found" — same posture as the
// backup/sync avail* nil-guards (backup_sync.go).
type IntakeStatusHandler struct {
	config          func() IntakeStatusView
	pollerScheduled func() bool
}

// NewIntakeStatusHandler builds the handler. Passing a nil config fn is
// allowed: every call renders the disabled default.
func NewIntakeStatusHandler(configFn func() IntakeStatusView, pollerScheduledFn func() bool) *IntakeStatusHandler {
	return &IntakeStatusHandler{config: configFn, pollerScheduled: pollerScheduledFn}
}

// SetConfigFunc re-wires the config snapshot source. Nil-guarded per the
// repo setter convention.
func (h *IntakeStatusHandler) SetConfigFunc(fn func() IntakeStatusView) {
	if h != nil && fn != nil {
		h.config = fn
	}
}

// SetPollerScheduledFunc re-wires the live poller-job probe. Nil-guarded.
func (h *IntakeStatusHandler) SetPollerScheduledFunc(fn func() bool) {
	if h != nil && fn != nil {
		h.pollerScheduled = fn
	}
}

// RegisterIntakeStatusMethods registers intake.status on server. Safe to
// call with a nil handler (registers the disabled envelope).
func (h *IntakeStatusHandler) RegisterIntakeStatusMethods(server *Server) {
	server.RegisterHandler("intake.status", h.handleIntakeStatus)
}

// handleIntakeStatus answers intake.status (no params). Report-only; it
// never fails on a disabled state and never surfaces token material.
func (h *IntakeStatusHandler) handleIntakeStatus(_ context.Context, _ json.RawMessage) (any, error) {
	view := IntakeStatusView{
		Note: "intake disabled (default); no poll job is scheduled",
	}
	if h != nil && h.config != nil {
		view = h.config()
	}
	view.PollerScheduled = false
	if view.Enabled && h != nil && h.pollerScheduled != nil {
		view.PollerScheduled = h.pollerScheduled()
	}
	if !view.Enabled {
		// Mirror the CLI's disabled posture: no partial config leaks,
		// poller is definitionally unscheduled.
		view.PollerScheduled = false
		if view.Note == "" {
			view.Note = "intake disabled (default); no poll job is scheduled"
		}
	} else if view.Note == "" {
		view.Note = "intake enabled"
	}
	return view, nil
}
