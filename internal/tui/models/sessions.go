package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/tableutil"
	"github.com/caimlas/meept/internal/tui/types"
)

// SessionSwitchToChatMsg signals a session switch that also switches view to chat.
type SessionSwitchToChatMsg struct {
	Session *types.Session
}

// ArchiveSessionRequestedMsg signals a request to archive (or unarchive) a session.
// When Archived is true the session is soft-archived; when false it is restored.
// Handled by App.Update which dispatches the RPC call via RPCClient.ArchiveSession.
type ArchiveSessionRequestedMsg struct {
	SessionID   string
	SessionName string
	Archived    bool
}

// DeleteSessionRequestedMsg signals a permanent delete request (shift+D).
// Handled by App.Update which dispatches the existing SessionDeleteMsg flow.
type DeleteSessionRequestedMsg struct {
	SessionID   string
	SessionName string
}

// SessionTraceRequestedMsg signals a dispatch-trace fetch for the
// selected session (09-cli-affordances task 6, key `t`). The model runs
// the fetch itself and lands the rows as SessionTraceUpdateMsg.
type SessionTraceRequestedMsg struct {
	SessionID string
}

// SessionTraceUpdateMsg carries the session.dispatch_trace rows for a
// session (or the fetch error). Rendered in the sessions detail pane as
// the routing-evidence block.
type SessionTraceUpdateMsg struct {
	SessionID string
	Entries   []dispatchTraceRow
	Err       error
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
	detailWidth  int // outer width of the right-hand detail pane, from SetSize
	loading      bool
	plansLoading bool
	err          error
	plansErr     error // Error from fetching plans for selected session

	// Dispatch-trace surfacing (09-cli-affordances task 6): rows fetched
	// via session.dispatch_trace for the selected session, plus the
	// fetch error and loading flag. Shown in the detail pane.
	trace          []dispatchTraceRow
	traceErr       error
	traceLoading   bool
	traceSessionID string // session the current trace rows belong to

	// UI state
	showingDetail bool
	showingHelp   bool

	// Project-grouped tree state. collapsed holds C2 buckets whose children
	// are hidden; the remaining fields are derived from the last row build
	// and map table rows (which include group headers) back to sessions.
	collapsed   map[string]bool
	rowSessions []*types.Session // nil for group header rows
	rowBuckets  []string         // C2 bucket of each rendered row
	headerRows  map[string]int   // bucket -> row index of its group header
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
		BorderForeground(paletteColor("border")).
		BorderBottom(true).
		Bold(true).
		Foreground(paletteColor("primary"))

	s.Selected = s.Selected.
		Foreground(paletteColor("#FFFFFF")). // palette-exempt: literal kept for max-contrast white on primary
		Background(paletteColor("primary")).
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

	// Both axes must be set: the table viewport starts at width 0, and a
	// zero-width viewport renders no rows (header only, blank body).
	detailWidth := max(width/3, 30)
	m.detailWidth = detailWidth
	// The panes join edge-to-edge (lipgloss.JoinHorizontal with no
	// separator, each carrying its own border), so the table viewport gets
	// exactly what the detail pane leaves. The old `width-detailWidth-4`
	// reserved a phantom 4-column gap that was never rendered, stealing the
	// width the date columns needed (M14). Clamped so a narrow pane cannot
	// produce a non-positive table width.
	tableWidth := max(width-detailWidth, 1)
	tableutil.Size(&m.table, tableWidth, tableHeight)

	m.setSessionsColumns()
}

