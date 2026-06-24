// Package tui — agents_panel.go implements the Bubbletea panel for AI
// employees (Phase 9 of the AI Employee Design spec,
// docs/superpowers/specs/2026-06-23-ai-employee-design.md §"TUI").
//
// The panel surfaces four sub-views (list, detail, approval queue, audit
// findings) behind a single Model. It follows the same pattern as
// internal/tui/models/plans.go: a table plus a detail preview, with
// loading/error/empty render states.
//
// RPC methods consumed (spec §"RPC"):
//   - agents.list              -> list employees
//   - agents.get               -> single employee (drill-in)
//   - agents.goals.list        -> active goals w/ health
//   - agents.audit.list        -> recent findings by severity
//   - agents.goals.approve     -> approve a tier-2 plan
//   - agents.goals.reject      -> reject a tier-2 plan (requires reason)
//   - agents.audit.resolve     -> resolve finding as false_positive
//   - agents.pause / resume    -> runtime control
package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// agentsSubView identifies which sub-view of the agents panel is active.
type agentsSubView int

const (
	agentsViewList       agentsSubView = iota // default: agents list
	agentsViewDetail                          // drill-in: constitution / goals / audit / state
	agentsViewApprovals                       // tier-2 plans awaiting user signoff
	agentsViewAudit                           // severity-colored audit findings
)

// AgentsPanel is the Bubbletea Model for the employees panel.
//
// It is constructed by App and embeds an RPCClient for daemon calls.
// All rendering uses the same lipgloss styles and lowercase-text rule as
// the rest of the TUI (CLAUDE.md UI convention).
type AgentsPanel struct {
	rpc AgentsRPCClient

	// Data caches (populated by RPC fetches).
	agents []AgentSummary

	// Currently selected agent ID (drives the detail view).
	selectedID string

	// Detail snapshot fetched on drill-in.
	detail *AgentDetail

	// UI state.
	subView agentsSubView
	table   table.Model
	width   int
	height  int

	loading bool
	err     error
}

// AgentsRPCClient is the small interface App satisfies via *RPCClient.
// Declared here so tests can inject a stub.
type AgentsRPCClient interface {
	Call(method string, params any) (json.RawMessage, error)
	IsConnected() bool
}

// AgentSummary is the row model used by the agents list. It mirrors the
// fields the daemon returns for agents.list plus a few values (drift,
// daily cost) that the manager computes from bot state.
type AgentSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Role          string    `json:"role"`
	Status        string    `json:"status"` // running | paused | error | stopped
	Tier          string    `json:"tier"`   // tier_1_reactive | tier_2_propose | tier_3_autonomous
	DriftScore    float64   `json:"drift_score"`
	DailyCostCents int      `json:"daily_cost_cents"`
	FindingsCount int       `json:"findings_count"`
	LastInvocation time.Time `json:"last_invocation"`
}

// AgentDetail is the drill-in payload. Combines the employee definition
// with its constitution summary, active goals, and recent findings.
type AgentDetail struct {
	Agent            AgentSummary   `json:"agent"`
	Purpose          string         `json:"purpose"`
	Charter          string         `json:"charter"`
	Never            []string       `json:"never"`
	ToolsAllowed     []string       `json:"tools_allowed"`
	ToolsForbidden   []string       `json:"tools_forbidden"`
	RiskCeiling      string         `json:"risk_ceiling"`
	EscalatesTo      []string       `json:"escalates_to"`
	ActiveGoals      []AgentGoal    `json:"active_goals"`
	RecentFindings   []AgentFinding `json:"recent_findings"`
}

// AgentGoal mirrors employee.Goal for wire transport.
type AgentGoal struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Mandate      string `json:"mandate"`
	State        string `json:"state"`  // active | paused | retired
	Health       string `json:"health"` // healthy | at_risk | broken | unknown
	ActivePlanID string `json:"active_plan_id,omitempty"`
	LastAssessed time.Time `json:"last_assessed"`
}

