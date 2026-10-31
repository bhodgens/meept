package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/tableutil"
)

// ActivityModel is the model for the activity view (plan tree
// 20261030-agent-activity-audit-views, leaf 03): a sortable activity table
// (chrono / by task / by plan), a polling live tail over activity.recent,
// and a findings + drift panel over activity.findings + agents.review.
//
// The RPC layer is request/response, so the "live" tail is POLLING on a
// ticker (activityTickMsg), never server push. The ticker is armed only
// while the tail is on AND the view is the app's current view: hiding the
// view delivers hideActivityViewMsg, which stops the tail (no background
// churn behind other views).
//
// All UI text is lowercase (repo convention). Every table mutation goes
// through tableutil.Size / tableutil.SetRows (a width-0 viewport renders
// nothing; an over-long row panics inside bubbles/table).
type ActivityModel struct {
	rpc   ActivityRPCClient
	table table.Model

	records  []ActivityRecordJSON // last activity.query page, canonical order
	tailRows []ActivityRecordJSON // last activity.recent page, NEWEST-FIRST

	sortMode  string // SortChrono | SortTask | SortPlan
	tailOn    bool
	panelOpen bool

	findings []FindingJSON
	drift    []DriftJSON

	dropped  int  // ring eviction counter from the last activity.recent poll
	total    int  // last query total (before limit/offset)
	liveTick bool // a tick cmd is armed (one tick in flight at a time)
	loading  bool
	err      error

	width  int
	height int
}

// Poll cadence for the live tail (master open question Q2 default: 2s).
const activityPollInterval = 2 * time.Second

// errNotConnected is the models-package copy of the parent package's
// ErrNotConnected constant (models cannot import tui).
const errNotConnected = "not connected to daemon"

// Sort modes passed as the activity.query "sort" param. Empty = chrono.
const (
	SortChrono = "chrono"
	SortTask   = "task"
	SortPlan   = "plan"
)

// activityColumnCount is the fixed cell count of an activity-table row.
const activityColumnCount = 6

// ActivityRPCClient is the RPC surface the activity model needs, implemented
// by the TUI RPCClient (rpc.go) and faked in tests. Method results decode the
// C2/C4 result shapes exactly.
type ActivityRPCClient interface {
	ActivityQuery(params ActivityQueryParams) ([]ActivityRecordJSON, int, error)
	ActivityRecent(limit int) ([]ActivityRecordJSON, int, int, error) // records, dropped, total, err
	ActivityFindings(params FindingsParams) ([]FindingJSON, int, error)
	AgentsReview(employeeID string) (DriftJSON, error)
	IsConnected() bool
}

// ActivityQueryParams carries the activity.query filter set (C2). Zero
// values mean "no bound".
type ActivityQueryParams struct {
	Since   string   `json:"since,omitempty"`
	Until   string   `json:"until,omitempty"`
	Sort    string   `json:"sort,omitempty"`
	TaskID  string   `json:"task_id,omitempty"`
	PlanID  string   `json:"plan_id,omitempty"`
	AgentID string   `json:"agent_id,omitempty"`
	Kinds   []string `json:"kinds,omitempty"`
	Limit   int      `json:"limit,omitempty"`
	Offset  int      `json:"offset,omitempty"`
}

// ActivityQueryResponse is the activity.query result (C2).
type ActivityQueryResponse struct {
	Records []ActivityRecordJSON `json:"records"`
	Total   int                  `json:"total"`
}

// ActivityRecordJSON mirrors the C1 ActivityRecord wire shape exactly (json
// tags are the contract; do not rename).
type ActivityRecordJSON struct {
	Kind      string            `json:"kind"`
	At        string            `json:"at"`
	AgentID   string            `json:"agent_id,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	TaskID    string            `json:"task_id,omitempty"`
	StepID    string            `json:"step_id,omitempty"`
	PlanID    string            `json:"plan_id,omitempty"`
	State     string            `json:"state,omitempty"`
	Severity  string            `json:"severity,omitempty"`
	Summary   string            `json:"summary"`
	Detail    map[string]string `json:"detail,omitempty"`
}

// FindingsParams carries the activity.findings filter set (C2).
type FindingsParams struct {
	EmployeeID string `json:"employee_id,omitempty"`
	Since      string `json:"since,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Resolved   *bool  `json:"resolved,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}

// FindingsResponse is the activity.findings result (C2).
type FindingsResponse struct {
	Findings []FindingJSON `json:"findings"`
	Total    int           `json:"total"`
}

