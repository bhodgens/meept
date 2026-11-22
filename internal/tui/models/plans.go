package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/tableutil"
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

	// Reject-reason prompt state (leaf 08): pressing "d" on a
	// pending_approval plan opens an inline text input; enter submits,
	// esc cancels.
	rejectInput       textinput.Model
	rejectInputActive bool
	rejectPlanID      string

	// fetchPending gates the debounced refresh: true while a
	// planDebounceMsg tick is in flight (leaf 08 stale-refresh fix).
	fetchPending bool

	// sessionMu guards sessionID: SetSession writes it on the event-loop
	// goroutine while fetchPlans (a tea.Cmd) reads it on the commands
	// goroutine (bughunt fix-wave 2026-10-06, caught by -race in the
	// golden plans view).
	sessionMu sync.Mutex

	// pendingTaskGates overlays task-gate approval snapshots (06-planning/01
	// T3) onto the plans table; guarded by sessionMu (same goroutine split
	// as sessionID). Keyed by task id; NOT persisted - gate state lives in
	// tasks.db and a restart clears the overlay.
	pendingTaskGates map[string]TaskGateApprovalMsg

	// gateVisible caches how many task-gate rows the last
	// updatePlansTable merged, so the header count can include them
	// (the gate overlay is not part of m.plans).
	gateVisible int

	// UI state
	showingDetail bool
	showingHelp   bool
	filteredCount int

	// docProposals holds 0-phase/0-step plans (evolver document
	// proposals) seen by the last table build, so the view can render
	// the shared document-proposal line (06-planning/03; reuses
	// planCountLine - no wording fork). Not persisted.
	docProposals []string
}

// planFetchDebounce is the minimum spacing between two plan list fetches
// (leaf 08 debounced refresh).
const planFetchDebounce = 300 * time.Millisecond

// planDebounceMsg clears the fetchPending gate when the debounce window
// elapses.
type planDebounceMsg struct{}

// plansRefreshRequestedMsg asks the plans model to refresh (debounced):
// sent by the app's event pump on plan lifecycle events (leaf 08
// stale-refresh fix). The model gates duplicate fetches with
// fetchPending so a burst of events costs one RPC.
type plansRefreshRequestedMsg struct{}

// RefreshRequested is the exported seam app.go uses on plan lifecycle
// events: it arms the same debounced fetch as the manual `r` key
// (leaf 08).
func (m *PlansModel) RefreshRequested() tea.Cmd {
	return m.Update(plansRefreshRequestedMsg{})
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
		BorderForeground(paletteColor("border")).
		BorderBottom(true).
		Bold(true).
		Foreground(paletteColor("primary"))

	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FFFFFF")). // palette-exempt: max-contrast literal
		Background(paletteColor("primary")).
		Bold(true)

	t.SetStyles(s)

	// Reject-reason prompt (leaf 08): single-line optional-reason input.
	ti := textinput.New()
	ti.Placeholder = "reason (optional)"
	ti.CharLimit = 300

	return &PlansModel{
		rpc:         rpcClient,
		table:       t,
		filter:      PlanFilterAll,
		rejectInput: ti,
	}
}

// PlansUpdateMsg carries the plans update.
type PlansUpdateMsg struct {
	Plans []types.PlanExtended
	Err   error
}

// TaskGateApprovalMsg (chat.go) snapshots one strategic task gate.

// pendingTaskGates holds the most recent task-gate snapshot per task id
// (06-planning/01, T3). Guarded by sessionMu: the App event pump writes
// it on the event-loop goroutine while fetchPlans/updatePlansTable can
// touch it from the commands goroutine (same discipline as sessionID).
// Deliberately NOT persisted: a restart clears the overlay, and the gate
// state itself lives in tasks.db.

