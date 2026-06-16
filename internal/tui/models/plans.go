package models

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// PlanFilter defines filter options for the plans view.
type PlanFilter int

const (
	PlanFilterAll PlanFilter = iota
	PlanFilterActive
	PlanFilterPending
	PlanFilterCompleted
)

// PlansModel is the model for the plans tab.
type PlansModel struct {
	rpc       PlansRPCClient
	plans     []types.PlanExtended
	table     table.Model
	selected  *types.PlanExtended
	width     int
	height    int
	loading   bool
	err       error
	filter    PlanFilter
	sessionID string

	// UI state
	showingDetail bool
	showingHelp   bool
}

// PlansRPCClient interface for the plans model.
type PlansRPCClient interface {
	Call(method string, params any) (json.RawMessage, error)
	IsConnected() bool
}

// NewPlansModel creates a new plans model.
func NewPlansModel(rpcClient PlansRPCClient) *PlansModel {
	columns := []table.Column{
		{Title: "title", Width: 22},
		{Title: ColState, Width: 10},
		{Title: "phases", Width: 8},
		{Title: "steps", Width: 8},
		{Title: "progress", Width: 12},
		{Title: "updated", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("#F97316"))

	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#F97316")).
		Bold(true)

	t.SetStyles(s)

	return &PlansModel{
		rpc:    rpcClient,
		table:  t,
		filter: PlanFilterAll,
	}
}

// PlansUpdateMsg carries the plans update.
type PlansUpdateMsg struct {
	Plans []types.PlanExtended
	Err   error
}

// PlanActionMsg carries the result of a plan action (approve/reject/etc.).
type PlanActionMsg struct {
	PlanID string
	Action string
	Err    error
}

// SetSession sets the session ID to filter plans for.
func (m *PlansModel) SetSession(sessionID string) {
	m.sessionID = sessionID
}

// SetSize updates the model dimensions.
func (m *PlansModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	tableHeight := max(height-12, 5)
	m.table.SetHeight(tableHeight)

	m.setPlansColumns()
}

func (m *PlansModel) setPlansColumns() {
	// Clear rows before changing columns to prevent panic from row/column mismatch
	m.table.SetRows([]table.Row{})

	available := m.width - 10 // borders/padding
	titleW := available * 26 / 100
	stateW := 10
	phasesW := 8
	stepsW := 8
	progressW := 12
	updatedW := 10

	if titleW < 15 {
		titleW = 15
	}

	m.table.SetColumns([]table.Column{
		{Title: "title", Width: titleW},
		{Title: ColState, Width: stateW},
		{Title: "phases", Width: phasesW},
		{Title: "steps", Width: stepsW},
		{Title: "progress", Width: progressW},
		{Title: "updated", Width: updatedW},
	})
}

// Init initializes the plans model.
func (m *PlansModel) Init() tea.Cmd {
	return m.fetchPlans
}

func (m *PlansModel) fetchPlans() tea.Msg {
	var params map[string]any
	if m.sessionID != "" {
		params = map[string]any{"session_id": m.sessionID}
	}

	raw, err := m.rpc.Call("plan.list_by_session", params)
	if err != nil {
		return PlansUpdateMsg{Err: err}
	}

	var resp types.PlanListResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return PlansUpdateMsg{Err: fmt.Errorf("unmarshal plans: %w", err)}
	}

	if resp.Err != "" {
		return PlansUpdateMsg{Err: fmt.Errorf("%s", resp.Err)}
	}

	return PlansUpdateMsg{Plans: resp.Plans}
}

