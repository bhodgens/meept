// Package components provides reusable TUI components.
package components

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// PhaseArtifact represents a produced or consumed artifact for a phase.
// This is a TUI-local representation that avoids importing the plan package
// (which would create an import cycle through agent -> tui coupling).
type PhaseArtifact struct {
	Name        string
	Kind        string
	Description string
	Required    bool
}

// PhaseRow represents a single phase row for the plan view widget.
type PhaseRow struct {
	Name           string
	State          string
	Sequence       int
	TotalSteps     int
	CompletedSteps int
	Produces       []PhaseArtifact
	Consumes       []PhaseArtifact
}

// PlanViewConfig configures a PlanView.
type PlanViewConfig struct {
	Title  string
	Phases []PhaseRow
	Width  int
	// DocumentNote, when set, is rendered for a phase-less plan (the
	// document-proposal line; 06-planning/03).
	DocumentNote string
}

// PlanView is a minimal, read-only rendering widget that renders a list of
// plan phases with their produces/consumes artifacts. It follows the same
// pure-renderer pattern as Sparkline (no bubbletea Model implementation).
// Embed or compose this in a larger tea.Model and call Render() from View().
type PlanView struct {
	config PlanViewConfig
}

// NewPlanView creates a new PlanView from the supplied config.
func NewPlanView(cfg PlanViewConfig) *PlanView {
	return &PlanView{config: cfg}
}

// SetPhases replaces the rendered phase list.
func (m *PlanView) SetPhases(phases []PhaseRow) {
	m.config.Phases = phases
}

// SetTitle replaces the widget title.
func (m *PlanView) SetTitle(title string) {
	m.config.Title = title
}

// SetDocumentNote sets a single-line note rendered when the plan has no
// phases (the shared document-proposal wording for evolver plans,
// models.DocumentProposalLine / planCountLine(0,0); 06-planning/03).
// Empty clears it.
func (m *PlanView) SetDocumentNote(note string) {
	m.config.DocumentNote = note
}

// Render returns the rendered plan view as a string.
func (m *PlanView) Render() string {
	if len(m.config.Phases) == 0 {
		if m.config.Title == "" && m.config.DocumentNote == "" {
			return lipgloss.NewStyle().Faint(true).Render("no phases")
		}
		// A titled plan with no phases is a document proposal (evolver
		// markdown plans): render the title + note instead of the bare
		// "no phases" placeholder (06-planning/03).
		var b strings.Builder
		if m.config.Title != "" {
			titleStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("214"))
			b.WriteString(titleStyle.Render(m.config.Title))
			b.WriteString("\n")
		}
		if m.config.DocumentNote != "" {
			b.WriteString(lipgloss.NewStyle().Faint(true).Render(m.config.DocumentNote))
			b.WriteString("\n")
		}
		return b.String()
	}

	var b strings.Builder

	title := m.config.Title
	if title == "" {
		title = "phases"
	}
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("214"))
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")

	stateColors := map[string]string{
		"completed": "46",
		"approved":  "214",
		"pending":   "250",
		"failed":    "196",
		"active":    "39",
	}

	for i, p := range m.config.Phases {
		stateColorStr, ok := stateColors[p.State]
		if !ok {
			stateColorStr = "250"
		}
		stateColor := lipgloss.Color(stateColorStr)
		stateLabel := lipgloss.NewStyle().Foreground(stateColor).Render(fmt.Sprintf("[%s]", p.State))

		header := fmt.Sprintf("%d. %s %s  %d/%d steps",
			i+1, p.Name, stateLabel, p.CompletedSteps, p.TotalSteps)
		b.WriteString(header)
		b.WriteString("\n")

		// Produces artifacts
		for _, a := range p.Produces {
			line := fmt.Sprintf("    produces: %s (%s)", a.Name, a.Kind)
			if a.Description != "" {
				line += fmt.Sprintf(" - %s", a.Description)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}

		// Consumes artifacts
		for _, a := range p.Consumes {
			required := ""
			if a.Required {
				required = ", required"
			}
			line := fmt.Sprintf("    consumes: %s (%s%s)", a.Name, a.Kind, required)
			if a.Description != "" {
				line += fmt.Sprintf(" - %s", a.Description)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