// SetPendingTaskGates replaces the task-gate overlay the plans view
// merges with plan.list_by_session rows (06-planning/01, T3). Task gates
// live in tasks.db, not the plan store, so plan.list_by_session never
// returns them; the App event pump feeds the latest task.pending_approval
// snapshots here. A gate for a session other than the current filter is
// stored but not shown (same scoping the chat card applies).
func (m *PlansModel) SetPendingTaskGates(gates []TaskGateApprovalMsg) {
	m.sessionMu.Lock()
	if m.pendingTaskGates == nil {
		m.pendingTaskGates = map[string]TaskGateApprovalMsg{}
	}
	for _, g := range gates {
		if g.TaskID == "" {
			continue
		}
		m.pendingTaskGates[g.TaskID] = g
	}
	// Unlock before updatePlansTable: it takes sessionMu itself
	// (sync.Mutex is not reentrant).
	m.sessionMu.Unlock()
	m.updatePlansTable()
}

// pendingTaskGatesForFilter returns the overlay gates visible under the
// current session filter. Caller must hold sessionMu (it reads sessionID
// and the gate map without its own lock).
func (m *PlansModel) pendingTaskGatesForFilter() []TaskGateApprovalMsg {
	if len(m.pendingTaskGates) == 0 {
		return nil
	}
	filter := m.sessionID
	var out []TaskGateApprovalMsg
	for _, g := range m.pendingTaskGates {
		// Session scoping: an EMPTY current filter shows every gate, and
		// a gate with an EMPTY session id (legacy publisher / session
		// rebind window) shows in every filter - hiding it would make a
		// live approval gate invisible exactly when the transcript is
		// mid-rebind. An explicit foreign session id is still dropped.
		if filter != "" && g.SessionID != "" && g.SessionID != filter {
			continue
		}
		out = append(out, g)
	}
	return out
}

// PlanActionMsg carries the result of a plan action (approve/reject/etc.).
type PlanActionMsg struct {
	PlanID string
	Action string
	Err    error
}

// planRejectInputMsg marks the reject-reason text input as active. While
// set, keystrokes flow into the input instead of the table (leaf 08:
// the plans tab had no reject affordance at all - GUI parity gap).
type planRejectInputMsg struct {
	active bool
}

// planRejectSubmittedMsg carries the submitted reject reason off the
// Update loop into the reject tea.Cmd (the input lives on the event-loop
// goroutine; the RPC call runs on the commands goroutine).
type planRejectSubmittedMsg struct {
	planID string
	reason string
}

// SetSession sets the session ID to filter plans for.
//
// fetchPlans runs as a tea.Cmd on a SEPARATE goroutine (bubbletea
// handleCommands), while SetSession runs on the event-loop goroutine inside
// App.Update. The plain string field needs its own lock or the two race
// (-race caught this via the golden plans view: SetSession wrote while a
// fetchPlans cmd from the PREVIOUS session read it).
func (m *PlansModel) SetSession(sessionID string) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	m.sessionID = sessionID
}

// currentSessionID returns the session filter under the same lock fetchPlans
// reads it with.
func (m *PlansModel) currentSessionID() string {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	return m.sessionID
}

// SetSize updates the model dimensions.
func (m *PlansModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	tableHeight := max(height-12, 5)

	// Both axes must be set: the table viewport starts at width 0, and a
	// zero-width viewport renders no rows (header only, blank body).
	tableutil.Size(&m.table, width, tableHeight)

	m.setPlansColumns()

	// Repopulate rows from cached data so a resize does not blank the table.
	// Gate rows count too: with zero store plans but a live task-gate
	// overlay, the merged table must repopulate (06-planning/01 T3).
	if len(m.plans) > 0 || m.gateVisible > 0 {
		m.updatePlansTable()
	}
}

func (m *PlansModel) setPlansColumns() {
	// SetColumns re-renders the installed rows, so clear them before the column
	// list changes; SetSize repopulates from the cached plans.
	m.table.SetRows([]table.Row{})

	// Plan view columns: Title | State | Phases | Steps | Progress | Updated
	//
	// Fitted to the viewport via tableutil.FitWidthsMin rather than
	// hand-computed percentages: the fixed columns alone sum to 48 plus a
	// 15-column title floor, which overruns a narrow pane and makes
	// bubbles/table render a box wider than its container with every cell
	// MaxWidth-truncated (M9). progress keeps room for an 8-cell bar plus
	// its " n/m" label; title absorbs the surplus.
	mins := []int{15, 10, 8, 8, 12, 10}
	weights := []int{26, 10, 8, 8, 12, 10}
	widths := tableutil.FitWidthsMin(tableutil.ContentBudget(m.table.Width(), len(mins)), mins, weights)

	m.table.SetColumns([]table.Column{
		{Title: "title", Width: widths[0]},
		{Title: ColState, Width: widths[1]},
		{Title: "phases", Width: widths[2]},
		{Title: "steps", Width: widths[3]},
		{Title: "progress", Width: widths[4]},
		{Title: "updated", Width: widths[5]},
	})
}

