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
	agentsViewList      agentsSubView = iota // default: agents list
	agentsViewDetail                         // drill-in: constitution / goals / audit / state
	agentsViewApprovals                      // tier-2 plans awaiting user signoff
	agentsViewAudit                          // severity-colored audit findings
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

	// countdownTick gates the 30s countdown refresh (quotaCountdownTickMsg):
	// while any agent is quota-hit the panel re-renders its cached episode
	// data so the "quota resets in Nh Mm" badge stays live. Leaf 08 Task 2
	// contract: countdown updates on the TUI's tick.
	countdownTick bool

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
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Role           string    `json:"role"`
	Status         string    `json:"status"` // running | paused | error | stopped | quota_wait
	Tier           string    `json:"tier"`   // tier_1_reactive | tier_2_propose | tier_3_autonomous
	DriftScore     float64   `json:"drift_score"`
	DailyCostCents int       `json:"daily_cost_cents"`
	FindingsCount  int       `json:"findings_count"`
	LastInvocation time.Time `json:"last_invocation"`
	// Quota episode state (optional — when absent, rendering is unchanged).
	// QuotaWaitUntil comes from the agent.quota_wait event's unblock_at;
	// QuotaModel is the primary model that hit the quota (event model_id);
	// QuotaBlocked means the 24h max-wait escalation fired (hard stop);
	// QuotaFallbackModel is the fallback model carrying work while the
	// primary provider waits out its quota reset.
	// QuotaWaitClass is the parked-turn class from the leaf 04 event
	// payload ("quota"|"throttle"; "" legacy) — it selects the wait label
	// via QuotaWaitLabel.
	QuotaWaitUntil     *time.Time `json:"quota_wait_until,omitempty"`
	QuotaModel         string     `json:"quota_model,omitempty"`
	QuotaBlocked       bool       `json:"quota_blocked,omitempty"`
	QuotaFallbackModel string     `json:"quota_fallback_model,omitempty"`
	QuotaWaitClass     string     `json:"quota_wait_class,omitempty"`
}

// AgentDetail is the drill-in payload. Combines the employee definition
// with its constitution summary, active goals, and recent findings.
type AgentDetail struct {
	Agent          AgentSummary   `json:"agent"`
	Purpose        string         `json:"purpose"`
	Charter        string         `json:"charter"`
	Never          []string       `json:"never"`
	ToolsAllowed   []string       `json:"tools_allowed"`
	ToolsForbidden []string       `json:"tools_forbidden"`
	RiskCeiling    string         `json:"risk_ceiling"`
	EscalatesTo    []string       `json:"escalates_to"`
	ActiveGoals    []AgentGoal    `json:"active_goals"`
	RecentFindings []AgentFinding `json:"recent_findings"`
}

// AgentGoal mirrors employee.Goal for wire transport.
type AgentGoal struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Mandate      string     `json:"mandate"`
	State        string     `json:"state"`  // active | paused | retired
	Health       string     `json:"health"` // healthy | at_risk | broken | unknown
	ActivePlanID string     `json:"active_plan_id,omitempty"`
	LastAssessed time.Time  `json:"last_assessed"`
	Gate         *AgentGate `json:"gate,omitempty"`
}