// AgentFinding mirrors employee.AuditFinding for wire transport.
type AgentFinding struct {
	ID           string    `json:"id"`
	EmployeeID   string    `json:"employee_id"`
	Severity     string    `json:"severity"` // info | warning | critical
	Checkpoint   string    `json:"checkpoint"`
	ViolatedRule string    `json:"violated_rule,omitempty"`
	Evidence     string    `json:"evidence,omitempty"`
	DetectedAt   time.Time `json:"detected_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	Resolution   string    `json:"resolution,omitempty"`
	DriftScore   float64   `json:"drift_score,omitempty"`
}

// NewAgentsPanel constructs the panel with default table styling.
func NewAgentsPanel(rpc AgentsRPCClient) *AgentsPanel {
	columns := []table.Column{
		{Title: "id", Width: 18},
		{Title: "status", Width: 10},
		{Title: "tier", Width: 16},
		{Title: "drift", Width: 8},
		{Title: "cost", Width: 10},
		{Title: "findings", Width: 8},
		{Title: "last run", Width: 12},
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

	return &AgentsPanel{
		rpc:     rpc,
		table:   t,
		subView: agentsViewList,
	}
}

// SetSize updates the panel dimensions and resizes the table.
func (p *AgentsPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	tableHeight := max(height-12, 5)
	p.table.SetHeight(tableHeight)
	p.resizeColumns()
}

func (p *AgentsPanel) resizeColumns() {
	// Clear rows before resizing columns to prevent row/column mismatch panic.
	p.table.SetRows([]table.Row{})

	if p.width < 30 {
		return
	}
	available := p.width - 10
	idW := available * 22 / 100
	if idW < 12 {
		idW = 12
	}
	statusW := 10
	tierW := 16
	driftW := 8
	costW := 10
	findingsW := 8
	lastW := available - idW - statusW - tierW - driftW - costW - findingsW
	if lastW < 8 {
		lastW = 8
	}
	p.table.SetColumns([]table.Column{
		{Title: "id", Width: idW},
		{Title: "status", Width: statusW},
		{Title: "tier", Width: tierW},
		{Title: "drift", Width: driftW},
		{Title: "cost", Width: costW},
		{Title: "findings", Width: findingsW},
		{Title: "last run", Width: lastW},
	})
}

// Init kicks off the initial agents.list fetch.
func (p *AgentsPanel) Init() tea.Cmd {
	return p.fetchAgents
}

// agentsListMsg carries the agents.list response.
type agentsListMsg struct {
	agents []AgentSummary
	err    error
}

func (p *AgentsPanel) fetchAgents() tea.Msg {
	raw, err := p.rpc.Call("agents.list", nil)
	if err != nil {
		return agentsListMsg{err: err}
	}
	var resp struct {
		Agents []AgentSummary `json:"agents"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return agentsListMsg{err: fmt.Errorf("unmarshal agents: %w", err)}
	}
	return agentsListMsg{agents: resp.Agents}
}

// agentsDetailMsg carries the agents.get + goals + audit merged payload.
type agentsDetailMsg struct {
	detail *AgentDetail
	err    error
}