// Init initializes the plans model.
func (m *PlansModel) Init() tea.Cmd {
	return m.fetchPlans
}

func (m *PlansModel) fetchPlans() tea.Msg {
	var params map[string]any
	if id := m.currentSessionID(); id != "" {
		params = map[string]any{"session_id": id}
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

	case TaskGateApprovalMsg:
		// 06-planning/01 T3: a live task-gate snapshot reached the plans
		// model directly (e.g. replayed during a session rebind). Keep
		// the overlay fresh so the awaiting-approval row survives even
		// when the chat transcript ate the event first.
		m.SetPendingTaskGates([]TaskGateApprovalMsg{msg})
		return nil

	case PlanActionMsg:
		// After an action completes, refresh plans
		if msg.Err != nil {
			m.err = msg.Err
		}
		m.loading = true
		return m.fetchPlans

	case planRejectInputMsg:
		m.rejectInputActive = msg.active
		if msg.active {
			m.rejectInput.Reset()
			m.rejectInput.Focus()
		}
		return nil

	case planRejectSubmittedMsg:
		m.rejectInputActive = false
		return m.rejectPlan(msg.planID, msg.reason)

	case planDebounceMsg:
		// Debounce window elapsed: the next refresh event may fetch again.
		m.fetchPending = false
		return nil

	case plansRefreshRequestedMsg:
		// Plan lifecycle event from the app pump: refresh the list
		// behind the same debounce gate as the manual `r` key so a
		// burst of plan events coalesces into one fetch (leaf 08).
		if !m.fetchPending {
			m.fetchPending = true
			return tea.Batch(
				m.fetchPlans,
				tea.Tick(planFetchDebounce, func(_ time.Time) tea.Msg {
					return planDebounceMsg{}
				}),
			)
		}
		return nil

	case tea.KeyPressMsg:
		// Reject-reason input captures every keystroke while active.
		if m.rejectInputActive {
			switch msg.String() {
			case KeyEnter:
				reason := m.rejectInput.Value()
				planID := m.rejectPlanID
				m.rejectPlanID = ""
				return func() tea.Msg {
					return planRejectSubmittedMsg{planID: planID, reason: reason}
				}
			case KeyEsc:
				m.rejectInputActive = false
				m.rejectPlanID = ""
				return nil
			}
			var cmd tea.Cmd
			m.rejectInput, cmd = m.rejectInput.Update(msg)
			return cmd
		}

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
			// Refresh - debounced (leaf 08): a burst of plan lifecycle
			// events previously fired one fetch per event. Only arm a
			// fetch when none is already pending; planDebounceMsg lands
			// ~300ms later and clears the pending flag.
			if !m.fetchPending {
				m.fetchPending = true
				return tea.Batch(
					m.fetchPlans,
					tea.Tick(planFetchDebounce, func(_ time.Time) tea.Msg {
						return planDebounceMsg{}
					}),
				)
			}
			return nil

		case "d":
			// Reject selected plan with a reason prompt (GUI parity:
			// plans_tab.dart shows a reject dialog with an optional
			// reason field; the TUI had no reject affordance at all).
			if m.selected != nil && m.selected.State == "pending_approval" {
				m.rejectPlanID = m.selected.ID
				return func() tea.Msg {
					return planRejectInputMsg{active: true}
				}
			}
			return nil

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

// rejectPlan rejects a pending_approval plan, optionally with a reason
// (leaf 08: GUI parity - plans_tab.dart sends plan.reject with the dialog
// text; the TUI previously had no reject affordance).
func (m *PlansModel) rejectPlan(planID, reason string) tea.Cmd {
	return func() tea.Msg {
		params := map[string]any{"plan_id": planID}
		if reason != "" {
			params["reason"] = reason
		}
		_, err := m.rpc.Call("plan.reject", params)
		return PlanActionMsg{PlanID: planID, Action: "reject", Err: err}
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
	m.sessionMu.Lock()
	gates := m.pendingTaskGatesForFilter()
	gateVisible := len(gates)
	m.gateVisible = gateVisible
	m.sessionMu.Unlock()

	plans := m.filterPlans()
	m.filteredCount = len(plans) + gateVisible
	rows := make([]table.Row, 0, len(plans)+gateVisible)

	// Task-gate rows first (06-planning/01 T3): a task parked at the
	// strategic approval gate never appears in plan.list_by_session,
	// so it is merged here as a synthetic row with an awaiting-approval
	// state and an approve/reject affordance hint (a/v keys route to
	// task.approve / task.reject for gate rows).
	for _, g := range gates {
		title := "task gate: " + g.TaskID
		rows = append(rows, table.Row{
			types.TruncateString(title, 22),
			m.getPlanStateIcon("pending_approval"),
			"-",
			fmt.Sprintf("0/%d", g.Total),
			m.renderProgressBar(0, g.Total, 8),
			// "awaiting approval" states the gate plainly; the last
			// column is width-truncated on narrow panes, so the
			// affordance also rides in the title and the plans-view
			// help hint (a: approve / v: revise route to task.approve /
			// task.reject for gate rows).
			"awaiting approval",
		})
	}
	// Preserve the row population across later resize/column refits:
	// SetSize -> setPlansColumns clears rows and repopulates ONLY when
	// cached data exists - gateVisible counts as cached data (kept in
	// sync here).
	if gateVisible > 0 {
		m.gateVisible = gateVisible
	}

	m.docProposals = nil

	for _, plan := range plans {
		stateIcon := m.getPlanStateIcon(plan.State)

		// Document proposal (06-planning/03): a 0-phase/0-step plan is
		// an evolver markdown proposal, not a broken/empty plan. Keep it
		// for the tab's document-proposal status line; the table cells
		// stay compact.
		if len(plan.Phases) == 0 && plan.TotalSteps == 0 {
			title := plan.Title
			if title == "" {
				title = plan.ID
			}
			m.docProposals = append(m.docProposals, title)
		}

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

		rows = append(rows, table.Row{
			types.TruncateString(title, 22),
			stateIcon,
			phasesStr,
			stepsStr,
			progress,
			updated,
		})
	}
	tableutil.SetRows(&m.table, rows)
	if len(rows) > 0 {
		m.table.GotoTop()
	}
}

// View renders the plans view.
func (m *PlansModel) View() string {
	// Reject-reason prompt (leaf 08): takes over the view while active.
	if m.rejectInputActive {
		return m.renderRejectPrompt()
	}

	// Detail modal overlay
	if m.showingDetail && m.selected != nil {
		return m.renderPlanDetailModal()
	}

	if m.showingHelp {
		return m.renderHelp()
	}

	// Task-gate rows are overlay state, not m.plans entries: include
	// them in the empty-view guards so a gate-only table still renders
	// (06-planning/01 T3).
	m.sessionMu.Lock()
	gatesCount := len(m.pendingTaskGatesForFilter())
	m.sessionMu.Unlock()

	if m.loading && len(m.plans) == 0 && gatesCount == 0 {
		return m.renderLoading()
	}

	if m.err != nil && len(m.plans) == 0 && gatesCount == 0 {
		return m.renderError()
	}

	var b strings.Builder

	// Header with filter tabs
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Table
	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("border"))

	b.WriteString(tableStyle.Render(m.table.View()))
	b.WriteString("\n")

	// Document-proposal status line (06-planning/03): 0-phase/0-step
	// evolver plans render the shared planCountLine wording instead of
	// bare zero counts (which the fixed-width table cells cannot carry).
	docStyle := lipgloss.NewStyle().Foreground(paletteColor("textMuted"))
	for _, title := range m.docProposals {
		b.WriteString(docStyle.Render(fmt.Sprintf("%s - %s", types.TruncateString(title, 40), DocumentProposalLine())))
		b.WriteString("\n")
	}

	// Detail preview panel
	if m.selected != nil {
		b.WriteString(m.renderPlanPreview())
	} else {
		b.WriteString(m.renderEmptyDetail())
	}

	// Help hint
	hintStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		MarginTop(1)

	// Task-gate banner (06-planning/01 T3): when task-gate rows are in
	// the table, name the affordance explicitly - the full text survives
	// any column truncation, and the keys map to the C2 RPCs (a: approve
	// -> task.approve; x: reject -> task.reject; reject never executes).
	if gatesCount > 0 {
		bannerStyle := lipgloss.NewStyle().
			Foreground(paletteColor("warning")).
			MarginTop(1)
		b.WriteString(bannerStyle.Render("task approval gate: a task is awaiting approval - a: approve | x: reject (never executes)"))
		b.WriteString("\n")
	}

	b.WriteString(hintStyle.Render("r: refresh | /: filter | a: approve | v: revise | c: confirm | d: reject | enter: details | ?: help"))

	return b.String()
}

// renderRejectPrompt renders the inline reject-reason prompt (leaf 08).
func (m *PlansModel) renderRejectPrompt() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("error"))

	promptStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textPrimary")).
		MarginTop(1)

	hintStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		MarginTop(1)

	var b strings.Builder
	b.WriteString(titleStyle.Render("reject plan"))
	b.WriteString("\n\n")
	if m.selected != nil {
		b.WriteString(promptStyle.Render("plan: " + m.selected.Title))
		b.WriteString("\n")
	}
	b.WriteString(m.rejectInput.View())
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("enter: reject  ·  esc: cancel"))
	return b.String()
}