// Update handles messages for the plans view.
func (m *PlansModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case PlansUpdateMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.plans = msg.Plans
		m.updatePlansTable()
		return nil

	case PlanActionMsg:
		// After an action completes, refresh plans
		if msg.Err != nil {
			m.err = msg.Err
		}
		m.loading = true
		return m.fetchPlans

	case tea.KeyPressMsg:
		// Handle detail modal first
		if m.showingDetail {
			if msg.String() == KeyEsc || msg.String() == "q" {
				m.showingDetail = false
				return nil
			}
			return nil
		}

		if m.showingHelp {
			m.showingHelp = false
			return nil
		}

		switch msg.String() {
		case "r":
			// Refresh
			m.loading = true
			return m.fetchPlans

		case "?":
			m.showingHelp = true
			return nil

		case "/":
			// Cycle through filters
			m.filter = (m.filter + 1) % 4
			m.updatePlansTable()
			return nil

		case "a":
			// Approve selected plan
			if m.selected != nil {
				return m.approvePlan(m.selected.ID)
			}
			return nil

		case "v":
			// Revise selected plan
			if m.selected != nil {
				return m.revisePlan(m.selected.ID)
			}
			return nil

		case "c":
			// Confirm selected plan
			if m.selected != nil {
				return m.confirmPlan(m.selected.ID)
			}
			return nil

		case KeyEnter:
			// Open detail modal
			if len(m.plans) > 0 {
				filtered := m.filterPlans()
				idx := m.table.Cursor()
				if idx >= 0 && idx < len(filtered) {
					m.selected = &filtered[idx]
					m.showingDetail = true
				}
			}
			return nil

		case KeyEsc:
			m.selected = nil
			m.showingDetail = false
			return nil

		case "up", "down", "j", "k":
			// Let table handle navigation
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			// Update selection as we navigate
			idx := m.table.Cursor()
			filtered := m.filterPlans()
			if idx >= 0 && idx < len(filtered) {
				m.selected = &filtered[idx]
			}
			return cmd
		}
	}

	// Pass other messages to table
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return cmd
}

func (m *PlansModel) approvePlan(planID string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.rpc.Call("plan.approve", map[string]any{"plan_id": planID})
		return PlanActionMsg{PlanID: planID, Action: "approve", Err: err}
	}
}

func (m *PlansModel) revisePlan(planID string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.rpc.Call("plan.revise", map[string]any{"plan_id": planID})
		return PlanActionMsg{PlanID: planID, Action: "revise", Err: err}
	}
}

func (m *PlansModel) confirmPlan(planID string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.rpc.Call("plan.confirm", map[string]any{"plan_id": planID})
		return PlanActionMsg{PlanID: planID, Action: "confirm", Err: err}
	}
}

// filterPlans returns plans matching the current filter.
func (m *PlansModel) filterPlans() []types.PlanExtended {
	var filtered []types.PlanExtended

	for _, p := range m.plans {
		include := false
		switch m.filter {
		case PlanFilterAll:
			include = true
		case PlanFilterActive:
			if p.State == "planning" || p.State == "executing" || p.State == "approved" {
				include = true
			}
		case PlanFilterPending:
			if p.State == "draft" || p.State == "pending_approval" {
				include = true
			}
		case PlanFilterCompleted:
			if p.State == "completed" || p.State == "confirmed" || p.State == "cancelled" || p.State == "failed" {
				include = true
			}
		default:
			include = true
		}

		if include {
			filtered = append(filtered, p)
		}
	}

	return filtered
}

func (m *PlansModel) updatePlansTable() {
	plans := m.filterPlans()
	rows := make([]table.Row, len(plans))

	for i, plan := range plans {
		stateIcon := m.getPlanStateIcon(plan.State)

		// Phases count
		phasesStr := fmt.Sprintf("%d", len(plan.Phases))

		// Steps column
		stepsStr := fmt.Sprintf("%d/%d", plan.CompletedSteps, plan.TotalSteps)

		// Progress bar
		progress := m.renderProgressBar(plan.CompletedSteps, plan.TotalSteps, 8)

		// Updated time
		updated := m.formatTimeAgo(plan.UpdatedAt)

		// Title
		title := plan.Title
		if title == "" {
			title = types.TruncateString(plan.ID, 20)
		}

		rows[i] = table.Row{
			types.TruncateString(title, 22),
			stateIcon,
			phasesStr,
			stepsStr,
			progress,
			updated,
		}
	}
	m.table.SetRows(rows)
	if len(rows) > 0 {
		m.table.GotoTop()
	}
}

