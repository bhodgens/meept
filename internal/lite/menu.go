package lite

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Colors are defined in styles.go
// Additional menu-specific colors
var (
	colorAccent     = lipgloss.Color("#F59E0B") // Amber
	colorForeground = lipgloss.Color("#E5E7EB") // Light gray
	colorBackground = lipgloss.Color("#1F2937") // Dark gray
)

// MenuItem represents an item in the menu.
type MenuItem struct {
	Key    string     // Keyboard shortcut (e.g., "1", "s")
	Label  string     // Display text
	Action string     // returned when selected
	Items  []MenuItem // for submenus (2-level max)
}

// DynamicItem represents a dynamically loaded item from RPC data.
type DynamicItem struct {
	Key    string      // Keyboard shortcut (e.g., "1", "2")
	Label  string      // Primary text
	Detail string      // Secondary/detail text
	Action string      // Action string when selected
	Data   interface{} // Raw data for the item
}

// Menu is the Ctrl+X menu system for meept-lite.
type Menu struct {
	visible  bool
	level    int    // 0 = top, 1 = submenu, 2 = dynamic content
	category string // current category key
	selected int    // selected item index
	width    int    // screen width
	height   int    // screen height

	// Menu structure
	categories []MenuItem

	// Dynamic content (level 2)
	dynamicTitle string        // Title for dynamic content
	dynamicItems []DynamicItem // Dynamic items from RPC
	dynamicHint  string        // Hint text for dynamic level
	loading      bool          // Show loading state
	loadingTitle string        // Title while loading

	// Styles
	boxStyle          lipgloss.Style
	titleStyle        lipgloss.Style
	itemStyle         lipgloss.Style
	itemSelectedStyle lipgloss.Style
	keyStyle          lipgloss.Style
	mutedStyle        lipgloss.Style
	separatorStyle    lipgloss.Style
	detailStyle       lipgloss.Style
}

// NewMenu creates a new menu.
func NewMenu() *Menu {
	m := &Menu{
		visible:  false,
		level:    0,
		selected: 0,
	}

	// Initialize styles
	m.boxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 2).
		Background(colorBackground)

	m.titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorAccent).
		MarginBottom(1)

	m.itemStyle = lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(colorForeground)

	m.itemSelectedStyle = lipgloss.NewStyle().
		PaddingLeft(2).
		Background(colorBorder).
		Foreground(colorAccent).
		Bold(true)

	m.keyStyle = lipgloss.NewStyle().
		Foreground(colorMuted).
		Bold(true)

	m.mutedStyle = lipgloss.NewStyle().
		Foreground(colorMuted)

	m.separatorStyle = lipgloss.NewStyle().
		Foreground(colorMuted)

	m.detailStyle = lipgloss.NewStyle().
		Foreground(colorMuted).
		Italic(true)

	// Define menu categories and items as per requirements
	m.categories = []MenuItem{
		{
			Key:    "1",
			Label:  "views",
			Action: "views",
			Items: []MenuItem{
				{Key: "c", Label: "chat", Action: "view:chat"},
				{Key: "t", Label: "tasks", Action: "view:tasks"},
				{Key: "q", Label: "queue", Action: "view:queue"},
				{Key: "m", Label: "memory", Action: "view:memory"},
			},
		},
		{
			Key:    "2",
			Label:  "sessions",
			Action: "sessions",
			Items: []MenuItem{
				{Key: "l", Label: "list", Action: "session:list"},
				{Key: "n", Label: "new", Action: "session:new"},
				{Key: "r", Label: "rename", Action: "session:rename"},
				{Key: "d", Label: "delete", Action: "session:delete"},
			},
		},
		{
			Key:    "3",
			Label:  "agent",
			Action: "agent",
			Items: []MenuItem{
				{Key: "s", Label: "status", Action: "agent:status"},
				{Key: "x", Label: "stop", Action: "agent:stop"},
				{Key: "m", Label: "model", Action: "agent:model"},
			},
		},
		{
			Key:    "4",
			Label:  "tasks",
			Action: "tasks",
			Items: []MenuItem{
				{Key: "l", Label: "list", Action: "task:list"},
				{Key: "c", Label: "create", Action: "task:create"},
				{Key: "x", Label: "cancel", Action: "task:cancel"},
			},
		},
		{
			Key:    "5",
			Label:  "memory",
			Action: "memory",
			Items: []MenuItem{
				{Key: "s", Label: "search", Action: "memory:search"},
				{Key: "r", Label: "recent", Action: "memory:recent"},
				{Key: "c", Label: "clear", Action: "memory:clear"},
			},
		},
		{
			Key:    "6",
			Label:  "config",
			Action: "config",
			Items: []MenuItem{
				{Key: "e", Label: "edit", Action: "config:edit"},
				{Key: "r", Label: "reload", Action: "config:reload"},
			},
		},
		{
			Key:    "?",
			Label:  "help",
			Action: "help",
			Items: []MenuItem{
				{Key: "k", Label: "keybindings", Action: "help:keybindings"},
				{Key: "c", Label: "commands", Action: "help:commands"},
			},
		},
	}

	return m
}

