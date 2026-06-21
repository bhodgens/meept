package models

import (
	"sort"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// SessionSwitchToChatMsg signals a session switch that also switches view to chat.
type SessionSwitchToChatMsg struct {
	Session *types.Session
}

// SessionsModel is a full-screen model for browsing sessions with a detail pane.
type SessionsModel struct {
	rpc          SessionsRPCClient
	sessions     []types.Session
	plans        []types.PlanExtended // Plans for the selected session
	table        table.Model
	selected     *types.Session
	width        int
	height       int
	loading      bool
	plansLoading bool
	err          error
	plansErr     error // Error from fetching plans for selected session

	// UI state
	showingDetail bool
	showingHelp   bool
}

// SessionsRPCClient interface for the sessions model.
type SessionsRPCClient interface {
	Call(method string, params any) (json.RawMessage, error)
	ListSessions() (*types.SessionListResponse, error)
	IsConnected() bool
}

// SessionsUpdateMsg carries the sessions update.
type SessionsUpdateMsg struct {
	Sessions []types.Session
	Err      error
}

// SessionPlansUpdateMsg carries plans for a specific session.
type SessionPlansUpdateMsg struct {
	SessionID string
	Plans     []types.PlanExtended
	Err       error
}

// OpenCreateSessionModalMsg signals a request to open the create session modal.
type OpenCreateSessionModalMsg struct{}

// NewSessionsModel creates a new sessions model.
func NewSessionsModel(rpc SessionsRPCClient) *SessionsModel {
	columns := []table.Column{
		{Title: "title", Width: 28},
		{Title: "created", Width: 12},
		{Title: "last activity", Width: 14},
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

	return &SessionsModel{
		rpc:   rpc,
		table: t,
	}
}

// SetSize updates the model dimensions.
func (m *SessionsModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	tableHeight := max(height-10, 5)
	m.table.SetHeight(tableHeight)

	// Set table viewport width to match the rendered table container's
	// inner width (tableWidth minus border frame). Without this, the
	// viewport width stays at 0 and View() returns "" — the table
	// appears blank even though rows are populated and navigation works.
	detailWidth := max(width/3, 30)
	tableWidth := width - detailWidth - 4
	m.table.SetWidth(tableWidth - 2) // -2 for rounded border frame

	m.setSessionsColumns()
}

func (m *SessionsModel) setSessionsColumns() {
	titleW := m.width*38/100 - 2
	createdW := 12
	activityW := 14

	if titleW < 15 {
		titleW = 15
	}

	m.table.SetColumns([]table.Column{
		{Title: "title", Width: titleW},
		{Title: "created", Width: createdW},
		{Title: "last activity", Width: activityW},
	})

	// Repopulate rows from cached data so resize doesn't wipe the table.
	// setSessionsColumns is called on every WindowSizeMsg; without this,
	// rows are lost and only restored when a new SessionsUpdateMsg arrives.
	if len(m.sessions) > 0 {
		m.updateSessionsTable()
	}
}

// Init initializes the sessions model.
func (m *SessionsModel) Init() tea.Cmd {
	return m.fetchSessions
}

func (m *SessionsModel) fetchSessions() tea.Msg {
	if !m.rpc.IsConnected() {
		return SessionsUpdateMsg{Err: fmt.Errorf("not connected")}
	}

	resp, err := m.rpc.ListSessions()
	if err != nil {
		return SessionsUpdateMsg{Err: err}
	}

	return SessionsUpdateMsg{Sessions: resp.Sessions}
}

func (m *SessionsModel) fetchSessionPlans(sessionID string) tea.Cmd {
	return func() tea.Msg {
		raw, err := m.rpc.Call("plan.list_by_session", map[string]any{"session_id": sessionID})
		if err != nil {
			return SessionPlansUpdateMsg{SessionID: sessionID, Err: err}
		}

		var resp types.PlanListResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return SessionPlansUpdateMsg{SessionID: sessionID, Err: fmt.Errorf("unmarshal plans: %w", err)}
		}

		if resp.Err != "" {
			return SessionPlansUpdateMsg{SessionID: sessionID, Err: fmt.Errorf("%s", resp.Err)}
		}

		return SessionPlansUpdateMsg{SessionID: sessionID, Plans: resp.Plans}
	}
}

// Update handles messages for the sessions view.
func (m *SessionsModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case SessionsUpdateMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.sessions = msg.Sessions
		m.sortSessions()
		m.updateSessionsTable()
		// Fetch plans for the auto-selected session
		if m.selected != nil {
			m.plansLoading = true
			return m.fetchSessionPlans(m.selected.ID)
		}
		return nil

	case SessionPlansUpdateMsg:
		m.plansLoading = false
		if msg.Err != nil {
			m.plansErr = msg.Err
			return nil
		}
		m.plansErr = nil
		// Only update if it's for the currently selected session
		if m.selected != nil && msg.SessionID == m.selected.ID {
			m.plans = msg.Plans
		}
		return nil

	case tea.KeyPressMsg:
		if m.showingDetail {
			if msg.String() == KeyEsc || msg.String() == "q" {
				m.showingDetail = false
			}
			return nil
		}

		if m.showingHelp {
			m.showingHelp = false
			return nil
		}

		switch msg.String() {
		case "n":
			// Request to create a new session - app will show input modal
			return func() tea.Msg {
				return OpenCreateSessionModalMsg{}
			}

		case "f":
			// Open the global search view
			return func() tea.Msg {
				return OpenSearchViewMsg{}
			}

		case "r":
			m.loading = true
			return m.fetchSessions

		case "?":
			m.showingHelp = true
			return nil

		case KeyEnter:
			if len(m.sessions) > 0 {
				idx := m.table.Cursor()
				if idx >= 0 && idx < len(m.sessions) {
					sess := &m.sessions[idx]
					// Return command that signals session switch with chat view
					return func() tea.Msg {
						return SessionSwitchToChatMsg{Session: sess}
					}
				}
			}
			return nil

		case KeyEsc:
			m.selected = nil
			m.showingDetail = false
			return nil

		case "up", "down", "j", "k":
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			idx := m.table.Cursor()
			if idx >= 0 && idx < len(m.sessions) {
				wasSelected := m.selected
				m.selected = &m.sessions[idx]
				// Fetch plans for the new selection if it changed
				if wasSelected == nil || wasSelected.ID != m.selected.ID {
					m.plansLoading = true
					m.plans = nil
					m.plansErr = nil
					return m.fetchSessionPlans(m.selected.ID)
				}
			}
			return cmd
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return cmd
}

func (m *SessionsModel) updateSessionsTable() {
	rows := make([]table.Row, len(m.sessions))

	// Derive title truncation from the dynamic column width so wider
	// terminals can show more of the session name/description.
	titleW := max(m.width*38/100-2, 15)

	for i, sess := range m.sessions {
		title := sess.Description
		if title == "" {
			title = sess.Name
		}

		created := m.formatTime(sess.CreatedAt)
		activity := m.formatTimeRelative(sess.LastActivity)

		rows[i] = table.Row{
			types.TruncateString(title, titleW),
			created,
			activity,
		}
	}

	m.table.SetRows(rows)
	if len(rows) > 0 {
		m.table.GotoTop()
	}

	// Auto-select first session (plans fetch triggered by caller)
	if len(m.sessions) > 0 {
		m.selected = &m.sessions[0]
	}
}

func (m *SessionsModel) formatTime(timestamp string) string {
	if timestamp == "" {
		return StatusNA
	}
	if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
		return t.Format("Jan 02 15:04")
	}
	if len(timestamp) > 8 {
		return types.TruncateString(timestamp[len(timestamp)-8:], 8)
	}
	return timestamp
}

func (m *SessionsModel) formatTimeRelative(timestamp string) string {
	if timestamp == "" {
		return StatusNA
	}
	if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
		return formatRelativeTime(t)
	}
	return types.TruncateString(timestamp, 14)
}