// View renders the plans view.
func (m *PlansModel) View() string {
	// Detail modal overlay
	if m.showingDetail && m.selected != nil {
		return m.renderPlanDetailModal()
	}

	if m.showingHelp {
		return m.renderHelp()
	}

	if m.loading && len(m.plans) == 0 {
		return m.renderLoading()
	}

	if m.err != nil && len(m.plans) == 0 {
		return m.renderError()
	}

	var b strings.Builder

	// Header with filter tabs
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Table
	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151"))

	b.WriteString(tableStyle.Render(m.table.View()))
	b.WriteString("\n")

	// Detail preview panel
	if m.selected != nil {
		b.WriteString(m.renderPlanPreview())
	} else {
		b.WriteString(m.renderEmptyDetail())
	}

	// Help hint
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		MarginTop(1)

	b.WriteString(hintStyle.Render("r: refresh | /: filter | a: approve | v: revise | c: confirm | enter: details | ?: help"))

	return b.String()
}

func (m *PlansModel) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316"))

	tabInactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Background(lipgloss.Color("#1F2937")).
		Padding(0, 1)

	tabActiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#F97316")).
		Bold(true).
		Padding(0, 1)

	filterStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber)).
		Padding(0, 1)

	title := titleStyle.Render("plans")

	// Filter tabs
	var allTab, activeTab, pendingTab, completedTab string
	switch m.filter {
	case PlanFilterAll:
		allTab = tabActiveStyle.Render("all")
		activeTab = tabInactiveStyle.Render("active")
		pendingTab = tabInactiveStyle.Render("pending")
		completedTab = tabInactiveStyle.Render("completed")
	case PlanFilterActive:
		allTab = tabInactiveStyle.Render("all")
		activeTab = tabActiveStyle.Render("active")
		pendingTab = tabInactiveStyle.Render("pending")
		completedTab = tabInactiveStyle.Render("completed")
	case PlanFilterPending:
		allTab = tabInactiveStyle.Render("all")
		activeTab = tabInactiveStyle.Render("active")
		pendingTab = tabActiveStyle.Render("pending")
		completedTab = tabInactiveStyle.Render("completed")
	case PlanFilterCompleted:
		allTab = tabInactiveStyle.Render("all")
		activeTab = tabInactiveStyle.Render("active")
		pendingTab = tabInactiveStyle.Render("pending")
		completedTab = tabActiveStyle.Render("completed")
	}

	tabs := allTab + " " + activeTab + " " + pendingTab + " " + completedTab

	// Plan count
	filtered := m.filterPlans()
	countStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray))
	count := countStyle.Render(fmt.Sprintf("(%d/%d)", len(filtered), len(m.plans)))

	header := lipgloss.JoinHorizontal(
		lipgloss.Left,
		title,
		"  ",
		tabs,
		"  ",
		count,
	)

	// Session indicator
	if m.sessionID != "" {
		sessStyle := filterStyle.Render(fmt.Sprintf("session: %s", types.TruncateString(m.sessionID, 12)))
		header = lipgloss.JoinHorizontal(lipgloss.Left, header, "  ", sessStyle)
	}

	return header
}