// Show makes the menu visible.
func (m *Menu) Show() {
	m.visible = true
	m.level = 0
	m.category = ""
	m.selected = 0
}

// Hide hides the menu.
func (m *Menu) Hide() {
	m.visible = false
	m.level = 0
	m.category = ""
	m.selected = 0
	m.loading = false
	m.loadingTitle = ""
	m.dynamicItems = nil
	m.dynamicTitle = ""
	m.dynamicHint = ""
}

// SetLoading sets the menu to loading state with a title.
func (m *Menu) SetLoading(title string) {
	m.loading = true
	m.loadingTitle = title
	m.level = 2 // Move to dynamic level
	m.selected = 0
}

// SetDynamicContent sets the dynamic content for the menu (level 2).
func (m *Menu) SetDynamicContent(title string, items []DynamicItem, hint string) {
	m.loading = false
	m.dynamicTitle = title
	m.dynamicItems = items
	m.dynamicHint = hint
	m.level = 2
	m.selected = 0
}

// ClearDynamicContent clears dynamic content and resets to level 0.
func (m *Menu) ClearDynamicContent() {
	m.dynamicItems = nil
	m.dynamicTitle = ""
	m.dynamicHint = ""
	m.loading = false
	m.loadingTitle = ""
	m.level = 0
}

// IsDynamicLevel returns true if the menu is at the dynamic content level.
func (m *Menu) IsDynamicLevel() bool {
	return m.level == 2
}

// IsVisible returns whether the menu is visible.
func (m *Menu) IsVisible() bool {
	return m.visible
}

// SetSize sets the screen dimensions for centering.
func (m *Menu) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// getCurrentItems returns the items for the current menu level (0 or 1).
// For level 2 (dynamic), use getDynamicItems() instead.
func (m *Menu) getCurrentItems() []MenuItem {
	if m.level == 0 {
		return m.categories
	}

	if m.level == 1 {
		// Find the category
		for _, cat := range m.categories {
			if cat.Key == m.category {
				return cat.Items
			}
		}
	}

	return nil
}

// getDynamicItems returns the dynamic items for level 2.
func (m *Menu) getDynamicItems() []DynamicItem {
	if m.level == 2 {
		return m.dynamicItems
	}
	return nil
}

// Update processes key input and returns the action if selected.
// Returns (action, consumed) where action is the selected action string
// and consumed indicates if the key was handled.
func (m *Menu) Update(msg tea.KeyMsg) (action string, consumed bool) {
	if !m.visible {
		return "", false
	}

	key := msg.String()

	// Handle loading state - only escape works
	if m.loading {
		if key == "esc" {
			m.Hide()
			return "", true
		}
		return "", true
	}

	// Handle level 2 (dynamic content)
	if m.level == 2 {
		return m.updateDynamic(key)
	}

	items := m.getCurrentItems()

	// Handle escape - go back or close
	if key == "esc" || key == "q" {
		if m.level == 1 {
			// Go back to top level
			m.level = 0
			m.category = ""
			m.selected = 0
			return "", true
		}
		// Close menu
		m.Hide()
		return "", true
	}

	// Handle navigation
	switch key {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		return "", true

	case "down", "j":
		if m.selected < len(items)-1 {
			m.selected++
		}
		return "", true

	case "left", "h":
		// Go back to top level if in submenu
		if m.level == 1 {
			m.level = 0
			m.category = ""
			m.selected = 0
		}
		return "", true

	case "right", "l", "enter":
		// Select current item or enter submenu
		if m.selected >= 0 && m.selected < len(items) {
			item := items[m.selected]
			if len(item.Items) > 0 {
				// Enter submenu
				m.level = 1
				m.category = item.Key
				m.selected = 0
				return "", true
			}
			// Direct action
			m.Hide()
			return item.Action, true
		}
		return "", true
	}

	// Handle direct key selection
	for i, item := range items {
		if item.Key == key {
			if len(item.Items) > 0 {
				// Enter submenu
				m.level = 1
				m.category = item.Key
				m.selected = 0
				return "", true
			}
			// Direct action
			m.selected = i
			m.Hide()
			return item.Action, true
		}
	}

	// Key not handled but menu is visible, consume it
	return "", true
}