// View renders the sessions view.
func (m *SessionsModel) View() string {
	if m.showingDetail && m.selected != nil {
		return m.renderSessionDetailModal()
	}

	if m.showingHelp {
		return m.renderHelp()
	}

	if m.loading && len(m.sessions) == 0 {
		return m.renderLoading()
	}

	if m.err != nil && len(m.sessions) == 0 {
		return m.renderError()
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Split into left (table) and right (detail) panes
	detailWidth := max(m.width/3, 30)
	tableWidth := m.width - detailWidth - 4

	// Table
	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Width(tableWidth)

	// Detail pane
	detailStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F97316")).
		Width(detailWidth).
		Height(m.height - 8)

	var detailContent string
	if m.selected != nil {
		detailContent = m.renderSessionDetail()
	} else {
		detailContent = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorGray)).
			Italic(true).
			Render("select a session")
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Top, tableStyle.Render(m.table.View()), detailStyle.Render(detailContent))
	b.WriteString(joined)
	b.WriteString("\n")

	// Help hint
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		MarginTop(1)

	b.WriteString(hintStyle.Render("n: new | r: refresh | enter: details | up/down: navigate | f: search | ?: help"))

	return b.String()
}

func (m *SessionsModel) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316"))

	countStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray))

	title := titleStyle.Render("sessions")
	count := countStyle.Render(fmt.Sprintf("(%d)", len(m.sessions)))

	return lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", count)
}