func (m *PlansModel) renderPlanPreview() string {
	plan := m.selected
	if plan == nil {
		return m.renderEmptyDetail()
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F97316")).
		Padding(0, 1).
		Width(m.width - 4)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Width(12)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	var content strings.Builder

	content.WriteString(labelStyle.Render("id:"))
	content.WriteString(valueStyle.Render(types.TruncateString(plan.ID, 40)))
	content.WriteString("\n")

	if plan.Description != "" {
		content.WriteString(labelStyle.Render("desc:"))
		content.WriteString(valueStyle.Render(types.TruncateString(plan.Description, 60)))
		content.WriteString("\n")
	}

	// File path
	if plan.FilePath != "" {
		content.WriteString(labelStyle.Render("file:"))
		content.WriteString(valueStyle.Render(types.TruncateString(plan.FilePath, 50)))
		content.WriteString("\n")
	}

	// Phase summary
	if len(plan.Phases) > 0 {
		content.WriteString(labelStyle.Render("phases:"))
		var phaseParts []string
		for _, ph := range plan.Phases {
			phaseParts = append(phaseParts, fmt.Sprintf("%s (%d/%d)", ph.Name, ph.CompletedSteps, ph.TotalSteps))
		}
		phaseStr := strings.Join(phaseParts, ", ")
		if len(phaseStr) > 50 {
			phaseStr = phaseStr[:47] + "..."
		}
		content.WriteString(valueStyle.Render(phaseStr))
		content.WriteString("\n")
	}

	// Revision count
	if plan.RevisionCount > 0 {
		revStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAmber))
		content.WriteString(labelStyle.Render("revisions:"))
		content.WriteString(revStyle.Render(fmt.Sprintf("%d", plan.RevisionCount)))
		content.WriteString("\n")
	}

	return panelStyle.Render(content.String())
}

func (m *PlansModel) renderPlanDetailModal() string {
	plan := m.selected
	if plan == nil {
		return ""
	}

	modalWidth := min(m.width-8, 80)

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("#F97316")).
		Padding(1, 2).
		Width(modalWidth)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316")).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber)).
		Bold(true).
		MarginTop(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Width(14)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	stateColor := m.getPlanStateColor(plan.State)
	stateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(stateColor)).
		Bold(true)

	var content strings.Builder

	// Title
	name := plan.Title
	if name == "" {
		name = plan.ID
	}
	content.WriteString(titleStyle.Render("plan: " + types.TruncateString(name, 50)))
	content.WriteString("\n\n")

	// Basic info
	content.WriteString(labelStyle.Render("id:"))
	content.WriteString(valueStyle.Render(plan.ID))
	content.WriteString("\n")

	content.WriteString(labelStyle.Render("state:"))
	content.WriteString(stateStyle.Render(m.getPlanStateIcon(plan.State)))
	content.WriteString("\n")

	if plan.ProjectID != "" {
		content.WriteString(labelStyle.Render("project:"))
		content.WriteString(valueStyle.Render(plan.ProjectID))
		content.WriteString("\n")
	}

	if plan.SourceSession != "" {
		content.WriteString(labelStyle.Render("session:"))
		content.WriteString(valueStyle.Render(plan.SourceSession))
		content.WriteString("\n")
	}

	if plan.TaskID != "" {
		content.WriteString(labelStyle.Render("task:"))
		content.WriteString(valueStyle.Render(plan.TaskID))
		content.WriteString("\n")
	}

	if plan.FilePath != "" {
		content.WriteString(labelStyle.Render("file:"))
		content.WriteString(valueStyle.Render(plan.FilePath))
		content.WriteString("\n")
	}

	content.WriteString(labelStyle.Render("created:"))
	content.WriteString(valueStyle.Render(plan.CreatedAt))
	content.WriteString("\n")

	content.WriteString(labelStyle.Render("updated:"))
	content.WriteString(valueStyle.Render(plan.UpdatedAt))
	content.WriteString("\n")

	if plan.RevisionCount > 0 {
		content.WriteString(labelStyle.Render("revisions:"))
		content.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAmber)).Render(fmt.Sprintf("%d", plan.RevisionCount)))
		content.WriteString("\n")
	}

	// Overall progress
	content.WriteString("\n")
	content.WriteString(labelStyle.Render("progress:"))
	progress := m.renderProgressBar(plan.CompletedSteps, plan.TotalSteps, 20)
	percent := float64(0)
	if plan.TotalSteps > 0 {
		percent = float64(plan.CompletedSteps) / float64(plan.TotalSteps) * 100
	}
	content.WriteString(valueStyle.Render(fmt.Sprintf("%s (%.0f%%)", progress, percent)))
	content.WriteString("\n")

	completedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGreen))
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed))
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray))

	pending := max(plan.TotalSteps-plan.CompletedSteps-plan.FailedSteps, 0)
	content.WriteString("              ")
	content.WriteString(completedStyle.Render(fmt.Sprintf("ok %d completed", plan.CompletedSteps)))
	content.WriteString("  ")
	content.WriteString(pendingStyle.Render(fmt.Sprintf("o %d pending", pending)))
	content.WriteString("  ")
	content.WriteString(failedStyle.Render(fmt.Sprintf("x %d failed", plan.FailedSteps)))
	content.WriteString("\n")

	// Description
	if plan.Description != "" {
		content.WriteString("\n")
		content.WriteString(sectionStyle.Render("--- description ---"))
		content.WriteString("\n")

		desc := plan.Description
		maxDescLen := max(modalWidth-10, 20)
		if len(desc) > maxDescLen {
			desc = desc[:maxDescLen-3] + "..."
		}
		content.WriteString(valueStyle.Render(desc))
		content.WriteString("\n")
	}

	// Phases section
	if len(plan.Phases) > 0 {
		content.WriteString("\n")
		content.WriteString(sectionStyle.Render("--- phases ---"))
		content.WriteString("\n")

		for _, phase := range plan.Phases {
			phaseColor := m.getPlanStateColor(phase.State)
			phaseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(phaseColor))

			bar := m.renderPhaseBar(phase, 16)

			fmt.Fprintf(&content, " %d. %s %s %s\n",
				phase.Sequence,
				phaseStyle.Render(m.getPlanStateIcon(phase.State)),
				valueStyle.Render(phase.Name),
				phaseStyle.Render(bar),
			)
		}
	}

	// Footer
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Italic(true)
	content.WriteString(footerStyle.Render("[esc/q] close | a: approve | v: revise | c: confirm"))

	return modalStyle.Render(content.String())
}