func (p *AgentsPanel) fetchDetail() tea.Msg {
	if p.selectedID == "" {
		return agentsDetailMsg{err: fmt.Errorf("no agent selected")}
	}

	// Fetch the employee record.
	raw, err := p.rpc.Call("agents.get", map[string]any{"id": p.selectedID})
	if err != nil {
		return agentsDetailMsg{err: fmt.Errorf("agents.get: %w", err)}
	}
	var emp struct {
		ID             string                 `json:"id"`
		Name           string                 `json:"name"`
		Description    string                 `json:"description"`
		Enabled        bool                   `json:"enabled"`
		Model          string                 `json:"model,omitempty"`
		Tools          []string               `json:"tools"`
		Constitution   map[string]any         `json:"constitution"`
		Constraints    map[string]any         `json:"constraints,omitempty"`
	}
	if err := json.Unmarshal(raw, &emp); err != nil {
		return agentsDetailMsg{err: fmt.Errorf("unmarshal agent: %w", err)}
	}

	detail := &AgentDetail{}
	detail.Agent.ID = emp.ID
	detail.Agent.Name = emp.Name
	detail.Agent.Role = emp.Description
	detail.Agent.Status = "stopped"
	detail.ToolsAllowed = emp.Tools

	// Pull constitution fields.
	if c, ok := emp.Constitution["purpose"].(string); ok {
		detail.Purpose = c
	}
	if c, ok := emp.Constitution["role"].(string); ok {
		detail.Agent.Role = c
	}
	if c, ok := emp.Constitution["charter"].(string); ok {
		detail.Charter = c
	}
	if c, ok := emp.Constitution["autonomy_tier"].(string); ok {
		detail.Agent.Tier = c
	}
	if c, ok := emp.Constitution["escalates_to"].([]any); ok {
		for _, v := range c {
			if s, ok := v.(string); ok {
				detail.EscalatesTo = append(detail.EscalatesTo, s)
			}
		}
	}
	if con, ok := emp.Constitution["constraints"].(map[string]any); ok {
		if v, ok := con["tools_allowed"].([]any); ok {
			for _, t := range v {
				if s, ok := t.(string); ok {
					detail.ToolsAllowed = append(detail.ToolsAllowed, s)
				}
			}
		}
		if v, ok := con["tools_forbidden"].([]any); ok {
			for _, t := range v {
				if s, ok := t.(string); ok {
					detail.ToolsForbidden = append(detail.ToolsForbidden, s)
				}
			}
		}
		if v, ok := con["never"].([]any); ok {
			for _, t := range v {
				if s, ok := t.(string); ok {
					detail.Never = append(detail.Never, s)
				}
			}
		}
		if v, ok := con["risk_ceiling"].(string); ok {
			detail.RiskCeiling = v
		}
	}

	// Fetch goals.
	rawGoals, err := p.rpc.Call("agents.goals.list", map[string]any{"employee_id": p.selectedID})
	if err == nil {
		var g struct {
			Goals []AgentGoal `json:"goals"`
		}
		if json.Unmarshal(rawGoals, &g) == nil {
			detail.ActiveGoals = g.Goals
		}
	}

	// Fetch recent audit findings (last 7 days, all severities).
	rawFindings, err := p.rpc.Call("agents.audit.list", map[string]any{
		"employee_id": p.selectedID,
		"since":       "168h",
	})
	if err == nil {
		var f struct {
			Findings []AgentFinding `json:"findings"`
		}
		if json.Unmarshal(rawFindings, &f) == nil {
			detail.RecentFindings = f.Findings
			detail.Agent.FindingsCount = len(f.Findings)
		}
	}

	return agentsDetailMsg{detail: detail}
}

// agentActionMsg carries the result of a pause/resume/approve/reject/resolve.
type agentActionMsg struct {
	action string
	err    error
}

func (p *AgentsPanel) pauseAgent() tea.Cmd {
	return func() tea.Msg {
		_, err := p.rpc.Call("agents.pause", map[string]any{"id": p.selectedID})
		return agentActionMsg{action: "pause", err: err}
	}
}

func (p *AgentsPanel) resumeAgent() tea.Cmd {
	return func() tea.Msg {
		_, err := p.rpc.Call("agents.resume", map[string]any{"id": p.selectedID})
		return agentActionMsg{action: "resume", err: err}
	}
}