// AgentGate is the completion check shown on a goal row.
type AgentGate struct {
	Command           string `json:"command"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	SkipWhenUnchanged bool   `json:"skip_when_unchanged"`
}

// AgentFinding mirrors employee.AuditFinding for wire transport.
type AgentFinding struct {
	ID           string     `json:"id"`
	EmployeeID   string     `json:"employee_id"`
	Severity     string     `json:"severity"` // info | warning | critical
	Checkpoint   string     `json:"checkpoint"`
	ViolatedRule string     `json:"violated_rule,omitempty"`
	Evidence     string     `json:"evidence,omitempty"`
	DetectedAt   time.Time  `json:"detected_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	Resolution   string     `json:"resolution,omitempty"`
	DriftScore   float64    `json:"drift_score,omitempty"`
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
		BorderForeground(Current().Border).
		BorderBottom(true).
		Bold(true).
		Foreground(Current().Primary)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FFFFFF")). // palette-exempt: max-contrast literal
		Background(Current().Primary).
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

// quotaStateMsg carries a quota state update for a single agent, sourced from
// an agent.quota_wait bus event (WS type agent_progress). An empty to value
// means "clear any quota episode for this agent" (quota_cleared / back to
// running). waitClass is the leaf 04 park event's class payload
// ("quota"|"throttle"; "" for legacy events and tier refreshes) — it rides
// through to the row so the badge renders the right wait label.
type quotaStateMsg struct {
	agentID       string
	to            string
	waitUntil     *time.Time
	blocked       bool
	model         string
	fallbackModel string
	escalation    string // "" | warn | action_recommended | blocked (leaf 05 tier vocabulary)
	waitClass     string // "quota" | "throttle" | "" (leaf 04 park-event class)
}

// quotaCountdownTickMsg re-renders quota badges so the live countdown stays
// current. Emitted on a 30s cadence only while an episode is active; the
// tick stops itself when the last episode clears.
type quotaCountdownTickMsg struct{}

// hasQuotaEpisodes reports whether any cached agent carries quota episode
// state (drives the countdown tick lifecycle).
func (p *AgentsPanel) hasQuotaEpisodes() bool {
	for i := range p.agents {
		if p.agents[i].QuotaWaitUntil != nil || p.agents[i].QuotaBlocked {
			return true
		}
	}
	return false
}

// scheduleQuotaCountdownTick starts the self-perpetuating 30s countdown
// refresh. Idempotent: the tick is armed once per episode window.
func (p *AgentsPanel) scheduleQuotaCountdownTick() tea.Cmd {
	if p.countdownTick {
		return nil
	}
	p.countdownTick = true
	return tea.Tick(30*time.Second, func(_ time.Time) tea.Msg {
		return quotaCountdownTickMsg{}
	})
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
		ID           string         `json:"id"`
		Name         string         `json:"name"`
		Description  string         `json:"description"`
		Enabled      bool           `json:"enabled"`
		Model        string         `json:"model,omitempty"`
		Tools        []string       `json:"tools"`
		Constitution map[string]any `json:"constitution"`
		Constraints  map[string]any `json:"constraints,omitempty"`
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
	case quotaCountdownTickMsg:
		// The previously armed tick has fired — its window is over.
		p.countdownTick = false
		// Recompute the cached status cells so countdown badges reflect
		// the current time (cells are plain strings; without this pass
		// they'd be frozen at event time).
		if p.hasQuotaEpisodes() {
			p.updateAgentsTable()
			return p.scheduleQuotaCountdownTick()
		}
		return nil

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

	case quotaStateMsg:
		// Apply the quota state update to the matching agent in the cache.
		// A quota_cleared transition (to == "" or "running") resets the
		// agent to its base status so stale episode data clears on running.
		var tickCmd tea.Cmd
		for i := range p.agents {
			if p.agents[i].ID == msg.agentID {
				switch msg.to {
				case AgentStateQuotaWait:
					p.agents[i].QuotaWaitUntil = msg.waitUntil
					p.agents[i].QuotaModel = msg.model
					p.agents[i].QuotaBlocked = false
					p.agents[i].QuotaFallbackModel = msg.fallbackModel
					p.agents[i].QuotaWaitClass = msg.waitClass
					p.agents[i].Status = AgentStateQuotaWait
				case AgentStateBlocked:
					// Blocked wins over wait time; keep the model info so
					// the detail view can still show what was carrying work.
					p.agents[i].QuotaBlocked = true
					p.agents[i].Status = AgentStateBlocked
					if msg.model != "" {
						p.agents[i].QuotaModel = msg.model
					}
					if msg.fallbackModel != "" {
						p.agents[i].QuotaFallbackModel = msg.fallbackModel
					}
					if msg.waitUntil != nil {
						p.agents[i].QuotaWaitUntil = msg.waitUntil
					}
				case "":
					// Tier escalation refresh (12h warn / 20h
					// action_recommended fire with to == "" while the
					// episode is live): update the unblock time when the
					// event carries one; never wipe the episode. Only a
					// genuine clear (escalation "" AND no unblock time)
					// falls through to the reset below.
					if msg.escalation != "" || msg.waitUntil != nil {
						if msg.waitUntil != nil {
							p.agents[i].QuotaWaitUntil = msg.waitUntil
						}
						if msg.model != "" {
							p.agents[i].QuotaModel = msg.model
						}
						p.updateAgentsTable()
						break
					}
					// quota_cleared: drop episode state entirely.
					p.agents[i].QuotaWaitUntil = nil
					p.agents[i].QuotaModel = ""
					p.agents[i].QuotaBlocked = false
					p.agents[i].QuotaFallbackModel = ""
					p.agents[i].QuotaWaitClass = ""
					p.agents[i].Status = "running"
				default:
					// quota_cleared / running: drop episode state entirely.
					p.agents[i].QuotaWaitUntil = nil
					p.agents[i].QuotaModel = ""
					p.agents[i].QuotaBlocked = false
					p.agents[i].QuotaFallbackModel = ""
					p.agents[i].QuotaWaitClass = ""
					p.agents[i].Status = "running"
				}
				p.updateAgentsTable()
				// Episode state changed: arm the countdown tick when an
				// episode became active, and let the tick stop itself when
				// the last one clears.
				if p.hasQuotaEpisodes() {
					tickCmd = p.scheduleQuotaCountdownTick()
				} else {
					p.countdownTick = false
				}
				break
			}
		}
		return tickCmd

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
		statusCell := p.statusBadge(a.Status)
		// If quota state is present, override the status cell.
		if a.QuotaWaitUntil != nil || a.QuotaBlocked {
			statusCell = p.quotaStatusBadge(a.QuotaWaitUntil, a.QuotaBlocked, a.QuotaWaitClass)
		}
		rows[i] = table.Row{
			truncate(a.ID, 18),
			statusCell,
			p.tierShort(a.Tier),
			fmt.Sprintf("%.2f", a.DriftScore),
			fmt.Sprintf("$%d.%02d", a.DailyCostCents/100, a.DailyCostCents%100),
			fmt.Sprintf("%d", a.FindingsCount),
			formatTimeAgoTime(a.LastInvocation),
		}
	}
	p.table.SetRows(rows)
	// Preserve the cursor across rebuilds. quota events and countdown ticks
	// rebuild rows frequently; an unconditional GotoTop would yank the
	// user's selection back to the first row each time.
	if n := len(rows); n > 0 {
		if cur := p.table.Cursor(); cur >= 0 && cur < n {
			p.table.SetCursor(cur)
		} else {
			p.table.GotoTop()
		}
	}
}