// FindingJSON mirrors the employee.AuditFinding wire shape (subset the panel
// renders; unknown fields are ignored by the decoder).
type FindingJSON struct {
	ID           string  `json:"id"`
	EmployeeID   string  `json:"employee_id"`
	GoalID       string  `json:"goal_id,omitempty"`
	PlanID       string  `json:"plan_id,omitempty"`
	TurnID       string  `json:"turn_id,omitempty"`
	Severity     string  `json:"severity"`
	Checkpoint   string  `json:"checkpoint"`
	ViolatedRule string  `json:"violated_rule,omitempty"`
	Evidence     string  `json:"evidence,omitempty"`
	DetectedAt   string  `json:"detected_at"`
	ResolvedAt   string  `json:"resolved_at,omitempty"`
	Resolution   string  `json:"resolution,omitempty"`
	DriftScore   float64 `json:"drift_score,omitempty"`
}

// DriftJSON mirrors the agents.review result subset the panel renders (C4:
// Review carries drift_score + recent_findings + employee).
type DriftJSON struct {
	EmployeeID     string        `json:"-"`
	DriftScore     float64       `json:"drift_score"`
	RecentFindings []FindingJSON `json:"recent_findings,omitempty"`
}

// UnmarshalJSON captures employee.id alongside the review fields: the panel
// keys drift rows by employee, and the review envelope names the employee
// only inside the nested employee object.
func (d *DriftJSON) UnmarshalJSON(data []byte) error {
	type alias DriftJSON
	aux := struct {
		Employee struct {
			ID string `json:"id"`
		} `json:"employee"`
		*alias
	}{alias: (*alias)(d)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	d.EmployeeID = aux.Employee.ID
	return nil
}

// NewActivityModel creates a new activity model (chronological mode, tail
// off, panel closed).
func NewActivityModel(rpc ActivityRPCClient) *ActivityModel {
	t := table.New(
		table.WithColumns(activityColumns(80)),
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
		Foreground(paletteColor("#FFFFFF")). // palette-exempt: literal kept for max-contrast white on primary
		Background(paletteColor("primary")).
		Bold(true)

	t.SetStyles(s)

	return &ActivityModel{
		rpc:      rpc,
		table:    t,
		sortMode: SortChrono,
	}
}

// activityColumns builds the column set for the current width.
func activityColumns(width int) []table.Column {
	return []table.Column{
		{Title: "when", Width: 16},
		{Title: "kind", Width: 14},
		{Title: "summary", Width: max(width-72, 8)},
		{Title: "agent", Width: 14},
		{Title: "state", Width: 10},
		{Title: "sev", Width: 8},
	}
}

// Init returns the initial fetch command. The ticker is NOT armed here:
// polling starts only when the tail is toggled on (no background churn).
func (m *ActivityModel) Init() tea.Cmd {
	return m.fetch()
}

// SetSize updates the model dimensions and re-sizes the table viewport on
// both axes (a width-0 viewport renders no rows).
func (m *ActivityModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.table.SetColumns(activityColumns(width))
	tableutil.Size(&m.table, width, max(height-6, 3))
	m.refreshRows()
}

// SortMode returns the active sort mode ("" is normalized to chrono).
func (m *ActivityModel) SortMode() string { return m.sortMode }

// TailOn reports whether the live-tail poller is running.
func (m *ActivityModel) TailOn() bool { return m.tailOn }

// PanelOpen reports whether the findings/drift panel is showing.
func (m *ActivityModel) PanelOpen() bool { return m.panelOpen }

// Dropped returns the ring's eviction counter from the last tail poll.
func (m *ActivityModel) Dropped() int { return m.dropped }

// Loading reports whether a fetch is in flight.
func (m *ActivityModel) Loading() bool { return m.loading }

// SetSortMode switches the sort mode and refetches (the daemon sorts).
func (m *ActivityModel) SetSortMode(mode string) {
	m.sortMode = mode
	m.loading = true
}

// fetch issues activity.query for the current sort mode.
func (m *ActivityModel) fetch() tea.Cmd {
	return func() tea.Msg {
		if !m.rpc.IsConnected() {
			return ActivityUpdateMsg{Err: errors.New(errNotConnected)}
		}
		records, total, err := m.rpc.ActivityQuery(ActivityQueryParams{Sort: m.sortMode, Limit: 200})
		if err != nil {
			return ActivityUpdateMsg{Err: err}
		}
		return ActivityUpdateMsg{Records: records, Total: total}
	}
}

// fetchRecent polls activity.recent for the live tail.
func (m *ActivityModel) fetchRecent() tea.Msg {
	if !m.rpc.IsConnected() {
		return RecentUpdateMsg{Err: errors.New(errNotConnected)}
	}
	records, dropped, _, err := m.rpc.ActivityRecent(50)
	if err != nil {
		return RecentUpdateMsg{Err: err}
	}
	return RecentUpdateMsg{Records: records, Dropped: dropped}
}

// fetchFindings issues activity.findings (open findings first page).
func (m *ActivityModel) fetchFindings() tea.Cmd {
	return func() tea.Msg {
		if !m.rpc.IsConnected() {
			return FindingsUpdateMsg{Err: errors.New(errNotConnected)}
		}
		findings, total, err := m.rpc.ActivityFindings(FindingsParams{Limit: 100})
		if err != nil {
			return FindingsUpdateMsg{Err: err}
		}
		return FindingsUpdateMsg{Findings: findings, Total: total}
	}
}

// fetchDrift calls agents.review for each agent seen in the current records.
func (m *ActivityModel) fetchDrift() tea.Cmd {
	return func() tea.Msg {
		if !m.rpc.IsConnected() {
			return DriftUpdateMsg{Err: errors.New(errNotConnected)}
		}
		seen := make([]string, 0, len(m.records))
		for _, r := range m.records {
			if r.AgentID == "" || slices.Contains(seen, r.AgentID) {
				continue
			}
			seen = append(seen, r.AgentID)
		}
		drift := make([]DriftJSON, 0, len(seen))
		for _, id := range seen {
			review, err := m.rpc.AgentsReview(id)
			if err != nil {
				continue // one agent's review failure must not blank the panel
			}
			if review.EmployeeID == "" {
				review.EmployeeID = id
			}
			drift = append(drift, review)
		}
		return DriftUpdateMsg{Drift: drift}
	}
}

// tailTick arms the next poll tick (only while the tail is on).
func (m *ActivityModel) tailTick() tea.Cmd {
	if !m.tailOn {
		return nil
	}
	m.liveTick = true
	return tea.Tick(activityPollInterval, func(time.Time) tea.Msg {
		return ActivityTickMsg{}
	})
}

// ActivityUpdateMsg carries an activity.query result.
type ActivityUpdateMsg struct {
	Records []ActivityRecordJSON
	Total   int
	Err     error
}

// RecentUpdateMsg carries an activity.recent poll result.
type RecentUpdateMsg struct {
	Records []ActivityRecordJSON
	Dropped int
	Err     error
}

// FindingsUpdateMsg carries an activity.findings result.
type FindingsUpdateMsg struct {
	Findings []FindingJSON
	Total    int
	Err      error
}

// DriftUpdateMsg carries the agents.review rollup.
type DriftUpdateMsg struct {
	Drift []DriftJSON
	Err   error
}

// ActivityTickMsg fires on the live-tail poll cadence.
type ActivityTickMsg struct{}

// hideActivityViewMsg is delivered by the App when the activity view stops
// being the current view; it stops the tail so no poller runs hidden.
type hideActivityViewMsg struct{}

// HideActivityView constructs the hide message (exported so the parent tui
// package can deliver it on view switches; the type stays unexported to
// keep the message out of other packages' APIs).
func HideActivityView() tea.Msg { return hideActivityViewMsg{} }

// StopTail stops the live-tail poller (called via hideActivityViewMsg or a
// second toggle press).
func (m *ActivityModel) StopTail() {
	m.tailOn = false
	m.liveTick = false
}

// Update handles messages for the activity view.
func (m *ActivityModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case ActivityUpdateMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.records = msg.Records
		m.total = msg.Total
		m.refreshRows()
		return nil

	case RecentUpdateMsg:
		if msg.Err != nil {
			// A failed poll must not kill the tail; keep the last rows.
			return m.tailTick()
		}
		m.err = nil
		m.tailRows = msg.Records
		m.dropped = msg.Dropped
		m.refreshRows()
		return m.tailTick()

	case FindingsUpdateMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.findings = msg.Findings
		return nil

	case DriftUpdateMsg:
		if msg.Err != nil {
			return nil
		}
		m.drift = msg.Drift
		return nil

	case ActivityTickMsg:
		if !m.tailOn {
			return nil
		}
		return m.fetchRecent

	case hideActivityViewMsg:
		m.StopTail()
		return nil

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "r":
			m.loading = true
			if m.panelOpen {
				return tea.Batch(m.fetchFindings(), m.fetchDrift())
			}
			return m.fetch()

		case KeyTab:
			// Cycle sort mode: chrono -> task -> plan -> chrono. The
			// daemon sorts; each cycle issues a fresh activity.query.
			switch m.sortMode {
			case SortChrono:
				m.SetSortMode(SortTask)
			case SortTask:
				m.SetSortMode(SortPlan)
			default:
				m.SetSortMode(SortChrono)
			}
			return m.fetch()

		case "t":
			// Live-tail toggle. Turning it on arms the tick; turning it
			// off cancels (the armed tick no-ops on tailOn=false).
			if m.tailOn {
				m.StopTail()
				return nil
			}
			m.tailOn = true
			return m.tailTick()

		case "p":
			// Findings + drift panel toggle. Opening fetches both.
			m.panelOpen = !m.panelOpen
			if m.panelOpen {
				m.loading = true
				return tea.Batch(m.fetchFindings(), m.fetchDrift())
			}
			return nil

		case KeyEsc:
			if m.panelOpen {
				m.panelOpen = false
				return nil
			}
			return nil

		case "up", "down", "j", "k":
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return cmd
		}
	}

	// Pass other messages to the table.
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return cmd
}

