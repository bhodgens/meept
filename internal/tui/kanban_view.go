// Package tui — kanban_view.go implements the kanban board view
// (plans/kanban-board 05-surfaces/03-tui-board): a TUI surface over the
// kanban.* RPC methods (master.md C4) following the C6 surface contract:
// sized tables (tableutil invariant), card detail + timeline, and
// keybinding-driven human commands whose lease/WIP refusals (C10) surface
// holder + age / column limit to the user.
//
// It follows the agents_panel.go pattern: a model with a small RPC
// interface (KanbanRPCClient) so tests inject a fake, cached wire rows,
// a sized bubbles/table per sub-view, and loading/error/empty states.
//
// Wire-shape note: the daemon's kanban structs carry no json tags (C1)
// while the sidecar/GUI lanes use snake_case, so this view decodes
// tolerantly — both spellings — exactly like
// ui/flutter_ui/lib/features/kanban/kanban_models.dart, without pinning
// 05/01 handler internals.
package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/tableutil"
)

// kanbanActor is the actor label this surface sends on human commands.
// The store records it on kanban_card_events rows (C3), so the timeline
// can tell tui moves from gui/cli ones.
const kanbanActor = "tui"

// KanbanRPCClient is the small interface App satisfies via *RPCClient.
// Declared here so tests can inject a fake (same shape as AgentsRPCClient).
type KanbanRPCClient interface {
	Call(method string, params any) (json.RawMessage, error)
	IsConnected() bool
}

// kanbanSubView picks between the card table and the card detail view.
type kanbanSubView int

const (
	kanbanViewBoard kanbanSubView = iota
	kanbanViewDetail
)

// kanbanPromptMode is the active line-input prompt (assign / comment /
// takeover confirm). Esc cancels; enter submits (assign/comment); y/n
// answer the takeover confirm — the destructive-confirm vocabulary the
// acp permission modal already uses.
type kanbanPromptMode int

const (
	kanbanPromptNone kanbanPromptMode = iota
	kanbanPromptAssign
	kanbanPromptComment
	kanbanPromptTakeover
)

// Cached wire rows.
type kanbanBoardRow struct {
	ID   string
	Name string
}

type kanbanColumnRow struct {
	ID       string
	Name     string
	Position int
	WIP      int // 0 = no limit (C1)
}

type kanbanCardRow struct {
	ID         string
	ColumnID   string
	Title      string
	RefKind    string
	RefID      string
	EmployeeID string
	Labels     []string
	Order      int
}

type kanbanTimelineRow struct {
	Kind      string
	Actor     string
	Body      string
	Timestamp string
}

// KanbanPanel is the Bubbletea model for the board view.
type KanbanPanel struct {
	rpc KanbanRPCClient

	boards   []kanbanBoardRow
	boardIdx int // selected board (cycle with [ / ])
	columns  []kanbanColumnRow
	cards    []kanbanCardRow // sorted by column position then order

	subView        kanbanSubView
	prompt         kanbanPromptMode
	inputBuf       string
	selectedID     string // drill-in / prompt target card id
	detailCard     *kanbanCardRow
	timeline       []kanbanTimelineRow
	pendingRef     string // decoded last refusal line ("" when none)
	pendingIsLease bool   // refusal was ErrLeaseHeld -> offer takeover replay
	pendingCard    string // refused move's card id (takeover replay target)
	pendingToCol   string // refused move's destination column id

	loading bool
	err     error
	status  string // transient success line (moved / assigned / ...)

	table   table.Model
	tlTable table.Model
	width   int
	height  int
}

// NewKanbanPanel builds the panel (mirrors NewAgentsPanel's construction
// so App wiring stays uniform).
func NewKanbanPanel(rpc KanbanRPCClient) *KanbanPanel {
	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "column"}, {Title: "card"}, {Title: "ref"},
			{Title: "who"}, {Title: "labels"},
		}),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	tl := table.New(
		table.WithColumns([]table.Column{
			{Title: "time"}, {Title: "kind"}, {Title: "actor"}, {Title: "body"},
		}),
		table.WithFocused(false),
		table.WithHeight(6),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(Current().Border).
		BorderBottom(true).
		Bold(true).
		Foreground(Current().Primary)
	s.Selected = s.Selected.
		Foreground(Current().Background).
		Background(Current().Primary).
		Bold(true)
	t.SetStyles(s)
	tl.SetStyles(s)

	return &KanbanPanel{
		rpc:     rpc,
		subView: kanbanViewBoard,
		table:   t,
		tlTable: tl,
	}
}

