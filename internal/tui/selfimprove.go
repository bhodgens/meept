package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// PendingFix represents a fix awaiting human approval.
type PendingFix struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	File        string `json:"file"`
	Risk        string `json:"risk"`
	Diff        string `json:"diff"`
}

// SelfImprovePanel is a bubbletea Model for reviewing and approving/rejecting
// pending self-improvement fixes via the RPC daemon.
type SelfImprovePanel struct {
	client       *RPCClient
	pendingFixes []PendingFix
	selectedIdx  int
	width        int
	height       int
	loading      bool
	err          string
	lastAction   string
}

// si* styles are lazily built so they resolve the runtime palette on first
// use instead of freezing literals at init time.
var (
	siTitleStyle = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(Current().Info) // was #7dc4ff
	})
	siFixStyle = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Foreground(Current().TextPrimary) // was #c0caf5
	})
	siSelStyle = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(Current().Success) // was #9ece6a
	})
	siRiskLow = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Foreground(Current().Success)
	})
	// palette-exempt: #e0af68 (terminal amber family, no exact palette role)
	siRiskMed = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")) // palette-exempt
	})
	siRiskHigh = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Foreground(Current().ErrorC) // was #f7768e
	})
	siHelpStyle = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Foreground(Current().TextMuted) // was #565f89
	})
	siErrorStyle = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Foreground(Current().ErrorC)
	})
	siOkStyle = sync.OnceValue(func() lipgloss.Style {
		return lipgloss.NewStyle().Foreground(Current().Success)
	})
)

// NewSelfImprovePanel creates a new approval panel.
func NewSelfImprovePanel(client *RPCClient) *SelfImprovePanel {
	return &SelfImprovePanel{
		client: client,
	}
}

// Init implements tea.Model.
func (p *SelfImprovePanel) Init() tea.Cmd {
	return p.fetchPending()
}

// fetchPending returns a tea.Cmd that loads pending approvals from the daemon.
func (p *SelfImprovePanel) fetchPending() tea.Cmd {
	return func() tea.Msg {
		// This is a no-op if client is nil (offline mode).
		if p.client == nil {
			return pendingFixesMsg{fixes: nil}
		}
		result, err := p.client.Call("selfimprove.status", nil)
		if err != nil {
			return pendingFixesMsg{err: err}
		}
		var status struct {
			PendingApprovals []string `json:"pending_approvals"`
		}
		if err := json.Unmarshal(result, &status); err != nil {
			return pendingFixesMsg{err: err}
		}
		fixes := make([]PendingFix, len(status.PendingApprovals))
		for i, id := range status.PendingApprovals {
			fixes[i] = PendingFix{ID: id}
		}
		return pendingFixesMsg{fixes: fixes}
	}
}

type pendingFixesMsg struct {
	fixes []PendingFix
	err   error
}

// Update implements tea.Model.
func (p *SelfImprovePanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pendingFixesMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err.Error()
			return p, nil
		}
		p.pendingFixes = msg.fixes
		if p.selectedIdx >= len(p.pendingFixes) {
			p.selectedIdx = 0
		}
		return p, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "a":
			cmd := p.approveCurrent()
			return p, cmd
		case "r":
			cmd := p.rejectCurrent()
			return p, cmd
		case "j", KeyDown:
			if p.selectedIdx < len(p.pendingFixes)-1 {
				p.selectedIdx++
			}
		case "k", "up":
			if p.selectedIdx > 0 {
				p.selectedIdx--
			}
		case "R":
			// Refresh
			p.loading = true
			cmd := p.fetchPending()
			return p, cmd
		}
	case actionResultMsg:
		p.lastAction = msg.desc
		cmd := p.fetchPending()
		return p, cmd
	}
	return p, nil
}

type actionResultMsg struct {
	desc string
}

func (p *SelfImprovePanel) approveCurrent() tea.Cmd {
	if p.client == nil || p.selectedIdx >= len(p.pendingFixes) {
		return nil
	}
	fix := p.pendingFixes[p.selectedIdx]
	return func() tea.Msg {
		_, err := p.client.Call("selfimprove.apply", map[string]string{"fix_id": fix.ID})
		if err != nil {
			return actionResultMsg{desc: fmt.Sprintf("approve %s failed: %s", fix.ID, err)}
		}
		return actionResultMsg{desc: fmt.Sprintf("approved %s", fix.ID)}
	}
}

func (p *SelfImprovePanel) rejectCurrent() tea.Cmd {
	if p.client == nil || p.selectedIdx >= len(p.pendingFixes) {
		return nil
	}
	fix := p.pendingFixes[p.selectedIdx]
	return func() tea.Msg {
		_, err := p.client.Call("selfimprove.reject", map[string]string{"fix_id": fix.ID, "reason": "rejected via tui"})
		if err != nil {
			return actionResultMsg{desc: fmt.Sprintf("reject %s failed: %s", fix.ID, err)}
		}
		return actionResultMsg{desc: fmt.Sprintf("rejected %s", fix.ID)}
	}
}

// View implements tea.Model.
func (p *SelfImprovePanel) View() tea.View {
	var b strings.Builder

	b.WriteString(siTitleStyle().Render("self-improve - pending approvals"))
	b.WriteString("\n")

	if p.loading {
		b.WriteString("loading...\n")
		return tea.NewView(b.String())
	}

	if p.err != "" {
		b.WriteString(siErrorStyle().Render("error: " + p.err))
		b.WriteString("\n")
		return tea.NewView(b.String())
	}

	if len(p.pendingFixes) == 0 {
		b.WriteString(siOkStyle().Render("no pending approvals"))
		b.WriteString("\n")
		return tea.NewView(b.String())
	}

	for i, fix := range p.pendingFixes {
		style := siFixStyle()
		marker := "  "
		if i == p.selectedIdx {
			style = siSelStyle()
			marker = "> "
		}
		riskStyle := siRiskLow()
		switch fix.Risk {
		case "medium":
			riskStyle = siRiskMed()
		case "high":
			riskStyle = siRiskHigh()
		}

		line := fmt.Sprintf("%s%s  [%s]  %s", marker, fix.ID, riskStyle.Render(fix.Risk), fix.File)
		if fix.Description != "" {
			line += fmt.Sprintf("  %s", fix.Description)
		}
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	if p.lastAction != "" {
		b.WriteString(siOkStyle().Render(p.lastAction))
		b.WriteString("\n")
	}

	b.WriteString(siHelpStyle().Render("a: approve  r: reject  j/k: navigate  R: refresh"))
	b.WriteString("\n")

	return tea.NewView(b.String())
}

// SetSize sets the panel dimensions.
func (p *SelfImprovePanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}