func (p *AgentsPanel) statusBadge(status string) string {
	// If the agent has quota state, delegate to quotaStatusBadge.
	style := lipgloss.NewStyle()
	switch status {
	case "running":
		style = style.Foreground(Current().Success)
	case "paused":
		style = style.Foreground(Current().Warning)
	case "error":
		style = style.Foreground(Current().ErrorC)
	case AgentStateQuotaWait:
		return style.Foreground(Current().Warning).Render(RenderAgentStatus(AgentStateQuotaWait))
	case AgentStateBlocked:
		return style.Foreground(Current().ErrorC).Render(RenderAgentStatus(AgentStateBlocked))
	default:
		style = style.Foreground(Current().TextMuted)
	}
	return style.Render(status)
}

// orTime dereferences p, returning the zero time when nil. Used so
// RenderQuotaDetailLines can accept optional wait times directly.
func orTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

// quotaStatusBadge returns the label for quota-related agent states (pure
// text, no styling — updateAgentsTable colors it). When waitUntil is nil and
// blocked is false the agent has no quota episode and the empty string is
// returned so rendering is unchanged (regression safety). When both wait
// time and blocked are present, the blocked label wins. waitClass is the
// park event's class wire value ("quota"|"throttle"|"" legacy): it selects
// the leaf 04 wait label ("quota_wait · reset HH:MM" vs "quota_wait ·
// throttle retry HH:MM", QuotaWaitLabel).
func (p *AgentsPanel) quotaStatusBadge(waitUntil *time.Time, blocked bool, waitClass string) string {
	if blocked {
		return RenderAgentStatus(AgentStateBlocked)
	}
	if waitUntil == nil {
		return ""
	}
	return QuotaWaitLabel(waitClass, *waitUntil)
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
	if p.subView == agentsViewApprovals {
		return p.renderApprovals()
	}
	if p.subView == agentsViewAudit {
		return p.renderAudit()
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
		BorderForeground(Current().Border)
	b.WriteString(tableStyle.Render(p.table.View()))
	b.WriteString("\n")

	b.WriteString(p.renderHelpHint())
	return b.String()
}

func (p *AgentsPanel) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(Current().Primary)

	tabActive := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")). // palette-exempt: max-contrast literal
		Background(Current().Primary).
		Bold(true).
		Padding(0, 1)
	tabInactive := lipgloss.NewStyle().
		Foreground(Current().TextMuted).
		Background(Current().SurfaceAlt).
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

	count := lipgloss.NewStyle().Foreground(Current().TextMuted).
		Render(fmt.Sprintf("(%d agents)", len(p.agents)))

	return lipgloss.JoinHorizontal(lipgloss.Left, header, "  ", tabsLine, "  ", count)
}