func (m *SessionsModel) setSessionsColumns() {
	// Fitted to the viewport (tableutil.FitWidthsMin): the fixed 12+14
	// columns plus a 15-column title floor overflow a narrow pane, so
	// bubbles/table rendered a box wider than its container and truncated
	// every cell (M9).
	mins := []int{15, 12, 14}
	weights := []int{38, 12, 14}
	widths := tableutil.FitWidthsMin(tableutil.ContentBudget(m.table.Width(), len(mins)), mins, weights)

	m.table.SetColumns([]table.Column{
		{Title: "title", Width: widths[0]},
		{Title: "created", Width: widths[1]},
		{Title: "last activity", Width: widths[2]},
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

// fetchSessionTrace loads the dispatch-trace rows for a session through
// session.dispatch_trace (09-cli-affordances task 6, same RPC the chat
// why-this-route popover and the CLI `meept session trace` use).
func (m *SessionsModel) fetchSessionTrace(sessionID string) tea.Cmd {
	return func() tea.Msg {
		raw, err := m.rpc.Call("session.dispatch_trace", map[string]any{"session_id": sessionID})
		if err != nil {
			return SessionTraceUpdateMsg{SessionID: sessionID, Err: err}
		}

		var resp struct {
			Entries []dispatchTraceRow `json:"entries"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return SessionTraceUpdateMsg{SessionID: sessionID, Err: fmt.Errorf("unmarshal dispatch trace: %w", err)}
		}

		return SessionTraceUpdateMsg{SessionID: sessionID, Entries: resp.Entries}
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
		prevID := ""
		if m.selected != nil {
			prevID = m.selected.ID
		}
		m.updateSessionsTable()
		// Fetch plans for the selected session — on first load and when
		// the preserved selection changed (the old row was deleted or
		// collapsed away). A refresh that keeps the same session must
		// not refetch its plans on every poll (L14).
		if m.selected != nil && (prevID == "" || m.selected.ID != prevID) {
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

	case SessionTraceRequestedMsg:
		// 09-cli-affordances task 6: fetch the dispatch trace for the
		// requested session (key `t` in the sessions view).
		m.traceLoading = true
		m.traceErr = nil
		m.trace = nil
		return m.fetchSessionTrace(msg.SessionID)

	case SessionTraceUpdateMsg:
		// 09-cli-affordances task 6: dispatch-trace rows landed for the
		// selected session. A stale session's rows are dropped.
		m.traceLoading = false
		if msg.Err != nil {
			m.traceErr = msg.Err
			m.trace = nil
			return nil
		}
		m.traceErr = nil
		if m.selected != nil && msg.SessionID == m.selected.ID {
			m.trace = msg.Entries
			m.traceSessionID = msg.SessionID
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
			// Open the search view scoped to sessions (C4 reuse).
			return func() tea.Msg {
				return OpenSearchViewMsg{Scope: "sessions"}
			}

		case "r":
			m.loading = true
			return m.fetchSessions

		case "d":
			// Archive (or unarchive) the selected session. Default trash
			// action mirrors the Flutter UI where archive is the primary
			// swipe action and permanent delete requires a long-press.
			if s := m.selectedSession(); s != nil {
				next := !s.Archived // toggle: archived → unarchive, active → archive
				name := s.Description
				if name == "" {
					name = s.Name
				}
				sessID := s.ID
				return func() tea.Msg {
					return ArchiveSessionRequestedMsg{SessionID: sessID, SessionName: name, Archived: next}
				}
			}
			return nil

		case "D":
			// Permanent delete (shift+d). Requires explicit capital to
			// avoid accidental data loss, mirroring the Flutter long-press
			// confirmation flow.
			if s := m.selectedSession(); s != nil {
				name := s.Description
				if name == "" {
					name = s.Name
				}
				sessID := s.ID
				return func() tea.Msg {
					return DeleteSessionRequestedMsg{SessionID: sessID, SessionName: name}
				}
			}
			return nil

		case "t":
			// 09-cli-affordances task 6: surface the dispatch trace for
			// the selected session (why each turn routed the way it did).
			// The request rides a message (same protocol shape as the
			// archive/delete flows); the model handles it by fetching.
			if s := m.selectedSession(); s != nil {
				sessID := s.ID
				return func() tea.Msg {
					return SessionTraceRequestedMsg{SessionID: sessID}
				}
			}
			return nil

		case "?":
			m.showingHelp = true
			return nil

		case KeyEnter:
			if s := m.selectedSession(); s != nil {
				// Return command that signals session switch with chat view
				return func() tea.Msg {
					return SessionSwitchToChatMsg{Session: s}
				}
			}
			return nil

		case KeyLeft:
			// Collapse the project group under the cursor (hide its children).
			m.setBucketCollapsed(m.bucketAtCursor(), true)
			return nil

		case KeyRight:
			// Expand the project group under the cursor (show its children).
			m.setBucketCollapsed(m.bucketAtCursor(), false)
			return nil

		case KeyEsc:
			m.selected = nil
			m.showingDetail = false
			return nil

		case "up", "down", "j", "k":
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			// The cursor indexes grouped rows (headers included), not the
			// flat sessions slice. Map through rowSessions; a header row has
			// no session and leaves m.selected unchanged.
			if s := m.selectedSession(); s != nil {
				wasSelected := m.selected
				m.selected = s
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

// archivedRowStyle dims archived session rows. Applied per-cell since
// bubbles/table has no per-row style API. Foreground resolves the palette's
// textMuted role lazily at first use so it tracks the active theme instead
// of being frozen at init time.
var archivedRowStyle = sync.OnceValue(func() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(paletteColor("textMuted"))
})

func (m *SessionsModel) updateSessionsTable() {
	// L14: preserve the selected session id across rebuilds (refresh and
	// resize) — the agents panel's cursor-preservation discipline. An
	// unconditional GotoTop + reselect-first yanked the user's selection
	// back to the first session on every poll.
	prevID := ""
	if s := m.selectedSession(); s != nil {
		prevID = s.ID
	} else if m.selected != nil {
		prevID = m.selected.ID
	}

	tree := m.buildSessionTreeRows()
	m.rowSessions = tree.sessions
	m.rowBuckets = tree.buckets
	m.headerRows = tree.headers

	tableutil.SetRows(&m.table, tree.rows)

	// Restore the cursor to the row carrying the previously selected
	// session; fall back to the first session row only when it is absent
	// (deleted, or hidden inside a collapsed bucket). Header rows are
	// non-selectable, so a fresh list parks on the first child session:
	// existing key handlers (d/D/enter) act on the cursor target, and
	// leaving it on a header would make a freshly loaded list inert.
	m.selected = nil
	if prevID != "" {
		for i, s := range m.rowSessions {
			if s != nil && s.ID == prevID {
				m.selected = s
				m.table.SetCursor(i)
				return
			}
		}
	}
	for i, s := range m.rowSessions {
		if s != nil {
			m.selected = s
			m.table.SetCursor(i)
			break
		}
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

	// Table. No Width here: lipgloss treats Width as a floor on the border
	// block, so it re-expanded the (already fitted) table past the pane and
	// pushed the join past the terminal. MaxWidth is the bound that matters.
	tableStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("border"))

	// Detail pane. Same: Width is a floor, MaxWidth is the ceiling. Without
	// MaxWidth the bordered block grew to the detail CONTENT's width, so a
	// long "created:" value rendered a pane wider than the space it was
	// allocated (M9).
	detailStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("primary")).
		MaxWidth(detailWidth).
		Height(m.height - 8)

	var detailContent string
	if m.selected != nil {
		detailContent = m.renderSessionDetail()
	} else {
		detailContent = lipgloss.NewStyle().
			Foreground(paletteColor("textMuted")).
			Width(max(detailWidth-4, 1)).
			MaxWidth(max(detailWidth-4, 1)).
			Italic(true).
			Render("select a session")
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Top, tableStyle.Render(m.table.View()), detailStyle.Render(detailContent))
	b.WriteString(joined)
	b.WriteString("\n")

	// Help hint. Wrapped to the pane rather than rendered bare: this literal is
	// 88 columns and lipgloss pads a style block to its widest line, so the
	// hint — not the panes — was setting the view's width (M9). MarginTop is
	// dropped for the same reason a margin block pads every line.
	hintStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Width(m.width).
		MaxWidth(m.width)

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("n: new | d: archive | D: delete | t: trace | r: refresh | enter: details | up/down: navigate | left/right: collapse/expand | f: search | ?: help"))

	return b.String()
}

func (m *SessionsModel) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("primary"))

	countStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted"))

	title := titleStyle.Render("sessions")
	count := countStyle.Render(fmt.Sprintf("(%d)", len(m.sessions)))

	return lipgloss.JoinHorizontal(lipgloss.Left, title, "  ", count)
}

func (m *SessionsModel) renderSessionDetail() string {
	sess := m.selected
	if sess == nil {
		return ""
	}

	// The value column carries every field's content, so it is the one that
	// must be bounded: a bare 10-column label style plus an unbounded value
	// lets one long field (a full "created: 2023-03-14 09:26" stamp) stretch
	// the block past its pane, and the pane is then joined against the table
	// and the sidebar (M9). The value column takes the pane's inner width
	// minus the label.
	inner := max(m.detailWidth-4, 1)
	labelStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Width(10)
	valueStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textPrimary")).
		Width(max(inner-10, 1)).
		MaxWidth(max(inner-10, 1))

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("textPrimary"))

	displayTitle := sess.Description
	if displayTitle == "" {
		displayTitle = sess.Name
	}
	content.WriteString(titleStyle.Render(types.TruncateString(displayTitle, 28)))
	if badge := m.getDesignationBadge(sess); badge != "" {
		content.WriteString(" ")
		content.WriteString(badge)
	}
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
		Foreground(paletteColor("warning")).
		Bold(true)
	content.WriteString(sectionStyle.Render("--- plans ---"))
	content.WriteString("\n")

	if m.plansLoading {
		content.WriteString(lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Italic(true).Render("loading..."))
	} else if m.plansErr != nil {
		errStyle := lipgloss.NewStyle().Foreground(paletteColor("error")).Italic(true)
		content.WriteString(errStyle.Render(fmt.Sprintf("error: %s", types.TruncateString(m.plansErr.Error(), 30))))
	} else if len(m.plans) == 0 {
		content.WriteString(lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Italic(true).Render("no plans"))
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

	// Dispatch trace section (09-cli-affordances task 6): the routing
	// evidence for the selected session, fetched on demand via `t`.
	content.WriteString("\n")
	content.WriteString(sectionStyle.Render("--- dispatch trace ---"))
	content.WriteString("\n")
	content.WriteString(m.renderTraceSection(valueStyle))

	return content.String()
}

// renderTraceSection renders the dispatch-trace block of the detail
// pane: loading / error / empty / one row per routing decision.
func (m *SessionsModel) renderTraceSection(valueStyle lipgloss.Style) string {
	mutedStyle := lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Italic(true)
	errStyle := lipgloss.NewStyle().Foreground(paletteColor("error")).Italic(true)

	switch {
	case m.traceLoading:
		return mutedStyle.Render("loading...")
	case m.traceErr != nil:
		return errStyle.Render(fmt.Sprintf("error: %s", types.TruncateString(m.traceErr.Error(), 30)))
	case len(m.trace) == 0:
		return mutedStyle.Render("no trace loaded (press t)")
	default:
		var b strings.Builder
		for _, e := range m.trace {
			b.WriteString(fmt.Sprintf("  intent: %s  case: %s  confidence: %.2f\n",
				e.IntentType, e.HandlerCase, e.Confidence))
			if e.InputSummary != "" {
				summary := types.TruncateString(e.InputSummary, 40)
				b.WriteString(valueStyle.Render("    " + summary))
				b.WriteString("\n")
			}
		}
		return b.String()
	}
}

func (m *SessionsModel) renderSessionDetailModal() string {
	sess := m.selected
	if sess == nil {
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

	labelStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Width(14)

	valueStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textPrimary"))

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
			Foreground(paletteColor("warning")).
			Bold(true)
		content.WriteString(sectionStyle.Render("--- associated plans ---"))
		content.WriteString("\n")

		if m.plansLoading {
			content.WriteString(lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Italic(true).Render("loading..."))
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
		Foreground(paletteColor("textMuted")).
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

func (m *SessionsModel) renderHelp() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("primary")).
		Padding(2, 4).
		Width(m.width - 4)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("primary")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(paletteColor("warning")).
		Bold(true).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textPrimary"))

	content := titleStyle.Render("sessions view help") + "\n\n"
	content += keyStyle.Render("n") + descStyle.Render("create new session") + "\n"
	content += keyStyle.Render("up/k") + descStyle.Render("move cursor up") + "\n"
	content += keyStyle.Render("down/j") + descStyle.Render("move cursor down") + "\n"
	content += keyStyle.Render("enter") + descStyle.Render("open session detail") + "\n"
	content += keyStyle.Render("d") + descStyle.Render("archive session (soft-delete)") + "\n"
	content += keyStyle.Render("D") + descStyle.Render("delete session permanently") + "\n"
	content += keyStyle.Render("t") + descStyle.Render("show dispatch trace for selected session") + "\n"
	content += keyStyle.Render("esc") + descStyle.Render("close detail") + "\n"
	content += keyStyle.Render("r") + descStyle.Render("refresh sessions") + "\n"
	content += keyStyle.Render("f") + descStyle.Render("search sessions") + "\n"
	content += keyStyle.Render("?") + descStyle.Render("toggle this help") + "\n"

	content += "\n"
	content += lipgloss.NewStyle().Foreground(paletteColor("textMuted")).Render("press any key to close")

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
		return paletteHex("textMuted")
	case "pending_approval":
		return "#3B82F6"
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

// selectedSession returns the session under the table cursor, or nil when the
// cursor is on a group header (or out of range). With the grouped tree the
// table row index no longer maps 1:1 to m.sessions, so the derived
// rowSessions slice is the source of truth.
func (m *SessionsModel) selectedSession() *types.Session {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.rowSessions) {
		return nil
	}
	return m.rowSessions[idx]
}

// sortSessions sorts sessions by designation priority, then by last activity.
// Archived sessions always sort to the bottom of their respective group
// (designated vs. non-designated), mirroring the Flutter _sessionSort.
func (m *SessionsModel) sortSessions() {
	sort.SliceStable(m.sessions, func(i, j int) bool {
		// Archived sessions sink to the bottom regardless of other fields.
		iArchived := m.sessions[i].Archived
		jArchived := m.sessions[j].Archived
		if iArchived != jArchived {
			return !iArchived
		}

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

// getDesignationBadge returns a styled badge for session designation.
func (m *SessionsModel) getDesignationBadge(sess *types.Session) string {
	if sess.Designation == nil || sess.Designation.Status == "none" {
		return ""
	}

	statusInfo := map[string]struct{ icon, color string }{
		"requires_approval": {"!", "#EF4444"},
		"waiting_human":     {"?", "#F97316"},
		"human_responded":   {"R", "#22C55E"},
		"bot_thinking":      {"...", "#3B82F6"},
	}

	info := statusInfo[sess.Designation.Status]
	if info.icon == "" {
		info = statusInfo["waiting_human"]
	}

	priorityBadge := ""
	if sess.Designation.Priority == "urgent" || sess.Designation.Priority == "high" {
		// UI text is lowercase (AGENTS.md UI Conventions): render the
		// priority as-is in lowercase, never strings.ToUpper ("[URGENT]").
		priorityBadge = " [" + strings.ToLower(sess.Designation.Priority) + "]"
	}

	badgeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(info.color))
	return badgeStyle.Render(" " + info.icon + " " + sess.Designation.Status + priorityBadge + " ")
}
