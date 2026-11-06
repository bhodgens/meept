package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// IntakeStatus represents the status of the intake poller.
type IntakeStatus struct {
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

// renderIntakeStatus renders a plain styled report of the intake status.
func renderIntakeStatus(s IntakeStatus, rawErr error) string {
	if rawErr != nil {
		return fmt.Sprintf("intake error: %s", rawErr.Error())
	}

	if !s.Enabled {
		note := s.Note
		if note == "" {
			note = "intake disabled"
		}
		return fmt.Sprintf("intake status\n---------------\n  status: %s", note)
	}

	var sb strings.Builder
	sb.WriteString("intake status\n")
	sb.WriteString("---------------\n")

	sb.WriteString(fmt.Sprintf("  enabled:      %v\n", s.Enabled))
	sb.WriteString(fmt.Sprintf("  watch_repo:    %s\n", s.WatchRepo))
	sb.WriteString(fmt.Sprintf("  interval:      %s\n", s.PollInterval))

	if len(s.RequiredLabels) > 0 {
		sb.WriteString(fmt.Sprintf("  labels:       %s\n", strings.Join(s.RequiredLabels, ", ")))
	} else {
		sb.WriteString("  labels:       factory (default)\n")
	}

	sb.WriteString(fmt.Sprintf("  done_label:   %s\n", s.DoneLabel))

	if strings.TrimSpace(s.DefaultEmployee) == "" {
		sb.WriteString("  employee:     missing\n")
	} else {
		sb.WriteString(fmt.Sprintf("  employee:     %s\n", s.DefaultEmployee))
	}

	if s.GateCommand == "" {
		sb.WriteString("  gate:         none\n")
	} else {
		sb.WriteString(fmt.Sprintf("  gate:         %s\n", s.GateCommand))
	}

	if s.ProjectNumber == 0 {
		sb.WriteString("  project:      none\n")
	} else {
		sb.WriteString(fmt.Sprintf("  project:      #%d\n", s.ProjectNumber))
	}

	if s.PollerScheduled {
		sb.WriteString("  poll job:     scheduled (intake_poller)\n")
	} else {
		sb.WriteString("  poll job:     not scheduled\n")
	}

	return sb.String()
}

// executeIntake calls the intake.status RPC and returns a CommandResult.
func (h *CommandHandler) executeIntake() *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{Output: ErrNotConnected, IsError: true}
	}
	raw, err := h.rpc.Call("intake.status", nil)
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("intake error: %v", err),
			IsError: false,
		}
	}

	var s IntakeStatus
	if err := json.Unmarshal(raw, &s); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("malformed intake status: %v", err),
			IsError: false,
		}
	}

	return &CommandResult{
		Output: renderIntakeStatus(s, nil),
	}
}
