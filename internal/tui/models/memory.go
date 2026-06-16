package models

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// MemoryModel is the model for the memory browser view.
type MemoryModel struct {
	rpc           MemoryRPCClient
	searchInput   textinput.Model
	results       []types.MemoryItem
	list          list.Model
	selectedItem  *types.MemoryItem
	width         int
	height        int
	loading       bool
	err           error
	focusedSearch bool
}

// MemoryRPCClient interface for the memory model.
type MemoryRPCClient interface {
	QueryMemory(query string, limit int) (*types.MemoryQueryResponse, error)
	IsConnected() bool
}

// memoryListItem implements list.Item for memory items.
type memoryListItem struct {
	item types.MemoryItem
}

func (i memoryListItem) Title() string {
	content := i.item.Content
	if len(content) > 60 {
		content = content[:57] + "..."
	}
	// Replace newlines with spaces
	content = strings.ReplaceAll(content, "\n", " ")
	return content
}

func (i memoryListItem) Description() string {
	memType := i.item.GetType()
	score := i.item.RelevanceScore
	return fmt.Sprintf("[%s] score: %.2f", memType, score)
}

func (i memoryListItem) FilterValue() string {
	return i.item.Content
}

// NewMemoryModel creates a new memory model.
func NewMemoryModel(rpc MemoryRPCClient) *MemoryModel {
	// Search input
	ti := textinput.New()
	ti.Placeholder = "search memory..."
	ti.Focus()
	ti.CharLimit = 256
	ti.SetWidth(50)

	// List
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#F97316")).
		Bold(true)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("#E5E7EB")).
		Background(lipgloss.Color("#F97316"))

	l := list.New([]list.Item{}, delegate, 40, 10)
	l.Title = "search results"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316"))

	return &MemoryModel{
		rpc:           rpc,
		searchInput:   ti,
		list:          l,
		focusedSearch: true,
	}
}

// SetSize updates the model dimensions.
func (m *MemoryModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Update search input width
	m.searchInput.SetWidth(width - 10)

	// Update list dimensions
	listHeight := max(
		// Account for search, detail panel, etc.
		height-16, 5)
	m.list.SetSize(width/2-4, listHeight)
}

// MemoryQueryMsg carries the memory query result.
type MemoryQueryMsg struct {
	Items []types.MemoryItem
	Err   error
}

// Init initializes the memory model.
func (m *MemoryModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *MemoryModel) queryMemory(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.rpc.QueryMemory(query, 25)
		if err != nil {
			return MemoryQueryMsg{Err: err}
		}
		return MemoryQueryMsg{Items: resp.GetItems()}
	}
}

// Update handles messages for the memory view.
func (m *MemoryModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case MemoryQueryMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
			return nil
		}
		m.err = nil
		m.results = msg.Items
		m.updateList()
		return nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case KeyEnter:
			if m.focusedSearch {
				query := strings.TrimSpace(m.searchInput.Value())
				if query != "" {
					m.loading = true
					m.selectedItem = nil
					return m.queryMemory(query)
				}
			} else {
				// Select item from list
				if item, ok := m.list.SelectedItem().(memoryListItem); ok {
					m.selectedItem = &item.item
				}
			}
			return nil

		case "tab":
			// Toggle focus between search and list
			m.focusedSearch = !m.focusedSearch
			if m.focusedSearch {
				m.searchInput.Focus()
			} else {
				m.searchInput.Blur()
			}
			return nil

		case "/":
			// Focus search
			m.focusedSearch = true
			m.searchInput.Focus()
			return nil

		case KeyEsc:
			if m.selectedItem != nil {
				m.selectedItem = nil
			} else if !m.focusedSearch {
				m.focusedSearch = true
				m.searchInput.Focus()
			}
			return nil
		}
	}

	// Update appropriate component based on focus
	var cmd tea.Cmd
	if m.focusedSearch {
		m.searchInput, cmd = m.searchInput.Update(msg)
	} else {
		m.list, cmd = m.list.Update(msg)
		// Update selected item as we navigate
		if item, ok := m.list.SelectedItem().(memoryListItem); ok {
			m.selectedItem = &item.item
		}
	}

	return cmd
}

func (m *MemoryModel) updateList() {
	items := make([]list.Item, len(m.results))
	for i, item := range m.results {
		items[i] = memoryListItem{item: item}
	}
	m.list.SetItems(items)
}