// SetSize sizes BOTH tables on both axes (tableutil invariant: a width-0
// viewport renders nothing) and refits columns, then repopulates rows
// from the caches so a resize never blanks the board.
func (p *KanbanPanel) SetSize(width, height int) {
	p.width = width
	p.height = height
	if width < 20 || height < 6 {
		return
	}
	tableHeight := max(height-4, 2)
	tableutil.Size(&p.table, width, tableHeight)
	tableutil.Size(&p.tlTable, width, max(tableHeight/2, 2))
	p.resizeColumns()
}

func (p *KanbanPanel) resizeColumns() {
	// Clear rows before SetColumns (SetColumns re-renders them).
	p.table.SetRows([]table.Row{})
	p.tlTable.SetRows([]table.Row{})

	if p.width < 30 {
		return
	}
	// Fit the viewport budget, not the outer width: see agents_panel's
	// M12 comment — hand-computed widths overflow the bordered box and
	// bubbles/table MaxWidth-truncates silently.
	mins := []int{8, 10, 6, 6, 4}
	weights := []int{16, 40, 14, 12, 14}
	widths := tableutil.FitWidthsMin(tableutil.ContentBudget(p.table.Width(), len(mins)), mins, weights)
	p.table.SetColumns([]table.Column{
		{Title: "column", Width: widths[0]},
		{Title: "card", Width: widths[1]},
		{Title: "ref", Width: widths[2]},
		{Title: "who", Width: widths[3]},
		{Title: "labels", Width: widths[4]},
	})

	tlMins := []int{8, 6, 6, 8}
	tlWeights := []int{16, 14, 14, 50}
	tlWidths := tableutil.FitWidthsMin(tableutil.ContentBudget(p.tlTable.Width(), len(tlMins)), tlMins, tlWeights)
	p.tlTable.SetColumns([]table.Column{
		{Title: "time", Width: tlWidths[0]},
		{Title: "kind", Width: tlWidths[1]},
		{Title: "actor", Width: tlWidths[2]},
		{Title: "body", Width: tlWidths[3]},
	})

	p.updateBoardTable()
	p.updateTimelineTable()
}

// columnIndex maps a column id to its position-sorted slot and name.
func (p *KanbanPanel) columnIndex(id string) (int, string, bool) {
	for i, c := range p.columns {
		if c.ID == id {
			return i, c.Name, true
		}
	}
	return -1, "", false
}

// updateBoardTable renders the cached cards as sized table rows: columns
// are the first table column, cards are rows (leaf 05/03 contract).
func (p *KanbanPanel) updateBoardTable() {
	if len(p.table.Columns()) == 0 {
		return
	}
	rows := make([]table.Row, 0, len(p.cards))
	for _, c := range p.cards {
		_, colName, ok := p.columnIndex(c.ColumnID)
		if !ok {
			colName = "?"
		}
		ref := ""
		if c.RefKind != "" || c.RefID != "" {
			ref = strings.TrimSpace(c.RefKind + " " + shortKanbanID(c.RefID))
		}
		rows = append(rows, table.Row{
			strings.ToLower(colName),
			c.Title,
			ref,
			c.EmployeeID,
			strings.Join(c.Labels, ","),
		})
	}
	tableutil.SetRows(&p.table, rows)
}

// updateTimelineTable renders the detail view's timeline rows (append-only
// kanban_card_events, oldest first, C3).
func (p *KanbanPanel) updateTimelineTable() {
	if len(p.tlTable.Columns()) == 0 {
		return
	}
	rows := make([]table.Row, 0, len(p.timeline))
	for _, ev := range p.timeline {
		ts := ev.Timestamp
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			ts = t.Local().Format("01-02 15:04")
		}
		rows = append(rows, table.Row{ts, strings.ToLower(ev.Kind), ev.Actor, ev.Body})
	}
	tableutil.SetRows(&p.tlTable, rows)
}