// refreshRows rebuilds the table rows from the caches. In tail mode the
// recent page IS the table (chronological, oldest first, tail -f style);
// otherwise the query page renders
// with group headers in task/plan modes. Both paths go through
// tableutil.SetRows (row length == column count).
func (m *ActivityModel) refreshRows() {
	var rows []table.Row
	if m.tailOn {
		rows = make([]table.Row, 0, len(m.tailRows))
		for _, r := range m.tailRows {
			rows = append(rows, m.recordRow(r))
		}
	} else {
		rows = make([]table.Row, 0, len(m.records)+4)
		if m.sortMode == SortChrono {
			for _, r := range m.records {
				rows = append(rows, m.recordRow(r))
			}
		} else {
			rows = m.groupedRows(rows)
		}
	}
	tableutil.SetRows(&m.table, rows)
}

// groupedRows renders task/plan mode with one header row per group. The
// daemon already returns records in canonical group order (group key, then
// time ascending, empty group last), so grouping is a scan that emits a
// header whenever the key changes.
func (m *ActivityModel) groupedRows(rows []table.Row) []table.Row {
	getKey := func(r ActivityRecordJSON) string {
		if m.sortMode == SortTask {
			return r.TaskID
		}
		return r.PlanID
	}
	lastKey := "\x00" // never matches a real (possibly empty) key
	for _, r := range m.records {
		key := getKey(r)
		if key != lastKey {
			label := key
			if label == "" {
				label = "no " + m.sortMode
			}
			rows = append(rows, m.headerRow(label))
			lastKey = key
		}
		rows = append(rows, m.recordRow(r))
	}
	return rows
}