func (m *SessionsModel) renderSessionDetail() string {
	sess := m.selected
	if sess == nil {
		return ""
	}

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Width(10)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E5E7EB"))

	displayTitle := sess.Description
	if displayTitle == "" {
		displayTitle = sess.Name
	}
	content.WriteString(titleStyle.Render(types.TruncateString(displayTitle, 28)))
	content.WriteString("\n\n")

	// Session ID
	content.WriteString(labelStyle.Render("id:"))
	content.WriteString(valueStyle.Render(types.TruncateString(sess.ID, 30)))
	content.WriteString("\n")

	// Name
	content.WriteString(labelStyle.Render("name:"))
	content.WriteString(valueStyle.Render(sess.Name))
	content.WriteString("\n")

	// Created
	content.WriteString(labelStyle.Render("created:"))
	content.WriteString(valueStyle.Render(m.formatTime(sess.CreatedAt)))
	content.WriteString("\n")

	// Last activity
	content.WriteString(labelStyle.Render("activity:"))
	content.WriteString(valueStyle.Render(m.formatTimeRelative(sess.LastActivity)))
	content.WriteString("\n")

	// Attached clients
	if len(sess.AttachedClients) > 0 {
		content.WriteString(labelStyle.Render("clients:"))
		content.WriteString(valueStyle.Render(fmt.Sprintf("%d", len(sess.AttachedClients))))
		content.WriteString("\n")
	}

	// Plans section
	content.WriteString("\n")
	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber)).
		Bold(true)
	content.WriteString(sectionStyle.Render("--- plans ---"))
	content.WriteString("\n")

	if m.plansLoading {
		content.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Italic(true).Render("loading..."))
	} else if m.plansErr != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Italic(true)
		content.WriteString(errStyle.Render(fmt.Sprintf("error: %s", types.TruncateString(m.plansErr.Error(), 30))))
	} else if len(m.plans) == 0 {
		content.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Italic(true).Render("no plans"))
	} else {
		for _, plan := range m.plans {
			stateColor := m.getPlanStateColor(plan.State)
			stateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor))
			icon := m.getPlanStateIcon(plan.State)

			title := plan.Title
			if title == "" {
				title = types.TruncateString(plan.ID, 16)
			}
			title = types.TruncateString(title, 20)

			content.WriteString(stateStyle.Render(icon))
			content.WriteString(" ")
			content.WriteString(valueStyle.Render(title))
			content.WriteString("\n")
		}
	}

	return content.String()
}

func (m *SessionsModel) renderSessionDetailModal() string {
	sess := m.selected
	if sess == nil {
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

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Width(14)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	var content strings.Builder

	displayTitle := sess.Description
	if displayTitle == "" {
		displayTitle = sess.Name
	}
	content.WriteString(titleStyle.Render("session: " + types.TruncateString(displayTitle, 50)))
	content.WriteString("\n\n")

	content.WriteString(labelStyle.Render("id:"))
	content.WriteString(valueStyle.Render(sess.ID))
	content.WriteString("\n")

	content.WriteString(labelStyle.Render("name:"))
	content.WriteString(valueStyle.Render(sess.Name))
	content.WriteString("\n")

	if sess.Description != "" {
		content.WriteString(labelStyle.Render("description:"))
		content.WriteString(valueStyle.Render(sess.Description))
		content.WriteString("\n")
	}

	content.WriteString(labelStyle.Render("created:"))
	content.WriteString(valueStyle.Render(m.formatTime(sess.CreatedAt)))
	content.WriteString("\n")

	content.WriteString(labelStyle.Render("last activity:"))
	content.WriteString(valueStyle.Render(m.formatTimeRelative(sess.LastActivity)))
	content.WriteString("\n")

	if len(sess.AttachedClients) > 0 {
		content.WriteString(labelStyle.Render("clients:"))
		content.WriteString(valueStyle.Render(fmt.Sprintf("%d attached", len(sess.AttachedClients))))
		content.WriteString("\n")
	}

	// Plans section
	if len(m.plans) > 0 || m.plansLoading {
		content.WriteString("\n")
		sectionStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorAmber)).
			Bold(true)
		content.WriteString(sectionStyle.Render("--- associated plans ---"))
		content.WriteString("\n")

		if m.plansLoading {
			content.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Italic(true).Render("loading..."))
		} else {
			for _, plan := range m.plans {
				stateColor := m.getPlanStateColor(plan.State)
				stateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(stateColor))

				title := plan.Title
				if title == "" {
					title = plan.ID
				}

				progress := ""
				if plan.TotalSteps > 0 {
					pct := float64(plan.CompletedSteps) / float64(plan.TotalSteps) * 100
					progress = fmt.Sprintf(" (%.0f%%)", pct)
				}

				content.WriteString(stateStyle.Render(m.getPlanStateIcon(plan.State)))
				content.WriteString(" ")
				content.WriteString(valueStyle.Render(types.TruncateString(title, 40)))
				content.WriteString(stateStyle.Render(progress))
				content.WriteString("\n")
			}
		}
	}

	// Footer
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Italic(true)
	content.WriteString(footerStyle.Render("[esc/q] close"))

	return modalStyle.Render(content.String())
}

