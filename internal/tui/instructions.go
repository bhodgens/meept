package tui

import (
	"fmt"
	"image/color"
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// RiskLevel represents the risk classification of a user instruction.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// InstructionConfirmationData holds the fields needed to render the instruction
// confirmation dialog. It is decoupled from preferences.ParsedInstruction so the
// TUI layer does not import internal/preferences (avoiding potential import
// cycles). Callers map from ParsedInstruction to this struct at the call site.
type InstructionConfirmationData struct {
	// RiskLevel is the assessed risk: low, medium, high, or critical.
	RiskLevel RiskLevel
	// Action is the tool or command that will be executed (e.g., "shell", "git_commit").
	Action string
	// ActionDetail is an optional human-readable description of the action
	// (e.g., the shell command text or agent trigger target).
	ActionDetail string
	// Trigger is the trigger specification (e.g., "cron: 0 * * * *", "post_hook: file_saved").
	Trigger string
	// Scope is the instruction scope: "project" or "global".
	Scope string
	// Priority is the instruction priority: "low", "normal", "high".
	Priority string
	// RawInput is the original natural-language instruction text.
	RawInput string
}

// InstructionConfirmationModel is a bubbletea model for confirming high-risk
// user instructions before they are persisted or executed.
//
// Keys:
//
//	y      confirm
//	n      cancel
//	esc    cancel
//	ctrl+c cancel
//
// All UI text is lowercase per CLAUDE.md UI conventions.
type InstructionConfirmationModel struct {
	data       InstructionConfirmationData
	confirmed  bool
	cancelled  bool
	width      int
	logger     *slog.Logger
	riskStyle  lipgloss.Style
}

// NewInstructionConfirmationModel constructs a new confirmation dialog model
// from the provided instruction data. If logger is nil, a no-op handler is
// used (the model still functions correctly without logging).
func NewInstructionConfirmationModel(data InstructionConfirmationData, logger *slog.Logger) InstructionConfirmationModel {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return InstructionConfirmationModel{
		data:      data,
		width:     64,
		logger:    logger,
		riskStyle: riskLevelStyle(data.RiskLevel),
	}
}

// Init returns the initial command (nil for this modal).
func (m InstructionConfirmationModel) Init() tea.Cmd {
	return nil
}

// Update handles keypress messages.
func (m InstructionConfirmationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.confirmed = true
			m.logger.Debug("instruction confirmed", "action", m.data.Action, "risk", m.data.RiskLevel)
			return m, tea.Quit
		case "n", "N", "esc":
			m.cancelled = true
			m.logger.Debug("instruction cancelled", "action", m.data.Action)
			return m, tea.Quit
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width < 40 {
			m.width = 40
		}
	}
	return m, nil
}

// View renders the confirmation dialog.
func (m InstructionConfirmationModel) View() tea.View {
	borderColor := riskBorderColor(m.data.RiskLevel)

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(m.width)

	var b strings.Builder

	// Header — lowercase per CLAUDE.md
	b.WriteString(fmt.Sprintf("  confirm instruction — %s\n\n", m.riskStyle.Render(string(m.data.RiskLevel))))

	// Action
	if m.data.Action != "" {
		b.WriteString(fmt.Sprintf("  action:   %s\n", m.data.Action))
	}
	if m.data.ActionDetail != "" {
		detail := truncateForDisplay(m.data.ActionDetail, m.width-14)
		b.WriteString(fmt.Sprintf("  command:  %s\n", detail))
	}

	// Trigger
	if m.data.Trigger != "" {
		b.WriteString(fmt.Sprintf("  trigger:  %s\n", m.data.Trigger))
	}

	// Scope
	if m.data.Scope != "" {
		b.WriteString(fmt.Sprintf("  scope:    %s\n", m.data.Scope))
	}

	// Priority
	if m.data.Priority != "" {
		b.WriteString(fmt.Sprintf("  priority: %s\n", m.data.Priority))
	}

	// Raw input (truncated)
	if m.data.RawInput != "" {
		raw := truncateForDisplay(m.data.RawInput, m.width-14)
		b.WriteString(fmt.Sprintf("  input:    %s\n", raw))
	}

	b.WriteString("\n")

	// Footer — all lowercase per CLAUDE.md
	b.WriteString("  [y] confirm    [n] cancel    [esc] cancel")

	return tea.NewView(borderStyle.Render(b.String()))
}

// IsConfirmed reports whether the user confirmed the instruction.
func (m InstructionConfirmationModel) IsConfirmed() bool {
	return m.confirmed
}

// IsCancelled reports whether the user cancelled the instruction.
func (m InstructionConfirmationModel) IsCancelled() bool {
	return m.cancelled
}

// Data returns the instruction data the model was constructed with.
func (m InstructionConfirmationModel) Data() InstructionConfirmationData {
	return m.data
}

// riskLevelStyle returns a lipgloss style for the given risk level label.
func riskLevelStyle(level RiskLevel) lipgloss.Style {
	switch level {
	case RiskCritical:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorError)
	case RiskHigh:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorError)
	case RiskMedium:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorWarning)
	case RiskLow:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess)
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorForeground)
	}
}

// riskBorderColor returns the border color for the given risk level.
func riskBorderColor(level RiskLevel) color.Color {
	switch level {
	case RiskCritical, RiskHigh:
		return ColorError
	case RiskMedium:
		return ColorWarning
	case RiskLow:
		return ColorSuccess
	default:
		return ColorAccent
	}
}