// View renders the memory view.
func (m *MemoryModel) View() string {
	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316")).
		MarginBottom(1)

	b.WriteString(titleStyle.Render("memory browser"))
	b.WriteString("\n\n")

	// Search input
	searchStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(0, 1).
		Width(m.width - 4)

	if m.focusedSearch {
		searchStyle = searchStyle.BorderForeground(lipgloss.Color("#F97316"))
	}

	b.WriteString(searchStyle.Render(m.searchInput.View()))
	b.WriteString("\n\n")

	switch {
	case m.loading:
		b.WriteString(m.renderLoading())
	case m.err != nil:
		b.WriteString(m.renderError())
	case len(m.results) == 0:
		b.WriteString(m.renderEmpty())
	default:
		// Two-column layout: list on left, detail on right
		listWidth := m.width / 2
		detailWidth := m.width - listWidth - 4

		listView := m.renderList(listWidth)
		detailView := m.renderDetail(detailWidth)

		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, listView, detailView))
	}

	// Help hint
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		MarginTop(1)

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("/ focus search | tab switch focus | enter select"))

	return b.String()
}

func (m *MemoryModel) renderLoading() string {
	style := lipgloss.NewStyle().
		Width(m.width-4).
		Align(lipgloss.Center).
		Padding(2, 0).
		Foreground(lipgloss.Color("#6B7280"))

	return style.Render("searching...")
}

func (m *MemoryModel) renderError() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#EF4444")).
		Padding(1, 2).
		Width(m.width - 4)

	return style.Render(
		lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true).Render("search error") +
			"\n\n" +
			fmt.Sprintf("%v", m.err),
	)
}

func (m *MemoryModel) renderEmpty() string {
	style := lipgloss.NewStyle().
		Width(m.width-4).
		Align(lipgloss.Center).
		Padding(2, 0).
		Foreground(lipgloss.Color("#6B7280")).
		Italic(true)

	return style.Render("no results. enter a search query and press enter.")
}

func (m *MemoryModel) renderList(width int) string {
	listStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Width(width - 2)

	if !m.focusedSearch {
		listStyle = listStyle.BorderForeground(lipgloss.Color("#F97316"))
	}

	return listStyle.Render(m.list.View())
}

func (m *MemoryModel) renderDetail(width int) string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 2).
		Width(width).
		Height(m.height - 10)

	if m.selectedItem == nil {
		content := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true).
			Render("select an item to view details")
		return panelStyle.Render(content)
	}

	item := m.selectedItem

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F97316"))

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Width(12)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	// Type color
	memType := item.GetType()
	typeColor := "#E5E7EB"
	switch strings.ToLower(memType) {
	case "episodic":
		typeColor = "#06B6D4" // Cyan
	case "task":
		typeColor = ColorAmber // Amber
	case "personality":
		typeColor = "#A855F7" // Purple
	}
	typeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(typeColor)).
		Bold(true)

	content := titleStyle.Render("memory detail") + "\n\n"
	content += labelStyle.Render("id:") + valueStyle.Render(item.ID) + "\n"
	content += labelStyle.Render("type:") + typeStyle.Render(memType) + "\n"

	if item.Category != "" {
		content += labelStyle.Render("category:") + valueStyle.Render(item.Category) + "\n"
	}

	content += labelStyle.Render("relevance:") + valueStyle.Render(fmt.Sprintf("%.2f", item.RelevanceScore)) + "\n"

	if item.CreatedAt != "" {
		content += labelStyle.Render("created:") + valueStyle.Render(item.CreatedAt) + "\n"
	}

	if item.UpdatedAt != "" {
		content += labelStyle.Render("updated:") + valueStyle.Render(item.UpdatedAt) + "\n"
	}

	content += "\n"
	content += lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F97316")).Render("content:") + "\n\n"

	// Word-wrap content
	contentText := item.Content
	maxWidth := width - 8
	if maxWidth > 0 && contentText != "" {
		contentText = wrapText(contentText, maxWidth)
	}
	content += valueStyle.Render(contentText)

	return panelStyle.Render(content)
}

// wrapText wraps text to fit within the given width.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var lines []string
	paragraphs := strings.SplitSeq(text, "\n")

	for para := range paragraphs {
		if len(para) <= width {
			lines = append(lines, para)
			continue
		}

		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		currentLine := words[0]
		for _, word := range words[1:] {
			if len(currentLine)+1+len(word) <= width {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		lines = append(lines, currentLine)
	}

	return strings.Join(lines, "\n")
}