func (m *SessionsModel) renderLoading() string {
	style := lipgloss.NewStyle().
		Width(m.width-4).
		Align(lipgloss.Center).
		Padding(4, 0)

	return style.Render("loading sessions...")
}

func (m *SessionsModel) renderError() string {
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

func (m *SessionsModel) renderHelp() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F97316")).
		Padding(2, 4).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber)).
		Bold(true).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	content := titleStyle.Render("sessions view help") + "\n\n"
	content += keyStyle.Render("n") + descStyle.Render("create new session") + "\n"
	content += keyStyle.Render("up/k") + descStyle.Render("move cursor up") + "\n"
	content += keyStyle.Render("down/j") + descStyle.Render("move cursor down") + "\n"
	content += keyStyle.Render("enter") + descStyle.Render("open session detail") + "\n"
	content += keyStyle.Render("esc") + descStyle.Render("close detail") + "\n"
	content += keyStyle.Render("r") + descStyle.Render("refresh sessions") + "\n"
	content += keyStyle.Render("f") + descStyle.Render("global search") + "\n"
	content += keyStyle.Render("?") + descStyle.Render("toggle this help") + "\n"

	content += "\n"
	content += lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Render("press any key to close")

	return panelStyle.Render(content)
}

func (m *SessionsModel) getPlanStateIcon(state string) string {
	switch state {
	case "planning":
		return "●"
	case "draft":
		return "○"
	case "pending_approval":
		return "◑"
	case "approved":
		return "✓"
	case "executing":
		return "◐"
	case "completed":
		return "●"
	case "confirmed":
		return "★"
	case "failed":
		return "✗"
	case "cancelled":
		return "○"
	default:
		return "?"
	}
}

// formatRelativeTime returns a human-readable relative time string.
func formatRelativeTime(t time.Time) string {
	diff := time.Since(t)

	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		mins := int(diff.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	}
	if diff < 24*time.Hour {
		hours := int(diff.Hours())
		return fmt.Sprintf("%dh ago", hours)
	}
	if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
	if diff < 30*24*time.Hour {
		weeks := int(diff.Hours() / 24 / 7)
		return fmt.Sprintf("%dw ago", weeks)
	}

	return t.Format("Jan 2")
}

func (m *SessionsModel) getPlanStateColor(state string) string {
	switch state {
	case "planning":
		return "#3B82F6"
	case "draft":
		return ColorGray
	case "pending_approval":
		return "#3B82F6"
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

// sortSessions sorts sessions by designation priority, then by last activity.
func (m *SessionsModel) sortSessions() {
	sort.Slice(m.sessions, func(i, j int) bool {
		iDesig := m.sessions[i].Designation
		jDesig := m.sessions[j].Designation

		// Sessions with designation come first
		if iDesig != nil && jDesig == nil {
			return true
		}
		if iDesig == nil && jDesig != nil {
			return false
		}
		if iDesig != nil && jDesig != nil {
			// Both have designation, sort by priority
			priorityOrder := map[string]int{"urgent": 0, "high": 1, "normal": 2, "low": 3}
			iPriority := priorityOrder[iDesig.Priority]
			jPriority := priorityOrder[jDesig.Priority]
			if iPriority != jPriority {
				return iPriority < jPriority
			}
			// Same priority, sort by status
			statusOrder := map[string]int{"requires_approval": 0, "waiting_human": 1, "human_responded": 2, "bot_thinking": 3}
			return statusOrder[iDesig.Status] < statusOrder[jDesig.Status]
		}

		// No designation, sort by last activity (most recent first)
		return m.sessions[i].LastActivity > m.sessions[j].LastActivity
	})
}