// headerRow builds the group separator row (spanning the summary column).
func (m *ActivityModel) headerRow(label string) table.Row {
	return table.Row{"-- " + label + " --", "", "", "", "", ""}
}

// recordRow maps one record to a table row (cell count == column count).
func (m *ActivityModel) recordRow(r ActivityRecordJSON) table.Row {
	when := r.At
	if ts, err := time.Parse(time.RFC3339, r.At); err == nil {
		when = ts.Local().Format("15:04:05")
	}
	sev := r.Severity
	if sev == "" {
		sev = "-"
	}
	return table.Row{when, r.Kind, r.Summary, r.AgentID, r.State, sev}
}

// View renders the activity view (lowercase text throughout).
func (m *ActivityModel) View() string {
	if m.panelOpen {
		return m.renderPanel()
	}

	var b strings.Builder

	// Header: title + sort mode + tail state.
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("primary"))
	modeStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Background(paletteColor("surfaceAlt")).
		Padding(0, 1)
	activeStyle := lipgloss.NewStyle().
		Foreground(paletteColor("#FFFFFF")). // palette-exempt: literal kept for max-contrast white on primary
		Background(paletteColor("primary")).
		Bold(true).
		Padding(0, 1)

	b.WriteString(titleStyle.Render("activity"))
	b.WriteString("  ")
	for _, mode := range []string{SortChrono, SortTask, SortPlan} {
		if m.sortMode == mode {
			b.WriteString(activeStyle.Render(mode))
		} else {
			b.WriteString(modeStyle.Render(mode))
		}
		b.WriteString(" ")
	}
	if m.tailOn {
		tailStyle := lipgloss.NewStyle().Foreground(paletteColor("success"))
		label := "tail on"
		if m.dropped > 0 {
			label = fmt.Sprintf("tail on (dropped %d)", m.dropped)
		}
		b.WriteString("  ")
		b.WriteString(tailStyle.Render(label))
	}
	b.WriteString("\n")

	if m.loading {
		style := lipgloss.NewStyle().
			Width(m.width).
			Align(lipgloss.Center).
			Padding(2, 0)
		b.WriteString(style.Render("loading activity..."))
		return b.String()
	}

	if m.err != nil {
		errStyle := lipgloss.NewStyle().
			Foreground(paletteColor("error")).
			Padding(0, 1)
		b.WriteString(errStyle.Render("error: " + m.err.Error()))
		b.WriteString("\n")
	}

	// Table
	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("border"))
	b.WriteString(tableStyle.Render(m.table.View()))
	b.WriteString("\n")

	hintStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Width(m.width).
		MaxWidth(m.width)
	hint := "r: refresh | tab: sort mode | t: live tail | p: findings panel | j/k: navigate"
	if m.tailOn {
		hint = "r: refresh | tab: sort mode | t: stop tail | p: findings panel | j/k: navigate"
	}
	b.WriteString(hintStyle.Render(hint))

	return b.String()
}