func (m *PlansModel) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("primary"))

	tabInactiveStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Background(paletteColor("surfaceAlt")).
		Padding(0, 1)

	tabActiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")). // palette-exempt: max-contrast literal
		Background(paletteColor("primary")).
		Bold(true).
		Padding(0, 1)

	filterStyle := lipgloss.NewStyle().
		Foreground(paletteColor("warning")).
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

	// Plan count: computed LIVE from current state (06-planning/01 T3).
	// The plans table merges two sources - plan.list_by_session rows and
	// the task-gate overlay - and either can change between
	// updatePlansTable passes (a gate arriving after a store refresh,
	// a session rebind hiding a foreign gate). Caching the header count
	// let a stale zero reach the render; deriving it here is cheap (map
	// + slice len) and always truthful. Filtered = visible after the
	// active filter tab (gates obey the same session scoping).
	countStyle := lipgloss.NewStyle().Foreground(paletteColor("textMuted"))
	m.sessionMu.Lock()
	visibleGates := len(m.pendingTaskGatesForFilter())
	totalRows := len(m.plans) + len(m.pendingTaskGates)
	m.sessionMu.Unlock()
	filteredPlans := len(m.filterPlans())
	count := countStyle.Render(fmt.Sprintf("(%d/%d)", filteredPlans+visibleGates, totalRows))

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
		BorderForeground(paletteColor("primary")).
		Padding(0, 1).
		Width(m.width - 4)

	labelStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Width(12)

	valueStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textPrimary"))

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
		revStyle := lipgloss.NewStyle().Foreground(paletteColor("warning"))
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
		BorderForeground(paletteColor("primary")).
		Padding(1, 2).
		Width(modalWidth)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("primary")).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Foreground(paletteColor("warning")).
		Bold(true).
		MarginTop(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Width(14)

	valueStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textPrimary"))

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

	// Document proposal (06-planning/03): a 0-phase/0-step plan is an
	// evolver markdown proposal. Name it (shared planCountLine wording,
	// no fork) instead of a bare zero-phase card; the file line above is
	// the open-file affordance.
	if len(plan.Phases) == 0 && plan.TotalSteps == 0 {
		content.WriteString(labelStyle.Render("scope:"))
		content.WriteString(valueStyle.Render(DocumentProposalLine()))
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
		content.WriteString(lipgloss.NewStyle().Foreground(paletteColor("warning")).Render(fmt.Sprintf("%d", plan.RevisionCount)))
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

	completedStyle := lipgloss.NewStyle().Foreground(paletteColor("success"))
	failedStyle := lipgloss.NewStyle().Foreground(paletteColor("error"))
	pendingStyle := lipgloss.NewStyle().Foreground(paletteColor("textMuted"))

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
		Foreground(paletteColor("textMuted")).
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
	if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
		return t.Format("15:04:05")
	}
	return types.TruncateString(timestamp, 10)
}