// Update handles messages for the agents panel.
func (p *AgentsPanel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case agentsListMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err
			return nil
		}
		p.err = nil
		p.agents = msg.agents
		p.updateAgentsTable()
		return nil

	case agentsDetailMsg:
		if msg.err != nil {
			p.err = msg.err
			return nil
		}
		p.err = nil
		p.detail = msg.detail
		return nil

	case agentActionMsg:
		if msg.err != nil {
			p.err = msg.err
		}
		// Refresh the detail view + the list.
		return tea.Batch(p.fetchDetail, p.fetchAgents)

	case tea.KeyPressMsg:
		if p.subView == agentsViewDetail {
			return p.handleDetailKey(msg)
		}

		switch msg.String() {
		case "r":
			p.loading = true
			return p.fetchAgents

		case "?":
			// Toggle help is handled by the parent App for the list view;
			// here we just consume the key.
			return nil

		case KeyEnter:
			// Drill into the selected agent.
			if len(p.agents) == 0 {
				return nil
			}
			idx := p.table.Cursor()
			if idx < 0 || idx >= len(p.agents) {
				return nil
			}
			p.selectedID = p.agents[idx].ID
			p.subView = agentsViewDetail
			return p.fetchDetail

		case KeyEsc:
			p.subView = agentsViewList
			p.detail = nil
			return nil

		case "1":
			p.subView = agentsViewList
			return p.fetchAgents

		case "2":
			if p.selectedID == "" && len(p.agents) > 0 {
				idx := p.table.Cursor()
				if idx >= 0 && idx < len(p.agents) {
					p.selectedID = p.agents[idx].ID
				}
			}
			p.subView = agentsViewApprovals
			return p.fetchDetail

		case "3":
			if p.selectedID == "" && len(p.agents) > 0 {
				idx := p.table.Cursor()
				if idx >= 0 && idx < len(p.agents) {
					p.selectedID = p.agents[idx].ID
				}
			}
			p.subView = agentsViewAudit
			return p.fetchDetail

		case "up", "down", "j", "k":
			var cmd tea.Cmd
			p.table, cmd = p.table.Update(msg)
			return cmd
		}
	}

	// Pass other messages to the table.
	var cmd tea.Cmd
	p.table, cmd = p.table.Update(msg)
	return cmd
}

func (p *AgentsPanel) handleDetailKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case KeyEsc, "q":
		p.subView = agentsViewList
		p.detail = nil
		return nil

	case "p":
		// pause the currently selected agent.
		return p.pauseAgent()

	case "u":
		// resume (un-pause) the currently selected agent.
		return p.resumeAgent()

	case "a":
		// Approve the first pending plan (if any) on the selected agent.
		if p.detail == nil {
			return nil
		}
		for _, g := range p.detail.ActiveGoals {
			if g.ActivePlanID != "" {
				planID := g.ActivePlanID
				goalID := g.ID
				return func() tea.Msg {
					_, err := p.rpc.Call("agents.goals.approve", map[string]any{
						"goal_id": goalID,
						"plan_id": planID,
					})
					return agentActionMsg{action: "approve", err: err}
				}
			}
		}
		return nil

	case "x":
		// Reject the first pending plan (best-effort: reason is mandatory).
		if p.detail == nil {
			return nil
		}
		for _, g := range p.detail.ActiveGoals {
			if g.ActivePlanID != "" {
				planID := g.ActivePlanID
				goalID := g.ID
				return func() tea.Msg {
					_, err := p.rpc.Call("agents.goals.reject", map[string]any{
						"goal_id": goalID,
						"plan_id": planID,
						"reason":  "rejected via tui",
					})
					return agentActionMsg{action: "reject", err: err}
				}
			}
		}
		return nil

	case "f":
		// Resolve the first unresolved finding as false_positive.
		if p.detail == nil {
			return nil
		}
		for _, fnd := range p.detail.RecentFindings {
			if fnd.ResolvedAt == nil {
				id := fnd.ID
				return func() tea.Msg {
					_, err := p.rpc.Call("agents.audit.resolve", map[string]any{
						"finding_id": id,
						"resolution": "false_positive",
					})
					return agentActionMsg{action: "resolve", err: err}
				}
			}
		}
		return nil
	}
	return nil
}

// updateAgentsTable rebuilds the table rows from the cached agents slice.
func (p *AgentsPanel) updateAgentsTable() {
	rows := make([]table.Row, len(p.agents))
	for i, a := range p.agents {
		rows[i] = table.Row{
			truncate(a.ID, 18),
			p.statusBadge(a.Status),
			p.tierShort(a.Tier),
			fmt.Sprintf("%.2f", a.DriftScore),
			fmt.Sprintf("$%d.%02d", a.DailyCostCents/100, a.DailyCostCents%100),
			fmt.Sprintf("%d", a.FindingsCount),
			formatTimeAgoTime(a.LastInvocation),
		}
	}
	p.table.SetRows(rows)
	if len(rows) > 0 {
		p.table.GotoTop()
	}
}

func (p *AgentsPanel) statusBadge(status string) string {
	style := lipgloss.NewStyle()
	switch status {
	case "running":
		style = style.Foreground(lipgloss.Color(ColorGreen))
	case "paused":
		style = style.Foreground(lipgloss.Color(ColorAmber))
	case "error":
		style = style.Foreground(lipgloss.Color(ColorRed))
	default:
		style = style.Foreground(lipgloss.Color(ColorGray))
	}
	return style.Render(status)
}