// renderPanel renders the findings + drift panel (T4): one drift summary row
// per employee (score 2-decimal), then the findings list. Findings text is
// severity-tagged and lowercase; raw evidence NEVER renders (it can carry
// arbitrary tool output).
func (m *ActivityModel) renderPanel() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("primary"))
	b.WriteString(titleStyle.Render("findings + drift"))
	b.WriteString("\n")

	if m.err != nil {
		errStyle := lipgloss.NewStyle().
			Foreground(paletteColor("error")).
			Padding(0, 1)
		b.WriteString(errStyle.Render("error: " + m.err.Error()))
		b.WriteString("\n")
	}

	sevStyle := func(sev string) lipgloss.Style {
		switch sev {
		case "critical":
			return lipgloss.NewStyle().Foreground(paletteColor("error")).Bold(true)
		case "warning":
			return lipgloss.NewStyle().Foreground(paletteColor("warning"))
		default:
			return lipgloss.NewStyle().Foreground(paletteColor("info"))
		}
	}

	// Drift summary: one row per employee.
	driftStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("border")).
		Width(m.width).
		MaxWidth(m.width)
	var db strings.Builder
	db.WriteString("drift by employee\n")
	if len(m.drift) == 0 {
		db.WriteString("  (no agents reviewed)\n")
	}
	for _, d := range m.drift {
		name := d.EmployeeID
		if name == "" {
			name = "unknown"
		}
		line := fmt.Sprintf("  %-24s drift %.2f", name, d.DriftScore)
		style := lipgloss.NewStyle()
		switch {
		case d.DriftScore >= 0.7:
			style = style.Foreground(paletteColor("error"))
		case d.DriftScore >= 0.4:
			style = style.Foreground(paletteColor("warning"))
		default:
			style = style.Foreground(paletteColor("success"))
		}
		db.WriteString(style.Render(line))
		db.WriteString("\n")
	}
	b.WriteString(driftStyle.Render(db.String()))
	b.WriteString("\n")

	// Findings list.
	findingsStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("border")).
		Width(m.width).
		MaxWidth(m.width)
	var fb strings.Builder
	open := 0
	for _, f := range m.findings {
		if f.ResolvedAt == "" {
			open++
		}
	}
	fmt.Fprintf(&fb, "findings (%d total, %d open)\n", len(m.findings), open)
	if len(m.findings) == 0 {
		fb.WriteString("  (no findings)\n")
	}
	for _, f := range m.findings {
		state := "open"
		if f.ResolvedAt != "" {
			state = "resolved"
		}
		when := f.DetectedAt
		if ts, err := time.Parse(time.RFC3339, f.DetectedAt); err == nil {
			when = ts.Local().Format("01-02 15:04")
		}
		line := fmt.Sprintf("  %s  %-8s %-9s %-14s %s (%s)",
			when, f.Severity, state, f.EmployeeID, f.ViolatedRule, f.Checkpoint)
		fb.WriteString(sevStyle(f.Severity).Render(line))
		fb.WriteString("\n")
	}
	b.WriteString(findingsStyle.Render(fb.String()))
	b.WriteString("\n")

	hintStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Width(m.width).
		MaxWidth(m.width)
	b.WriteString(hintStyle.Render("p/esc: back to activity | r: refresh"))

	return b.String()
}