// ------------------------------------------------------------------
// fetch commands (C4 read methods; reads ride the query-style param map
// the sidecar precedent uses: {"board_id": ...}, {"card_id": ...})
// ------------------------------------------------------------------

type kanbanBoardsMsg struct {
	boards []kanbanBoardRow
	err    error
}

type kanbanBoardDataMsg struct {
	columns []kanbanColumnRow
	cards   []kanbanCardRow
	err     error
}

type kanbanDetailMsg struct {
	card     *kanbanCardRow
	timeline []kanbanTimelineRow
	err      error
}

// kanbanActionMsg carries one mutation result. note is the rendered line
// (decoded refusal, or success text); the lease fields let `t` replay the
// refused move as a takeover without re-deriving anything from strings.
type kanbanActionMsg struct {
	err        error
	note       string
	isLease    bool
	cardID     string // refused move's card
	toColumnID string // refused move's destination
}

func (p *KanbanPanel) Init() tea.Cmd {
	return p.fetchBoards()
}

// kanbanDisabledHint is the C3 user-facing guidance shown when the daemon
// has not registered the kanban.* RPC methods ([kanban].enabled=false).
const kanbanDisabledHint = "kanban board is not enabled; set [kanban].enabled=true"

// kanbanFetchErr maps the daemon's "method not found: kanban.board.list"
// refusal (returned when the kanban subsystem is disabled) to the short C3
// guidance string. Every other error is passed through unchanged so the
// existing error rendering stays intact.
func kanbanFetchErr(err error) error {
	if strings.Contains(err.Error(), "method not found: kanban.board.list") {
		return errors.New(kanbanDisabledHint)
	}
	return err
}

func (p *KanbanPanel) fetchBoards() tea.Cmd {
	return func() tea.Msg {
		if p.rpc == nil || !p.rpc.IsConnected() {
			return kanbanBoardsMsg{err: errors.New(ErrNotConnected)}
		}
		raw, err := p.rpc.Call("kanban.board.list", nil)
		if err != nil {
			return kanbanBoardsMsg{err: kanbanFetchErr(err)}
		}
		var boards []kanbanBoardRow
		for _, m := range kanbanDecodeList(raw, "boards", "Boards") {
			boards = append(boards, kanbanBoardRow{
				ID:   kanbanFieldStr(m, "id", "ID"),
				Name: kanbanFieldStr(m, "name", "Name"),
			})
		}
		return kanbanBoardsMsg{boards: boards}
	}
}

func (p *KanbanPanel) fetchBoardData() tea.Cmd {
	boardID := p.currentBoardID()
	if boardID == "" {
		return nil
	}
	return func() tea.Msg {
		if p.rpc == nil || !p.rpc.IsConnected() {
			return kanbanBoardDataMsg{err: errors.New(ErrNotConnected)}
		}
		rawCols, err := p.rpc.Call("kanban.column.list", map[string]string{"board_id": boardID})
		if err != nil {
			return kanbanBoardDataMsg{err: err}
		}
		var columns []kanbanColumnRow
		for _, m := range kanbanDecodeList(rawCols, "columns", "Columns") {
			columns = append(columns, kanbanColumnRow{
				ID:       kanbanFieldStr(m, "id", "ID"),
				Name:     kanbanFieldStr(m, "name", "Name"),
				Position: kanbanFieldInt(m, "position", "Position"),
				WIP:      kanbanFieldInt(m, "wip", "WIP"),
			})
		}
		sort.Slice(columns, func(i, j int) bool { return columns[i].Position < columns[j].Position })

		rawCards, err := p.rpc.Call("kanban.card.list", map[string]string{"board_id": boardID})
		if err != nil {
			return kanbanBoardDataMsg{err: err}
		}
		var cards []kanbanCardRow
		for _, m := range kanbanDecodeList(rawCards, "cards", "Cards") {
			cards = append(cards, kanbanDecodeCard(m))
		}
		// Display order: column position, then manual order (C1
		// Card.Order), then id for stability.
		pos := make(map[string]int, len(columns))
		for i, c := range columns {
			pos[c.ID] = i
		}
		sort.SliceStable(cards, func(i, j int) bool {
			pi, pj := pos[cards[i].ColumnID], pos[cards[j].ColumnID]
			if pi != pj {
				return pi < pj
			}
			if cards[i].Order != cards[j].Order {
				return cards[i].Order < cards[j].Order
			}
			return cards[i].ID < cards[j].ID
		})
		return kanbanBoardDataMsg{columns: columns, cards: cards}
	}
}