func (p *AgentsPanel) tierShort(tier string) string {
	switch tier {
	case "tier_1_reactive":
		return "t1 reactive"
	case "tier_2_propose":
		return "t2 propose"
	case "tier_3_autonomous":
		return "t3 autonomous"
	default:
		if tier == "" {
			return StatusNA
		}
		return truncate(tier, 16)
	}
}

// View renders the panel.
func (p *AgentsPanel) View() string {
	if p.subView == agentsViewDetail {
		return p.renderDetail()
	}

	if p.loading && len(p.agents) == 0 {
		return p.renderLoading()
	}
	if p.err != nil && len(p.agents) == 0 {
		return p.renderError()
	}

	var b strings.Builder
	b.WriteString(p.renderHeader())
	b.WriteString("\n")

	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151"))
	b.WriteString(tableStyle.Render(p.table.View()))
	b.WriteString("\n")

	b.WriteString(p.renderHelpHint())
	return b.String()
}

func (p *AgentsPanel) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316"))

	tabActive := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#F97316")).
		Bold(true).
		Padding(0, 1)
	tabInactive := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Background(lipgloss.Color("#1F2937")).
		Padding(0, 1)

	header := titleStyle.Render("agents")
	tabs := []struct {
		label string
		view  agentsSubView
	}{
		{"list", agentsViewList},
		{"approvals", agentsViewApprovals},
		{"audit", agentsViewAudit},
	}
	var tabParts []string
	for _, t := range tabs {
		if p.subView == t.view {
			tabParts = append(tabParts, tabActive.Render(t.label))
		} else {
			tabParts = append(tabParts, tabInactive.Render(t.label))
		}
	}
	tabsLine := strings.Join(tabParts, " ")

	count := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).
		Render(fmt.Sprintf("(%d agents)", len(p.agents)))

	return lipgloss.JoinHorizontal(lipgloss.Left, header, "  ", tabsLine, "  ", count)
}

func (p *AgentsPanel) renderHelpHint() string {
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		MarginTop(1)
	return hintStyle.Render("r: refresh | enter: details | 1: list | 2: approvals | 3: audit | esc: back")
}

func (p *AgentsPanel) renderLoading() string {
	style := lipgloss.NewStyle().
		Width(max(p.width-4, 20)).
		Align(lipgloss.Center).
		Padding(4, 0)
	return style.Render("loading agents...")
}

func (p *AgentsPanel) renderError() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorRed)).
		Padding(1, 2).
		Width(max(p.width-4, 20))

	errMsg := "unknown error"
	if p.err != nil {
		errMsg = fmt.Sprintf("%v", p.err)
	}
	return style.Render(
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true).Render("error") +
			"\n\n" + errMsg + "\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Render("press 'r' to refresh"),
	)
}