func (m *PlansModel) renderPhaseBar(phase types.PlanPhaseView, width int) string {
	if phase.TotalSteps == 0 {
		return strings.Repeat("░", width)
	}

	filledWidth := (phase.CompletedSteps * width) / phase.TotalSteps
	emptyWidth := width - filledWidth

	if filledWidth > width {
		filledWidth = width
		emptyWidth = 0
	}

	bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", emptyWidth)
	return fmt.Sprintf("%s %d/%d", bar, phase.CompletedSteps, phase.TotalSteps)
}

func (m *PlansModel) renderProgressBar(completed, total, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}

	filledWidth := (completed * width) / total
	emptyWidth := width - filledWidth

	if filledWidth > width {
		filledWidth = width
		emptyWidth = 0
	}

	bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", emptyWidth)
	return fmt.Sprintf("%s %d/%d", bar, completed, total)
}

func (m *PlansModel) formatTimeAgo(timestamp string) string {
	if timestamp == "" {
		return StatusNA
	}
	if len(timestamp) > 5 {
		return types.TruncateString(timestamp[len(timestamp)-8:], 8)
	}
	return timestamp
}

func (m *PlansModel) getPlanStateIcon(state string) string {
	switch state {
	case "planning":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6")).Render("●") + " plan"
	case "draft":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Render("●") + " draft"
	case "pending_approval":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6")).Render("●") + " review"
	case "approved":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGreen)).Render("✓") + " ok"
	case "executing":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAmber)).Render("●") + " exec"
	case "completed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGreen)).Render("●") + " done"
	case "confirmed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGreen)).Bold(true).Render("★") + " ok"
	case "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Render("✗") + " fail"
	case "cancelled":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Render("○") + " cancel"
	default:
		return "? " + types.TruncateString(state, 4)
	}
}

