// Package prompts provides a read-only Bubble Tea list component for browsing
// the 4-tier prompt template hierarchy.
//
// The primary interface is the /prompts slash command (see
// internal/tui/command_handler.go). This component is provided for embedding
// in a sidebar or panel view (e.g., a future Flutter-style settings tab).
package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// PromptEntry describes one discoverable template.
type PromptEntry struct {
	Name   string
	Tier   string
	Source string
}

// promptListItem implements list.Item.
type promptListItem struct {
	entry PromptEntry
}

func (i promptListItem) Title() string { return i.entry.Name }
func (i promptListItem) Description() string {
	return fmt.Sprintf("[%s] %s", i.entry.Tier, i.entry.Source)
}
func (i promptListItem) FilterValue() string { return i.entry.Name }

// Model is the Bubble Tea model for the prompt template browser.
type Model struct {
	list     list.Model
	detail   *PromptDetail
	entries  []PromptEntry
	width    int
	height   int
	err      error
}

// PromptDetail holds the full content for a selected template.
type PromptDetail struct {
	Entry   PromptEntry
	Content string
}

// New creates a new prompts model with default size.
func New() *Model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#3B82F6")).
		Bold(true)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("#E5E7EB")).
		Background(lipgloss.Color("#3B82F6"))

	l := list.New([]list.Item{}, delegate, 60, 15)
	l.Title = "prompt templates"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)

	m := &Model{list: l}
	m.Load()
	return m
}

// Load discovers templates from the 4-tier hierarchy.
func (m *Model) Load() {
	m.entries = Discover()
	items := make([]list.Item, len(m.entries))
	for i, e := range m.entries {
		items[i] = promptListItem{entry: e}
	}
	m.list.SetItems(items)
}

// SetSize updates the model dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	if m.detail != nil {
		// Detail view uses full width; list hidden.
		return
	}
	m.list.SetSize(width, height)
}

// Init starts the model.
func (m *Model) Init() tea.Cmd { return nil }

// Update handles messages.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			if m.detail != nil {
				m.detail = nil
				m.SetSize(m.width, m.height)
				return nil
			}
			if item, ok := m.list.SelectedItem().(promptListItem); ok {
				content, err := os.ReadFile(item.entry.Source)
				if err != nil {
					m.err = err
					return nil
				}
				m.detail = &PromptDetail{Entry: item.entry, Content: string(content)}
				return nil
			}
		case "esc":
			if m.detail != nil {
				m.detail = nil
				m.SetSize(m.width, m.height)
				return nil
			}
		case "q":
			if m.detail != nil {
				m.detail = nil
				m.SetSize(m.width, m.height)
				return nil
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return cmd
}

// View renders the current state.
func (m *Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\n", m.err)
	}
	if m.detail != nil {
		var sb strings.Builder
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3B82F6"))
		fmt.Fprintf(&sb, "%s\n\n", headerStyle.Render("prompt: "+m.detail.Entry.Name))
		fmt.Fprintf(&sb, "source: %s (tier: %s)\n\n", m.detail.Entry.Source, m.detail.Entry.Tier)
		sb.WriteString(m.detail.Content)
		sb.WriteString("\n\n[enter/esc/q to go back]")
		return sb.String()
	}
	if len(m.entries) == 0 {
		return "no prompt templates found\n"
	}
	return m.list.View()
}

// Discover walks the 4-tier hierarchy and returns all templates, de-duplicated
// by name (highest-priority tier wins).
func Discover() []PromptEntry {
	home, _ := os.UserHomeDir()
	tiers := []struct {
		label string
		dir   string
	}{
		{"project", ".meept/prompts"},
		{"user", filepath.Join(home, ".meept", "prompts")},
		{"system", filepath.Join(home, ".config", "meept", "prompts")},
		{"bundled", "config/prompts"},
	}
	seen := make(map[string]PromptEntry)
	for _, tier := range tiers {
		_ = filepath.Walk(tier.dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(path), ".md") {
				return nil
			}
			rel, _ := filepath.Rel(tier.dir, path)
			rel = filepath.ToSlash(rel)
			if _, ok := seen[rel]; ok {
				return nil
			}
			seen[rel] = PromptEntry{Name: rel, Tier: tier.label, Source: path}
			return nil
		})
	}
	result := make([]PromptEntry, 0, len(seen))
	for _, v := range seen {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Validate checks that all discovered templates parse as text/template after
// frontmatter stripping. Returns a list of errors keyed by template name.
func Validate() []ValidationError {
	entries := Discover()
	var errs []ValidationError
	for _, e := range entries {
		content, err := os.ReadFile(e.Source)
		if err != nil {
			errs = append(errs, ValidationError{Name: e.Name, Err: err})
			continue
		}
		body := stripFrontmatter(string(content))
		if strings.TrimSpace(body) == "" {
			errs = append(errs, ValidationError{Name: e.Name, Err: fmt.Errorf("empty template")})
			continue
		}
		if _, err := template.New("validate").Parse(body); err != nil {
			errs = append(errs, ValidationError{Name: e.Name, Err: err})
		}
	}
	return errs
}

// ValidationError pairs a template name with a parse error.
type ValidationError struct {
	Name string
	Err  error
}

// stripFrontmatter removes YAML frontmatter from body.
func stripFrontmatter(body string) string {
	const marker = "---"
	if !strings.HasPrefix(body, marker+"\n") && !strings.HasPrefix(body, marker+"\r\n") {
		return body
	}
	rest := body[len(marker):]
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else {
		rest = rest[1:]
	}
	searches := []string{
		"\n" + marker + "\n",
		"\r\n" + marker + "\r\n",
		"\n" + marker + "\r\n",
		"\r\n" + marker + "\n",
	}
	for _, s := range searches {
		if idx := strings.Index(rest, s); idx >= 0 {
			return rest[idx+len(s):]
		}
	}
	if strings.HasSuffix(rest, "\n"+marker) {
		return ""
	}
	if strings.HasSuffix(rest, "\r\n"+marker) {
		return ""
	}
	return body
}