func (p *AgentsPanel) renderDetail() string {
	if p.detail == nil {
		return p.renderLoading()
	}

	d := p.detail

	modalWidth := min(max(p.width-8, 40), 100)

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
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

	var b strings.Builder

	// Title
	name := d.Agent.Name
	if name == "" {
		name = d.Agent.ID
	}
	b.WriteString(titleStyle.Render("agent: " + truncate(name, 50)))
	b.WriteString("\n\n")

	// Identity
	b.WriteString(labelStyle.Render("id:"))
	b.WriteString(valueStyle.Render(d.Agent.ID))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("role:"))
	b.WriteString(valueStyle.Render(d.Agent.Role))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("tier:"))
	b.WriteString(valueStyle.Render(d.Agent.Tier))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("status:"))
	b.WriteString(p.statusBadge(d.Agent.Status))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("risk cap:"))
	b.WriteString(valueStyle.Render(orNA(d.RiskCeiling)))
	b.WriteString("\n")
	if len(d.EscalatesTo) > 0 {
		b.WriteString(labelStyle.Render("escalates:"))
		b.WriteString(valueStyle.Render(strings.Join(d.EscalatesTo, ", ")))
		b.WriteString("\n")
	}

	// Purpose / Charter
	if d.Purpose != "" {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("--- purpose ---"))
		b.WriteString("\n")
		b.WriteString(valueStyle.Render(wrapText(d.Purpose, modalWidth-6)))
		b.WriteString("\n")
	}
	if d.Charter != "" {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("--- charter ---"))
		b.WriteString("\n")
		b.WriteString(valueStyle.Render(wrapText(d.Charter, modalWidth-6)))
		b.WriteString("\n")
	}

	// Constraints
	if len(d.Never) > 0 || len(d.ToolsForbidden) > 0 {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("--- constraints ---"))
		b.WriteString("\n")
		if len(d.Never) > 0 {
			b.WriteString(labelStyle.Render("never:"))
			b.WriteString(valueStyle.Render(strings.Join(d.Never, ", ")))
			b.WriteString("\n")
		}
		if len(d.ToolsForbidden) > 0 {
			b.WriteString(labelStyle.Render("forbidden:"))
			b.WriteString(valueStyle.Render(strings.Join(d.ToolsForbidden, ", ")))
			b.WriteString("\n")
		}
	}

	// Goals
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render(fmt.Sprintf("--- goals (%d) ---", len(d.ActiveGoals))))
	b.WriteString("\n")
	if len(d.ActiveGoals) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).
			Italic(true).Render("no active goals"))
		b.WriteString("\n")
	}
	for _, g := range d.ActiveGoals {
		b.WriteString(p.renderGoalLine(g))
		b.WriteString("\n")
	}

	// Findings summary
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render(fmt.Sprintf("--- recent findings (%d) ---", len(d.RecentFindings))))
	b.WriteString("\n")
	if len(d.RecentFindings) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).
			Italic(true).Render("none in the last 7 days"))
		b.WriteString("\n")
	}
	for _, f := range d.RecentFindings {
		b.WriteString(p.renderFindingLine(f))
		b.WriteString("\n")
	}

	// Actions hint
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).
		Render("p: pause | u: resume | a: approve | x: reject | f: resolve finding | esc: back"))

	return modalStyle.Render(b.String())
}

func (p *AgentsPanel) renderGoalLine(g AgentGoal) string {
	healthColor := ColorGray
	healthLabel := g.Health
	switch g.Health {
	case "healthy":
		healthColor = ColorGreen
	case "at_risk":
		healthColor = ColorAmber
	case "broken":
		healthColor = ColorRed
	}
	healthStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(healthColor)).Bold(true)
	dot := healthStyle.Render("●")
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")).Render(g.Title)
	plan := ""
	if g.ActivePlanID != "" {
		plan = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).
			Render(" plan: " + truncate(g.ActivePlanID, 12))
	}
	return fmt.Sprintf("%s %s (%s)%s", dot, title, healthLabel, plan)
}

func (p *AgentsPanel) renderFindingLine(f AgentFinding) string {
	sevColor := ColorGray
	switch f.Severity {
	case "critical":
		sevColor = ColorRed
	case "warning":
		sevColor = ColorAmber
	case "info":
		sevColor = "#3B82F6"
	}
	sevStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sevColor)).Bold(true)
	rule := f.ViolatedRule
	if rule == "" {
		rule = f.Checkpoint
	}
	return fmt.Sprintf("%s %s — %s",
		sevStyle.Render(f.Severity),
		truncate(rule, 40),
		f.DetectedAt.Format("01-02 15:04"),
	)
}

// orNA returns s when non-empty, else StatusNA.
func orNA(s string) string {
	if s == "" {
		return StatusNA
	}
	return s
}

// wrapText breaks s into lines no longer than width (runes).
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var b strings.Builder
	line := words[0]
	for _, w := range words[1:] {
		if len([]rune(line))+1+len([]rune(w)) > width {
			b.WriteString(line)
			b.WriteString("\n")
			line = w
		} else {
			line += " " + w
		}
	}
	b.WriteString(line)
	return b.String()
}

// formatTimeAgoTime renders a time.Time as a short HH:MM:SS or "n/a".
func formatTimeAgoTime(t time.Time) string {
	if t.IsZero() {
		return StatusNA
	}
	return t.Format("15:04:05")
}