func (m *PlansModel) getPlanStateIcon(state string) string {
	switch state {
	case "planning":
		return lipgloss.NewStyle().Foreground(paletteColor("info")).Render("●") + " plan"
	case "draft":
		return lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Render("●") + " draft"
	case "pending_approval":
		return lipgloss.NewStyle().Foreground(paletteColor("info")).Render("●") + " review"
	case "approved":
		return lipgloss.NewStyle().Foreground(paletteColor("success")).Render("✓") + " ok"
	case "executing":
		return lipgloss.NewStyle().Foreground(paletteColor("warning")).Render("●") + " exec"
	case "completed":
		return lipgloss.NewStyle().Foreground(paletteColor("success")).Render("●") + " done"
	case "confirmed":
		return lipgloss.NewStyle().Foreground(paletteColor("success")).Bold(true).Render("★") + " ok"
	case "failed":
		return lipgloss.NewStyle().Foreground(paletteColor("error")).Render("✗") + " fail"
	case "cancelled":
		return lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Render("○") + " cancel"
	default:
		return "? " + types.TruncateString(state, 4)
	}
}

func (m *PlansModel) getPlanStateColor(state string) string {
	switch state {
	case "planning":
		return "#3B82F6" // Blue
	case "draft":
		return paletteHex("textMuted")
	case "pending_approval":
		return "#3B82F6" // Blue
	case "approved":
		return paletteHex("success")
	case "executing":
		return paletteHex("warning")
	case "completed":
		return paletteHex("success")
	case "confirmed":
		return paletteHex("success")
	case "failed":
		return paletteHex("error")
	case "cancelled":
		return paletteHex("textMuted")
	default:
		return paletteHex("textMuted")
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
		BorderForeground(paletteColor("error")).
		Padding(1, 2).
		Width(m.width - 4)

	errMsg := "unknown error"
	if m.err != nil {
		errMsg = fmt.Sprintf("%v", m.err)
	}

	return style.Render(
		lipgloss.NewStyle().Foreground(paletteColor("error")).Bold(true).Render("error") +
			"\n\n" +
			errMsg +
			"\n\n" +
			lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Render("press 'r' to refresh"),
	)
}

func (m *PlansModel) renderEmptyDetail() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("border")).
		Padding(1, 2).
		Width(m.width - 4)

	content := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Italic(true).
		Render("select a plan to view details")

	return panelStyle.Render(content)
}

