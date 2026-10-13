package models

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/tableutil"
)

// MemoryReviewModal is the TUI human-review view over the auto-claim review
// queue (leaf T03-L2). It lists status=auto claims from the SAME
// memory.reviewQueue RPC the CLI `meept memory review` uses, and verdicts go
// through the SAME memory.promoteClaim / memory.rejectClaim methods the CLI
// uses — no second write path. After a verdict the list refreshes from the
// daemon, dropping the verdict-ed row while keeping the cursor stable.
//
// All UI text is lowercase (repo convention).
type MemoryReviewModal struct {
	rpc ReviewRPCClient

	items    []ReviewClaim
	table    table.Model
	selected *ReviewClaim // detail panel; nil when closed
	visible  bool
	loading  bool
	err      error

	width  int
	height int
}

// reviewPreviewMax caps the content-preview column width (chars).
const reviewPreviewMax = 60

// reviewColumnCount is the fixed cell count of a review-table row.
const reviewColumnCount = 5

// ReviewClaim is one pending auto claim as returned by memory.reviewQueue.
// Field names match the daemon response keys (memory.Memory JSON + wrapper).
type ReviewClaim struct {
	ClaimID    string  `json:"claim_id"`
	Content    string  `json:"content"`
	Type       string  `json:"type"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	CreatedAt  string  `json:"created_at"`
}

// ReviewQueueParams carries the review-queue filters (leaf T03-L1 contract).
// Nil pointer fields mean "no bound".
type ReviewQueueParams struct {
	MinConfidence *float64 `json:"min_confidence,omitempty"`
	MaxConfidence *float64 `json:"max_confidence,omitempty"`
	OlderThan     string   `json:"older_than,omitempty"`
	Limit         int      `json:"limit,omitempty"`
	Offset        int      `json:"offset,omitempty"`
}

// ReviewQueueResponse is the memory.reviewQueue payload the TUI consumes
// (auto_claims items + total; decisions/predictions are ignored here).
type ReviewQueueResponse struct {
	AutoClaims []ReviewClaim `json:"auto_claims"`
	Total      int           `json:"total"`
}

// ReviewRPCClient is the RPC surface the review modal needs, implemented by
// the TUI RPCClient (rpc.go) and faked in tests. Verdict methods map 1:1 to
// the memory.promoteClaim / memory.rejectClaim RPC methods.
type ReviewRPCClient interface {
	ReviewQueue(params *ReviewQueueParams) (*ReviewQueueResponse, error)
	PromoteClaim(id, reason string) error
	RejectClaim(id, reason string) error
	IsConnected() bool
}

// ReviewQueueMsg carries a fetched review queue.
type ReviewQueueMsg struct {
	Items []ReviewClaim
	Total int
	Err   error
}

// ReviewVerdictMsg carries the outcome of one verdict call.
type ReviewVerdictMsg struct {
	ClaimID string
	Verdict string // "promote" | "reject"
	Err     error
}

// NewMemoryReviewModal creates the review modal backed by rpc (nil-safe:
// fetches report "not connected" instead of panicking).
func NewMemoryReviewModal(rpc ReviewRPCClient) *MemoryReviewModal {
	t := table.New(
		table.WithColumns(reviewColumns(100)),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(paletteColor("border")).
		BorderBottom(true).
		Bold(true).
		Foreground(paletteColor("info"))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#FFFFFF")). // palette-exempt: max-contrast literal
		Background(paletteColor("warning")).
		Bold(true)
	t.SetStyles(s)

	return &MemoryReviewModal{
		rpc:   rpc,
		table: t,
	}
}

// reviewColumns builds the column set for a viewport width (sized by
// tableutil.FitWidthsMin so the sum never exceeds the content budget).
func reviewColumns(viewportWidth int) []table.Column {
	widths := tableutil.FitWidthsMin(
		tableutil.ContentBudget(viewportWidth, reviewColumnCount),
		[]int{14, 10, 10, 8, 24},
		[]int{14, 10, 10, 8, 30},
	)
	titles := []string{"id", "confidence", "category", "age", "preview"}
	cols := make([]table.Column, reviewColumnCount)
	for i, title := range titles {
		cols[i] = table.Column{Title: title, Width: widths[i]}
	}
	return cols
}

// reviewRows rebuilds the table rows from the cached items. Always written
// through tableutil.SetRows (row/cell-count normalization).
func (m *MemoryReviewModal) reviewRows() {
	rows := make([]table.Row, len(m.items))
	for i, item := range m.items {
		rows[i] = table.Row{
			reviewTruncate(item.ClaimID, 18),
			fmt.Sprintf("%.2f", item.Confidence),
			reviewTruncate(item.Category, 10),
			reviewAge(item.CreatedAt),
			reviewPreview(item.Content, reviewPreviewMax),
		}
	}
	tableutil.SetRows(&m.table, rows)
	// bubbles/table only upper-clamps the cursor inside SetRows, so a
	// table emptied by SetSize's column resync keeps cursor=-1; restore 0
	// so verdict keys act on the first row instead of no-op-ing.
	if len(rows) > 0 && m.table.Cursor() < 0 {
		m.table.SetCursor(0)
	}
}

// SetSize updates the model dimensions. Both table axes are set through
// tableutil.Size: bubbles/table's viewport starts at width 0 and a zero-width
// viewport renders an empty body even with rows populated.
func (m *MemoryReviewModal) SetSize(width, height int) {
	m.width = width
	m.height = height

	tableHeight := max(height-8, 4)
	tableutil.Size(&m.table, width, tableHeight)

	// A column change re-renders installed rows, so clear first, re-set
	// columns at the new widths, then repopulate from cache.
	m.table.SetRows([]table.Row{})
	m.table.SetColumns(reviewColumns(m.table.Width()))
	m.reviewRows()
}

// reviewTruncate truncates s to max chars (rune-safe, no ellipsis — bubbles
// MaxWidth-truncates the cell anyway).
func reviewTruncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// reviewPreview renders the content preview: newlines flattened, capped at
// max runes.
func reviewPreview(content string, max int) string {
	flat := strings.ReplaceAll(content, "\n", " ")
	return reviewTruncate(strings.TrimSpace(flat), max)
}

// reviewAge renders a coarse wall-clock age from the claim's created_at
// (RFC3339). Unparseable timestamps render "-" rather than a wrong age.
func reviewAge(createdAt string) string {
	if createdAt == "" {
		return "-"
	}
	ts, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return "-"
	}
	d := time.Since(ts)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Init fetches the review queue.
func (m *MemoryReviewModal) Init() tea.Cmd {
	return m.fetchQueue
}

// fetchQueue issues the memory.reviewQueue RPC (same method the CLI uses).
func (m *MemoryReviewModal) fetchQueue() tea.Msg {
	resp, err := m.rpc.ReviewQueue(&ReviewQueueParams{Limit: 50})
	if err != nil {
		return ReviewQueueMsg{Err: fmt.Errorf("review queue: %w", err)}
	}
	return ReviewQueueMsg{Items: resp.AutoClaims, Total: resp.Total}
}

// Show makes the modal visible and returns the fetch command.
func (m *MemoryReviewModal) Show() tea.Cmd {
	m.visible = true
	m.selected = nil
	m.err = nil
	return m.fetchQueue
}

// Hide closes the modal.
func (m *MemoryReviewModal) Hide() {
	m.visible = false
	m.selected = nil
}

// IsVisible reports whether the modal is open.
func (m *MemoryReviewModal) IsVisible() bool {
	return m.visible
}

// Update handles messages for the review view.
func (m *MemoryReviewModal) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case ReviewQueueMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.items = msg.Items
		// Keep the cursor stable across refreshes; bubbles/table clamps
		// the cursor inside SetRows when the list shrank.
		m.reviewRows()
		return nil

	case ReviewVerdictMsg:
		if msg.Err != nil {
			m.err = fmt.Errorf("%s %s: %w", msg.Verdict, msg.ClaimID, msg.Err)
			return nil
		}
		m.err = nil
		m.selected = nil
		// Refresh from the daemon: the verdict-ed claim left the queue.
		return m.fetchQueue

	case tea.KeyPressMsg:
		switch msg.String() {
		case "p":
			return m.verdictSelected("promote", m.rpc.PromoteClaim)
		case "r":
			return m.verdictSelected("reject", m.rpc.RejectClaim)
		case KeyEnter:
			if m.selected != nil {
				// Detail already open; esc closes it.
				return nil
			}
			if idx := m.table.Cursor(); idx >= 0 && idx < len(m.items) {
				m.selected = &m.items[idx]
			}
			return nil
		case KeyEsc:
			if m.selected != nil {
				m.selected = nil
				return nil
			}
			m.visible = false
			return nil
		case "up", "down", "k", "j":
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return cmd
		}
	}

	// Pass other messages to the table (home/end/pgup/pgdn…).
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return cmd
}

// verdictSelected issues the verdict RPC for the row under the cursor and
// returns the command delivering the result. Nil when the queue is empty.
func (m *MemoryReviewModal) verdictSelected(verdict string, apply func(id, reason string) error) tea.Cmd {
	if m.selected != nil {
		return nil // detail mode: keys are inert until esc
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.items) {
		return nil
	}
	claim := m.items[idx]
	return func() tea.Msg {
		err := apply(claim.ClaimID, "")
		return ReviewVerdictMsg{ClaimID: claim.ClaimID, Verdict: verdict, Err: err}
	}
}

// View renders the review view.
func (m *MemoryReviewModal) View() string {
	if m.width == 0 {
		return "loading..."
	}

	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(paletteColor("warning")).
		MarginBottom(1)
	b.WriteString(titleStyle.Render(fmt.Sprintf("memory review — %d pending", len(m.items))))
	b.WriteString("\n\n")

	switch {
	case m.loading:
		b.WriteString(lipgloss.NewStyle().
			Foreground(paletteColor("textMuted")).
			Render("loading review queue..."))
	case m.err != nil && len(m.items) == 0:
		b.WriteString(lipgloss.NewStyle().
			Foreground(paletteColor("error")).
			Render(fmt.Sprintf("error: %v", m.err)))
	case len(m.items) == 0:
		b.WriteString(lipgloss.NewStyle().
			Foreground(paletteColor("textMuted")).
			Italic(true).
			Render("no claims pending review"))
	default:
		tableStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(paletteColor("border"))
		b.WriteString(tableStyle.Render(m.table.View()))
		if m.err != nil {
			b.WriteString("\n")
			b.WriteString(lipgloss.NewStyle().
				Foreground(paletteColor("error")).
				Render(fmt.Sprintf("error: %v", m.err)))
		}
		if m.selected != nil {
			b.WriteString("\n\n")
			b.WriteString(m.renderDetail())
		}
	}

	b.WriteString("\n")
	hint := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		MarginTop(1)
	b.WriteString(hint.Render("j/k navigate | enter detail | p promote | r reject | esc close"))

	return b.String()
}

// renderDetail renders the full-claim panel (enter).
func (m *MemoryReviewModal) renderDetail() string {
	item := m.selected

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(paletteColor("warning")).
		Padding(1, 2).
		Width(m.width - 4)

	labelStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Width(12)
	valueStyle := lipgloss.NewStyle().
		Foreground(paletteColor("textPrimary"))

	content := labelStyle.Render("id:") + valueStyle.Render(item.ClaimID) + "\n"
	content += labelStyle.Render("type:") + valueStyle.Render(item.Type) + "\n"
	if item.Category != "" {
		content += labelStyle.Render("category:") + valueStyle.Render(item.Category) + "\n"
	}
	content += labelStyle.Render("confidence:") + valueStyle.Render(fmt.Sprintf("%.2f", item.Confidence)) + "\n"
	content += labelStyle.Render("created:") + valueStyle.Render(item.CreatedAt) + "\n"
	content += "\n" + valueStyle.Render(strings.ReplaceAll(item.Content, "\n", " ")) + "\n"
	content += "\n" + labelStyle.Render("reason:") + valueStyle.Render("(none — pass --reason via the cli to record one)") + "\n"
	content += labelStyle.Render("note:") + valueStyle.Render("verdicts from this panel record no reason; the cli accepts one")

	return panelStyle.Render(content)
}

// SelectedID exposes the id of the claim under the cursor ("" when the
// queue is empty) — used by tests and future callers; the modal keeps
// unexported state otherwise.
func (m *MemoryReviewModal) SelectedID() string {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.items) {
		return ""
	}
	return m.items[idx].ClaimID
}