func (p *AgentsPanel) renderHelpHint() string {
	hintStyle := lipgloss.NewStyle().
		Foreground(Current().TextMuted).
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
		BorderForeground(Current().ErrorC).
		Padding(1, 2).
		Width(max(p.width-4, 20))

	errMsg := "unknown error"
	if p.err != nil {
		errMsg = fmt.Sprintf("%v", p.err)
	}
	return style.Render(
		lipgloss.NewStyle().Foreground(Current().ErrorC).Bold(true).Render("error") +
			"\n\n" + errMsg + "\n\n" +
			lipgloss.NewStyle().Foreground(Current().TextMuted).Render("press 'r' to refresh"),
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
		BorderForeground(Current().Primary).
		Padding(1, 2).
		Width(modalWidth)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(Current().Primary).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Foreground(Current().Warning).
		Bold(true).
		MarginTop(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(Current().TextMuted).
		Width(14)

	valueStyle := lipgloss.NewStyle().
		Foreground(Current().TextPrimary)

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
	if d.Agent.QuotaBlocked || d.Agent.QuotaWaitUntil != nil {
		// Quota episode: show the colored quota status instead of the base
		// status, plus the primary/active model lines when a fallback is
		// carrying the work.
		b.WriteString(p.quotaStatusBadge(d.Agent.QuotaWaitUntil, d.Agent.QuotaBlocked, d.Agent.QuotaWaitClass))
		b.WriteString("\n")
		for _, line := range RenderQuotaDetailLines(
			d.Agent.QuotaModel, d.Agent.QuotaFallbackModel,
			orTime(d.Agent.QuotaWaitUntil),
		) {
			b.WriteString(labelStyle.Render("quota:"))
			b.WriteString(valueStyle.Render(line))
			b.WriteString("\n")
		}
	} else {
		b.WriteString(p.statusBadge(d.Agent.Status))
		b.WriteString("\n")
	}
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
		b.WriteString(lipgloss.NewStyle().Foreground(Current().TextMuted).
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
		b.WriteString(lipgloss.NewStyle().Foreground(Current().TextMuted).
			Italic(true).Render("none in the last 7 days"))
		b.WriteString("\n")
	}
	for _, f := range d.RecentFindings {
		b.WriteString(p.renderFindingLine(f))
		b.WriteString("\n")
	}

	// Actions hint
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(Current().TextMuted).
		Render("p: pause | u: resume | a: approve | x: reject | f: resolve finding | esc: back"))

	return modalStyle.Render(b.String())
}

// renderApprovals renders the tier-2 approval queue view. If the selected
// agent has pending plans (goals with active_plan_id), they are listed in a
// table with approve/reject hints. When no plans are pending, a centered
// empty-state message is shown.
func (p *AgentsPanel) renderApprovals() string {
	var b strings.Builder
	b.WriteString(p.renderHeader())
	b.WriteString("\n\n")

	sectionStyle := lipgloss.NewStyle().
		Foreground(Current().Warning).
		Bold(true)

	b.WriteString(sectionStyle.Render("--- pending plan approvals ---"))
	b.WriteString("\n\n")

	// Collect pending plans from the loaded detail (if any).
	var pending []AgentGoal
	if p.detail != nil {
		for _, g := range p.detail.ActiveGoals {
			if g.ActivePlanID != "" {
				pending = append(pending, g)
			}
		}
	}

	if len(pending) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(Current().TextMuted).
			Italic(true).
			Width(max(p.width-4, 20)).
			Align(lipgloss.Center).
			Padding(2, 0)
		b.WriteString(empty.Render("no pending approvals"))
		b.WriteString("\n")
	} else {
		// Render a simple table.
		labelStyle := lipgloss.NewStyle().
			Foreground(Current().TextMuted).
			Width(16)
		valueStyle := lipgloss.NewStyle().
			Foreground(Current().TextPrimary)

		for _, g := range pending {
			b.WriteString(labelStyle.Render("employee:"))
			b.WriteString(valueStyle.Render(p.selectedID))
			b.WriteString("\n")
			b.WriteString(labelStyle.Render("goal:"))
			b.WriteString(valueStyle.Render(g.Title))
			b.WriteString("\n")
			b.WriteString(labelStyle.Render("plan id:"))
			b.WriteString(valueStyle.Render(truncate(g.ActivePlanID, 32)))
			b.WriteString("\n")
			b.WriteString(labelStyle.Render("health:"))
			b.WriteString(valueStyle.Render(g.Health))
			b.WriteString("\n\n")
		}

		hint := lipgloss.NewStyle().Foreground(Current().TextMuted)
		b.WriteString(hint.Render("a: approve | x: reject | esc: back to list"))
		b.WriteString("\n")
	}

	return b.String()
}