// updateDynamic handles key input for level 2 (dynamic content).
func (m *Menu) updateDynamic(key string) (action string, consumed bool) {
	items := m.dynamicItems

	// Handle escape - go back to level 0 and clear dynamic content
	if key == "esc" || key == "q" {
		m.ClearDynamicContent()
		m.Hide()
		return "", true
	}

	// Handle navigation
	switch key {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		return "", true

	case "down", "j":
		if m.selected < len(items)-1 {
			m.selected++
		}
		return "", true

	case "left", "h":
		// Go back to level 0, clear dynamic
		m.ClearDynamicContent()
		m.Hide()
		return "", true

	case "enter", "right", "l":
		// Select current item
		if m.selected >= 0 && m.selected < len(items) {
			item := items[m.selected]
			if item.Action != "" {
				m.Hide()
				return item.Action, true
			}
		}
		return "", true
	}

	// Handle direct number key selection (1-9)
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		idx := int(key[0] - '1')
		if idx < len(items) {
			item := items[idx]
			if item.Action != "" {
				m.Hide()
				return item.Action, true
			}
		}
		return "", true
	}

	// Key not handled but menu is visible, consume it
	return "", true
}

// View renders the menu as a centered overlay.
func (m *Menu) View() string {
	if !m.visible {
		return ""
	}

	// Handle loading state
	if m.loading {
		return m.renderLoading()
	}

	// Handle level 2 (dynamic content)
	if m.level == 2 {
		return m.renderDynamic()
	}

	var b strings.Builder
	items := m.getCurrentItems()

	// Determine menu width based on content
	menuWidth := 40
	if m.level == 1 {
		menuWidth = 30
	}

	// Title
	title := "menu"
	if m.level == 1 {
		// Find category label
		for _, cat := range m.categories {
			if cat.Key == m.category {
				title = cat.Label
				break
			}
		}
	}
	b.WriteString(m.titleStyle.Width(menuWidth - 4).Render(title))
	b.WriteString("\n")

	// Separator
	b.WriteString(m.separatorStyle.Render(strings.Repeat("─", menuWidth-4)))
	b.WriteString("\n")

	// Items
	for i, item := range items {
		style := m.itemStyle
		if i == m.selected {
			style = m.itemSelectedStyle
		}

		// Format: [key]  label  (with submenu indicator)
		keyPart := m.keyStyle.Render("[" + item.Key + "]")
		labelPart := item.Label

		// Add submenu indicator if has items
		if len(item.Items) > 0 {
			labelPart += " >"
		}

		// Build line with proper width
		line := keyPart + "  " + style.Render(labelPart)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Footer hint
	b.WriteString("\n")
	var hint string
	if m.level == 0 {
		hint = "press key or esc to close"
	} else {
		hint = "press key, esc/h to go back"
	}
	hintStyle := m.mutedStyle.Align(lipgloss.Center).Width(menuWidth - 4)
	b.WriteString(hintStyle.Render(hint))

	// Render the box
	content := m.boxStyle.Width(menuWidth).Render(b.String())

	// Center on screen
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderLoading renders the loading state.
func (m *Menu) renderLoading() string {
	var b strings.Builder
	menuWidth := 40

	title := m.loadingTitle
	if title == "" {
		title = "loading"
	}
	b.WriteString(m.titleStyle.Width(menuWidth - 4).Render(title))
	b.WriteString("\n")

	// Separator
	b.WriteString(m.separatorStyle.Render(strings.Repeat("─", menuWidth-4)))
	b.WriteString("\n\n")

	// Loading message
	loadingStyle := m.mutedStyle.Align(lipgloss.Center).Width(menuWidth - 4)
	b.WriteString(loadingStyle.Render("loading..."))
	b.WriteString("\n\n")

	// Hint
	hintStyle := m.mutedStyle.Align(lipgloss.Center).Width(menuWidth - 4)
	b.WriteString(hintStyle.Render("esc to cancel"))

	content := m.boxStyle.Width(menuWidth).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// renderDynamic renders level 2 dynamic content.
func (m *Menu) renderDynamic() string {
	var b strings.Builder
	items := m.dynamicItems

	// Determine menu width - wider for dynamic content
	menuWidth := 50

	// Title
	title := m.dynamicTitle
	if title == "" {
		title = "results"
	}
	b.WriteString(m.titleStyle.Width(menuWidth - 4).Render(title))
	b.WriteString("\n")

	// Separator
	b.WriteString(m.separatorStyle.Render(strings.Repeat("─", menuWidth-4)))
	b.WriteString("\n")

	// Items (or empty message)
	if len(items) == 0 {
		emptyStyle := m.mutedStyle.Align(lipgloss.Center).Width(menuWidth - 4)
		b.WriteString("\n")
		b.WriteString(emptyStyle.Render("(no items)"))
		b.WriteString("\n")
	} else {
		// Show up to 9 items (for 1-9 key selection)
		maxItems := 9
		if len(items) < maxItems {
			maxItems = len(items)
		}

		for i := 0; i < maxItems; i++ {
			item := items[i]
			style := m.itemStyle
			if i == m.selected {
				style = m.itemSelectedStyle
			}

			// Format: [key]  label  detail
			keyPart := m.keyStyle.Render("[" + item.Key + "]")
			labelPart := item.Label

			var line string
			if item.Detail != "" {
				detailPart := m.detailStyle.Render(item.Detail)
				line = keyPart + "  " + style.Render(labelPart) + "  " + detailPart
			} else {
				line = keyPart + "  " + style.Render(labelPart)
			}

			b.WriteString(line)
			b.WriteString("\n")
		}

		// Show "... and N more" if truncated
		if len(items) > 9 {
			moreStyle := m.mutedStyle.PaddingLeft(6)
			b.WriteString(moreStyle.Render("... and " + strings.TrimSpace(strings.Repeat(" ", 0)) + itoa(len(items)-9) + " more"))
			b.WriteString("\n")
		}
	}

	// Footer hint
	b.WriteString("\n")
	hint := m.dynamicHint
	if hint == "" {
		hint = "esc to go back"
	}
	hintStyle := m.mutedStyle.Align(lipgloss.Center).Width(menuWidth - 4)
	b.WriteString(hintStyle.Render(hint))

	content := m.boxStyle.Width(menuWidth).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// itoa converts an int to string (simple helper to avoid strconv import).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ShouldOpenMenu checks if a key event should open the menu.
// Ctrl+X always opens, "/" opens only at position 0 (start of line).
func ShouldOpenMenu(msg tea.KeyMsg, cursorPos int) bool {
	key := msg.String()

	// Ctrl+X always opens menu
	if key == "ctrl+x" {
		return true
	}

	// "/" opens menu only at start of line
	if key == "/" && cursorPos == 0 {
		return true
	}

	return false
}

// MenuAction represents action constants for menu items.
// These can be used by the caller to handle menu actions.
const (
	// View actions
	ActionViewChat   = "view:chat"
	ActionViewTasks  = "view:tasks"
	ActionViewQueue  = "view:queue"
	ActionViewMemory = "view:memory"

	// Session actions
	ActionSessionList   = "session:list"
	ActionSessionNew    = "session:new"
	ActionSessionRename = "session:rename"
	ActionSessionDelete = "session:delete"

	// Agent actions
	ActionAgentStatus = "agent:status"
	ActionAgentStop   = "agent:stop"
	ActionAgentModel  = "agent:model"

	// Task actions
	ActionTaskList   = "task:list"
	ActionTaskCreate = "task:create"
	ActionTaskCancel = "task:cancel"

	// Memory actions
	ActionMemorySearch = "memory:search"
	ActionMemoryRecent = "memory:recent"
	ActionMemoryClear  = "memory:clear"

	// Config actions
	ActionConfigEdit   = "config:edit"
	ActionConfigReload = "config:reload"

	// Help actions
	ActionHelpKeybindings = "help:keybindings"
	ActionHelpCommands    = "help:commands"
)