func (p *KanbanPanel) fetchDetail(cardID string) tea.Cmd {
	if cardID == "" {
		return nil
	}
	return func() tea.Msg {
		if p.rpc == nil || !p.rpc.IsConnected() {
			return kanbanDetailMsg{err: errors.New(ErrNotConnected)}
		}
		rawCard, err := p.rpc.Call("kanban.card.get", map[string]string{"card_id": cardID})
		if err != nil {
			return kanbanDetailMsg{err: err}
		}
		card := kanbanDecodeCard(kanbanDecodeRecord(rawCard, "card", "Card", "result"))

		rawTl, err := p.rpc.Call("kanban.card.timeline", map[string]string{"card_id": cardID})
		if err != nil {
			return kanbanDetailMsg{err: err}
		}
		var events []kanbanTimelineRow
		for _, m := range kanbanDecodeList(rawTl, "events", "Events") {
			events = append(events, kanbanTimelineRow{
				Kind:      kanbanFieldStr(m, "kind", "Kind"),
				Actor:     kanbanFieldStr(m, "actor", "Actor"),
				Body:      kanbanFieldStr(m, "body", "Body"),
				Timestamp: kanbanFieldStr(m, "created_at", "CreatedAt"),
			})
		}
		return kanbanDetailMsg{card: &card, timeline: events}
	}
}

func (p *KanbanPanel) currentBoardID() string {
	if p.boardIdx < 0 || p.boardIdx >= len(p.boards) {
		return ""
	}
	return p.boards[p.boardIdx].ID
}

// ------------------------------------------------------------------
// human commands (C4 mutations + C10 error surfacing)
// ------------------------------------------------------------------

// selectedCard returns the card under the board table cursor. Rows are
// written from p.cards in order, so the cursor indexes the cache.
func (p *KanbanPanel) selectedCard() (kanbanCardRow, bool) {
	idx := p.table.Cursor()
	if idx < 0 || idx >= len(p.cards) {
		return kanbanCardRow{}, false
	}
	return p.cards[idx], true
}

func (p *KanbanPanel) moveCard(card kanbanCardRow, delta int) tea.Cmd {
	colIdx, _, ok := p.columnIndex(card.ColumnID)
	if !ok || len(p.columns) == 0 {
		return nil
	}
	target := colIdx + delta
	if target < 0 || target >= len(p.columns) {
		p.setNotice("no column to the " + kanbanDirectionName(delta))
		return nil
	}
	to := p.columns[target]
	cardID := card.ID
	return func() tea.Msg {
		err := p.callRPC("kanban.card.move", map[string]any{
			"card_id":   cardID,
			"column_id": to.ID,
			"actor":     kanbanActor,
		})
		if err != nil {
			// C10: the UI must display holder + age (lease) or the
			// limit (wip). Decode here so the panel just renders the
			// note, and record the refused move so `t` can replay it.
			return kanbanActionMsg{
				err:        err,
				note:       kanbanRefusalMessage(err),
				isLease:    kanbanLeaseRe.MatchString(err.Error()),
				cardID:     cardID,
				toColumnID: to.ID,
			}
		}
		return kanbanActionMsg{note: "moved to " + strings.ToLower(to.Name)}
	}
}