func (m *PlansModel) renderHelp() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("primary")).
		Padding(2, 4).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("primary")).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Foreground(paletteColor("warning")).
		Bold(true).
		MarginTop(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(paletteColor("warning")).
		Bold(true).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textPrimary"))

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
	content += keyStyle.Render("d") + descStyle.Render("reject pending plan (with reason prompt)") + "\n"

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
	content += lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Render("press any key to close")

	return panelStyle.Render(content)
}

// ReapplyGatesForSession re-shows gates whose snapshot belongs to the
// given session when the current session filter is a DIFFERENT session
// (06-planning/01): a live approval gate must stay visible while the
// transcript is mid-rebind. It retargets the gate's session to the
// requested one and rebuilds the table. Test-finish aid + rebind
// recovery; production event flow refreshes via SetPendingTaskGates.
func (m *PlansModel) ReapplyGatesForSession(sessionID string) {
	m.sessionMu.Lock()
	m.sessionID = sessionID
	// Retarget overlay gates to the requested session so the filter
	// matches (an empty gate session is already show-all).
	for id, g := range m.pendingTaskGates {
		if g.SessionID != sessionID {
			g.SessionID = sessionID
			m.pendingTaskGates[id] = g
		}
	}
	m.sessionMu.Unlock()
	m.updatePlansTable()
}

// GateCountForTest reports how many task-gate overlay entries exist.
// Test-only accessor (06-planning/01 probe); production code reads the
// merged table, not the map.
func (m *PlansModel) GateCountForTest() int {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	return len(m.pendingTaskGates)
}
