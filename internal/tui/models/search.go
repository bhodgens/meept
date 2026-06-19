package models

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// searchDebounceDuration is how long to wait after the last keystroke before
// firing the semantic search RPC. Mirrors the find-bar debounce pattern in
// chat.go but with a longer interval to amortize the embedding cost.
const searchDebounceDuration = 250 * time.Millisecond

// searchDefaultLimit is the max number of results requested per query.
const searchDefaultLimit = 20

// OpenSearchViewMsg requests the app to switch to the global search view.
type OpenSearchViewMsg struct{}

// CloseSearchViewMsg requests the app to switch back to the sessions view.
type CloseSearchViewMsg struct{}

// NavigateToSessionMsg requests the app to open a session, optionally at a
// specific message. MessageID == 0 means no specific message.
type NavigateToSessionMsg struct {
	SessionID string
	MessageID int64
}

// searchDebounceMsg is emitted by tea.Tick after the user pauses typing in
// the search input. Mirrors findDebounceMsg in chat.go.
type searchDebounceMsg struct{}

// searchResultsMsg delivers search results to the model.
type searchResultsMsg struct {
	results []SearchResultView
	mode    string
	err     error
}

// SearchResultView is a UI-friendly search result. It mirrors
// services.SearchResult but lives in the TUI layer to avoid a dependency
// on the services package.
type SearchResultView struct {
	Type      string  `json:"type"` // "session", "task", "memory", "plan", "message"
	ID        string  `json:"id"`   // for messages: "sessionID:msgID"
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet"`
	Relevance float64 `json:"relevance"`
}

// SemanticSearchRPCResponse mirrors services.SemanticSearchResponse for JSON
// unmarshalling of the RPC result.
type SemanticSearchRPCResponse struct {
	Results []SearchResultView `json:"results"`
	Mode    string             `json:"mode"`
	Err     string             `json:"err,omitempty"`
}

// SearchRPCClient is the minimal interface for semantic search RPC calls.
type SearchRPCClient interface {
	SearchSemantic(query, scope string, limit int) (*SemanticSearchRPCResponse, error)
}

// SearchModel is the model for the global search view. It owns a text input
// for the query, a cursor-selectable result list, and a scope selector that
// cycles through "all", "messages", "memories", "tasks", "plans".
type SearchModel struct {
	rpc             SearchRPCClient
	query           textinput.Model
	results         []SearchResultView
	mode            string
	loading         bool
	err             error
	cursor          int
	width           int
	height          int
	scopeIndex      int
	scopes          []string
	debouncePending bool
	logger          *slog.Logger
}

// NewSearchModel creates a new search model. The rpc parameter may be nil —
// the view will render "search unavailable" instead of crashing.
func NewSearchModel(rpc SearchRPCClient, logger *slog.Logger) *SearchModel {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.Focus()
	ti.CharLimit = 256
	ti.SetWidth(60)

	if logger == nil {
		logger = slog.Default()
	}

	return &SearchModel{
		rpc:    rpc,
		query:  ti,
		scopes: []string{"all", "messages", "memories", "tasks", "plans"},
		logger: logger,
	}
}

// SetSize updates the model dimensions.
func (m *SearchModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	inputWidth := max(width-20, 20)
	m.query.SetWidth(inputWidth)
}

// Init initializes the search model.
func (m *SearchModel) Init() tea.Cmd {
	return m.query.Focus()
}

// Update handles messages for the search view.
func (m *SearchModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case searchDebounceMsg:
		// Only fire the search if a debounce is still pending. A newer
		// keystroke may have re-armed the debounce; in that case this tick
		// is stale and we ignore it.
		if m.debouncePending {
			m.debouncePending = false
			return m.fireSearch()
		}
		return nil

	case searchResultsMsg:
		m.loading = false
		m.err = msg.err
		m.results = msg.results
		m.mode = msg.mode
		if m.cursor >= len(m.results) {
			m.cursor = max(len(m.results)-1, 0)
		}
		return nil

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case KeyEsc:
			m.resetQuery()
			return func() tea.Msg { return CloseSearchViewMsg{} }

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return nil

		case "down", "j":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
			return nil

		case KeyTab:
			m.scopeIndex = (m.scopeIndex + 1) % len(m.scopes)
			m.cursor = 0
			// Re-search immediately if there's a query.
			if strings.TrimSpace(m.query.Value()) != "" {
				return m.fireSearch()
			}
			return nil

		case KeyEnter:
			return m.handleEnter()

		default:
			// Forward to text input. Detect text change to schedule debounce.
			prevVal := m.query.Value()
			var cmd tea.Cmd
			m.query, cmd = m.query.Update(msg)
			if m.query.Value() != prevVal {
				m.debouncePending = true
				return tea.Batch(cmd, tea.Tick(searchDebounceDuration, func(time.Time) tea.Msg {
					return searchDebounceMsg{}
				}))
			}
			return cmd
		}
	}

	return nil
}