// renderAudit renders the audit findings sub-view. Shows findings from the
// selected agent (or all agents if none selected) with severity coloring.
func (p *AgentsPanel) renderAudit() string {
	var b strings.Builder
	b.WriteString(p.renderHeader())
	b.WriteString("\n\n")

	sectionStyle := lipgloss.NewStyle().
		Foreground(Current().Warning).
		Bold(true)

	b.WriteString(sectionStyle.Render("--- recent audit findings ---"))
	b.WriteString("\n\n")

	// Collect findings from the loaded detail.
	var findings []AgentFinding
	if p.detail != nil {
		for _, f := range p.detail.RecentFindings {
			if f.ResolvedAt == nil {
				findings = append(findings, f)
			}
		}
	}

	if len(findings) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(Current().TextMuted).
			Italic(true).
			Width(max(p.width-4, 20)).
			Align(lipgloss.Center).
			Padding(2, 0)
		b.WriteString(empty.Render("no recent findings"))
		b.WriteString("\n")
	} else {
		sevStyle := lipgloss.NewStyle().Bold(true)
		ruleStyle := lipgloss.NewStyle().Foreground(Current().TextPrimary)

		for _, f := range findings {
			color := Current().TextMuted
			switch f.Severity {
			case "critical":
				color = Current().ErrorC
			case "warning":
				color = Current().Warning
			case "info":
				color = Current().Info
			}
			dot := sevStyle.Foreground(color).Render("●")

			rule := f.ViolatedRule
			if rule == "" {
				rule = f.Checkpoint
			}
			b.WriteString(fmt.Sprintf("%s %s  %s  %s\n",
				dot,
				ruleStyle.Render(truncate(rule, 40)),
				lipgloss.NewStyle().Foreground(Current().TextMuted).
					Render(f.Severity),
				formatTimeAgoTime(f.DetectedAt),
			))
		}

		b.WriteString("\n")
		hint := lipgloss.NewStyle().Foreground(Current().TextMuted)
		b.WriteString(hint.Render("f: resolve as false positive | esc: back to list"))
		b.WriteString("\n")
	}

	return b.String()
}

func (p *AgentsPanel) renderGoalLine(g AgentGoal) string {
	healthColor := Current().TextMuted
	healthLabel := g.Health
	switch g.Health {
	case "healthy":
		healthColor = Current().Success
	case "at_risk":
		healthColor = Current().Warning
	case "broken":
		healthColor = Current().ErrorC
	}
	healthStyle := lipgloss.NewStyle().Foreground(healthColor).Bold(true)
	dot := healthStyle.Render("●")
	title := lipgloss.NewStyle().Foreground(Current().TextPrimary).Render(g.Title)
	plan := ""
	if g.ActivePlanID != "" {
		plan = lipgloss.NewStyle().Foreground(Current().TextMuted).
			Render(" plan: " + truncate(g.ActivePlanID, 12))
	}
	gate := ""
	if g.Gate != nil && g.Gate.Command != "" {
		gate = lipgloss.NewStyle().Foreground(Current().TextMuted).
			Render(" gate: " + truncate(g.Gate.Command, 24))
	}
	return fmt.Sprintf("%s %s (%s)%s%s", dot, title, healthLabel, plan, gate)
}

func (p *AgentsPanel) renderFindingLine(f AgentFinding) string {
	sevColor := Current().TextMuted
	switch f.Severity {
	case "critical":
		sevColor = Current().ErrorC
	case "warning":
		sevColor = Current().Warning
	case "info":
		sevColor = Current().Info
	}
	sevStyle := lipgloss.NewStyle().Foreground(sevColor).Bold(true)
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