func (m *PlansModel) getPlanStateColor(state string) string {
	switch state {
	case "planning":
		return "#3B82F6" // Blue
	case "draft":
		return ColorGray
	case "pending_approval":
		return "#3B82F6" // Blue
	case "approved":
		return ColorGreen
	case "executing":
		return ColorAmber
	case "completed":
		return ColorGreen
	case "confirmed":
		return ColorGreen
	case "failed":
		return ColorRed
	case "cancelled":
		return ColorGray
	default:
		return ColorGray
	}
}

func (m *PlansModel) renderLoading() string {
	style := lipgloss.NewStyle().
		Width(m.width-4).
		Align(lipgloss.Center).
		Padding(4, 0)

	return style.Render("loading plans...")
}

func (m *PlansModel) renderError() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorRed)).
		Padding(1, 2).
		Width(m.width - 4)

	errMsg := "unknown error"
	if m.err != nil {
		errMsg = fmt.Sprintf("%v", m.err)
	}

	return style.Render(
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true).Render("error") +
			"\n\n" +
			errMsg +
			"\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Render("press 'r' to refresh"),
	)
}

func (m *PlansModel) renderEmptyDetail() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 2).
		Width(m.width - 4)

	content := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Italic(true).
		Render("select a plan to view details")

	return panelStyle.Render(content)
}

func (m *PlansModel) renderHelp() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F97316")).
		Padding(2, 4).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316")).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber)).
		Bold(true).
		MarginTop(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber)).
		Bold(true).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	content := titleStyle.Render("plans view help") + "\n\n"

	content += sectionStyle.Render("navigation") + "\n"
	content += keyStyle.Render("up/k") + descStyle.Render("move cursor up") + "\n"
	content += keyStyle.Render("down/j") + descStyle.Render("move cursor down") + "\n"
	content += keyStyle.Render("enter") + descStyle.Render("open plan detail modal") + "\n"
	content += keyStyle.Render("esc") + descStyle.Render("close modal / clear selection") + "\n"

	content += "\n" + sectionStyle.Render("view controls") + "\n"
	content += keyStyle.Render("/") + descStyle.Render("cycle filter (all/active/pending/completed)") + "\n"
	content += keyStyle.Render("r") + descStyle.Render("refresh plans") + "\n"
	content += keyStyle.Render("?") + descStyle.Render("toggle this help") + "\n"

	content += "\n" + sectionStyle.Render("plan actions") + "\n"
	content += keyStyle.Render("a") + descStyle.Render("approve selected plan") + "\n"
	content += keyStyle.Render("v") + descStyle.Render("revise selected plan") + "\n"
	content += keyStyle.Render("c") + descStyle.Render("confirm completed plan") + "\n"

	content += "\n" + sectionStyle.Render("state icons") + "\n"
	content += keyStyle.Render("● plan") + descStyle.Render("planning") + "\n"
	content += keyStyle.Render("● draft") + descStyle.Render("draft") + "\n"
	content += keyStyle.Render("● review") + descStyle.Render("pending approval") + "\n"
	content += keyStyle.Render("✓ ok") + descStyle.Render("approved") + "\n"
	content += keyStyle.Render("● exec") + descStyle.Render("executing") + "\n"
	content += keyStyle.Render("● done") + descStyle.Render("completed") + "\n"
	content += keyStyle.Render("★ ok") + descStyle.Render("confirmed") + "\n"
	content += keyStyle.Render("✗ fail") + descStyle.Render("failed") + "\n"
	content += keyStyle.Render("○ cancel") + descStyle.Render("cancelled") + "\n"

	content += "\n"
	content += lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Render("press any key to close")

	return panelStyle.Render(content)
}