// handleEnter dispatches based on the selected result type. For messages,
// it emits a NavigateToSessionMsg. For other types, it logs a debug message
// (view-switching for non-message types is MVP-deferred).
func (m *SearchModel) handleEnter() tea.Cmd {
	if len(m.results) == 0 || m.cursor < 0 || m.cursor >= len(m.results) {
		return nil
	}
	r := m.results[m.cursor]

	switch r.Type {
	case "message", "session":
		sessionID, msgID := parseMessageID(r.ID)
		if sessionID == "" {
			m.logger.Debug("search enter: could not parse message ID", "id", r.ID, "type", r.Type)
			return nil
		}
		return func() tea.Msg {
			return NavigateToSessionMsg{SessionID: sessionID, MessageID: msgID}
		}
	default:
		m.logger.Debug("search enter: navigation not implemented for type", "type", r.Type, "id", r.ID)
		return nil
	}
}

// parseMessageID parses an ID of the form "sessionID:msgID" (as produced by
// the backend for message results). For session-type results where the ID is
// just the session ID, it returns the sessionID with msgID=0.
func parseMessageID(id string) (string, int64) {
	if id == "" {
		return "", 0
	}
	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 1 {
		return parts[0], 0
	}
	msgID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return parts[0], 0
	}
	return parts[0], msgID
}

// fireSearch executes the semantic search RPC asynchronously.
func (m *SearchModel) fireSearch() tea.Cmd {
	q := strings.TrimSpace(m.query.Value())
	if q == "" {
		m.results = nil
		m.err = nil
		return nil
	}
	if m.rpc == nil {
		m.err = fmt.Errorf("search unavailable")
		return nil
	}
	scope := m.scopes[m.scopeIndex]
	m.loading = true

	rpc := m.rpc
	return func() tea.Msg {
		resp, err := rpc.SearchSemantic(q, scope, searchDefaultLimit)
		if err != nil {
			return searchResultsMsg{err: err}
		}
		if resp.Err != "" {
			return searchResultsMsg{err: fmt.Errorf("%s", resp.Err)}
		}
		return searchResultsMsg{results: resp.Results, mode: resp.Mode}
	}
}

func (m *SearchModel) resetQuery() {
	m.query.SetValue("")
	m.results = nil
	m.err = nil
	m.mode = ""
	m.cursor = 0
	m.debouncePending = false
	m.loading = false
}

// View renders the search view.
func (m *SearchModel) View() string {
	if m.rpc == nil {
		return m.renderUnavailable()
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Results area
	b.WriteString(m.renderResults())
	b.WriteString("\n")

	// Footer hints
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m *SearchModel) renderUnavailable() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorRed)).
		Padding(1, 2).
		Width(max(m.width-4, 40))

	return style.Render(
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true).Render("search unavailable") +
			"\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray)).Render("press esc to go back"),
	)
}

func (m *SearchModel) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316"))

	scopeLabel := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray))

	scopeValue := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB")).
		Bold(true)

	modeLabel := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray))

	modeValue := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber))

	currentScope := m.scopes[m.scopeIndex]
	mode := m.mode
	if mode == "" {
		mode = "—"
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("search"))
	b.WriteString("  ")
	b.WriteString(m.query.View())
	b.WriteString("  ")
	b.WriteString(scopeLabel.Render("scope:"))
	b.WriteString(scopeValue.Render(fmt.Sprintf("[%s]", currentScope)))
	b.WriteString("  ")
	b.WriteString(modeLabel.Render("mode:"))
	b.WriteString(modeValue.Render(mode))

	return b.String()
}

func (m *SearchModel) renderResults() string {
	dividerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151"))

	resultArea := lipgloss.NewStyle().
		Width(max(m.width, 40))

	var b strings.Builder

	b.WriteString(dividerStyle.Render(strings.Repeat("─", max(m.width-2, 20))))
	b.WriteString("\n")

	if m.loading {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorGray)).
			Italic(true).
			Render("searching..."))
		return resultArea.Render(b.String())
	}

	if m.err != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed))
		b.WriteString(errStyle.Render(fmt.Sprintf("error: %v", m.err)))
		return resultArea.Render(b.String())
	}

	if len(m.results) == 0 {
		if strings.TrimSpace(m.query.Value()) == "" {
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorGray)).
				Italic(true).
				Render("type to search across sessions, memories, tasks, and plans"))
		} else {
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorGray)).
				Italic(true).
				Render("no results"))
		}
		return resultArea.Render(b.String())
	}

	typeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F97316")).
		Bold(true).
		Width(10)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	relevanceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAmber))

	snippetStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		Italic(true).
		PaddingLeft(12)

	cursorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F97316")).
		Bold(true)

	for i, r := range m.results {
		marker := "  "
		if i == m.cursor {
			marker = cursorStyle.Render("▸ ")
		}

		snippet := r.Snippet
		if len(snippet) > 120 {
			snippet = snippet[:117] + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")

		b.WriteString(marker)
		b.WriteString(typeStyle.Render(fmt.Sprintf("[%s]", r.Type)))
		b.WriteString(" ")
		b.WriteString(titleStyle.Render(r.Title))
		b.WriteString(" ")
		b.WriteString(relevanceStyle.Render(fmt.Sprintf("(%.2f)", r.Relevance)))
		b.WriteString("\n")
		b.WriteString(snippetStyle.Render(snippet))
		b.WriteString("\n")
	}

	return resultArea.Render(b.String())
}

func (m *SearchModel) renderFooter() string {
	dividerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151"))

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorGray)).
		MarginTop(0)

	var b strings.Builder
	b.WriteString(dividerStyle.Render(strings.Repeat("─", max(m.width-2, 20))))
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("enter: open | esc: back | up/down: navigate | tab: scope"))
	return b.String()
}