// takeoverCard runs the ONLY lease bypass (decision 1 / C10): cancels the
// episode, releases the token, audits, then moves. When toCol is non-empty
// it completes the refused move; empty releases the claim in place.
// Requires board.mutate AND board.takeover (C5); a missing grant surfaces
// as the daemon's permission error through the same refusal line.
func (p *KanbanPanel) takeoverCard(cardID, toCol string) tea.Cmd {
	if cardID == "" {
		return nil
	}
	return func() tea.Msg {
		params := map[string]any{"card_id": cardID, "actor": kanbanActor}
		if toCol != "" {
			params["to_column_id"] = toCol
		}
		if err := p.callRPC("kanban.card.takeover", params); err != nil {
			return kanbanActionMsg{err: err, note: kanbanRefusalMessage(err)}
		}
		return kanbanActionMsg{note: "takeover done (audited)"}
	}
}

func (p *KanbanPanel) assignCard(cardID, employeeID string) tea.Cmd {
	if cardID == "" || employeeID == "" {
		return nil
	}
	return func() tea.Msg {
		err := p.callRPC("kanban.card.assign", map[string]any{
			"card_id": cardID, "employee_id": employeeID,
		})
		if err != nil {
			return kanbanActionMsg{err: err, note: kanbanRefusalMessage(err)}
		}
		return kanbanActionMsg{note: "assigned " + employeeID}
	}
}

func (p *KanbanPanel) commentCard(cardID, text string) tea.Cmd {
	if cardID == "" || text == "" {
		return nil
	}
	return func() tea.Msg {
		err := p.callRPC("kanban.card.comment", map[string]any{
			"card_id": cardID, "body": text,
		})
		if err != nil {
			return kanbanActionMsg{err: err, note: kanbanRefusalMessage(err)}
		}
		return kanbanActionMsg{note: "comment added"}
	}
}

func (p *KanbanPanel) reconcileNow() tea.Cmd {
	return func() tea.Msg {
		// Empty params = legal mutation (the gateway forwards nil params).
		err := p.callRPC("kanban.reconcile.now", map[string]any{})
		if err != nil {
			return kanbanActionMsg{err: err, note: kanbanRefusalMessage(err)}
		}
		return kanbanActionMsg{note: "reconcile done (board == source)"}
	}
}

func (p *KanbanPanel) callRPC(method string, params any) error {
	if p.rpc == nil || !p.rpc.IsConnected() {
		return errors.New(ErrNotConnected)
	}
	_, err := p.rpc.Call(method, params)
	return err
}

// ------------------------------------------------------------------
// C10 refusal decoding (frozen kanban error strings, C1)
// ------------------------------------------------------------------

var (
	// ErrLeaseHeld.Error() = "lease held by <holder> since <rfc3339>" —
	// the same regex contract the Flutter lane pins (kanban_service.dart
	// _parseLeaseHeld).
	kanbanLeaseRe = regexp.MustCompile(`lease held by (\S+) since (\S+)`)
	// ErrWIPFull.Error() = "WIP limit reached for column <id>: <limit>".
	kanbanWIPRe = regexp.MustCompile(`WIP limit reached for column (\S+): (\d+)`)
)

// kanbanRefusalMessage decodes a forwarded kanban refusal into a
// lowercase surface line. Lease refusals get holder + age (C10: "The UI
// must display holder + age from the error") plus the takeover hint; WIP
// refusals get the column + limit.
func kanbanRefusalMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if m := kanbanLeaseRe.FindStringSubmatch(msg); m != nil {
		age := "unknown age"
		if since, perr := time.Parse(time.RFC3339, m[2]); perr == nil {
			age = kanbanAgeLabel(time.Since(since))
		}
		return fmt.Sprintf("lease held by %s for %s — press t to takeover", m[1], age)
	}
	if m := kanbanWIPRe.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("wip full: column %s limit %s (wip_mode=block)", m[1], m[2])
	}
	return strings.ToLower(msg)
}

// kanbanAgeLabel formats a lease age: seconds under a minute, else the
// shared minute/hour formatter (format_duration.go, parity with the GUI's
// relative lease label).
func kanbanAgeLabel(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return FormatDuration(d)
}

func kanbanDirectionName(delta int) string {
	if delta < 0 {
		return "left"
	}
	return "right"
}
