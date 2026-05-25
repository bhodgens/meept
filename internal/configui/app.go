// internal/configui/app.go
package configui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Phase represents the current screen state of the config UI.
type Phase int

const (
	PhaseMenu    Phase = iota // main menu
	PhaseSection              // editing a section's fields
	PhaseEditor               // inline field editor
	PhaseDrilldown            // drill-down into nested struct
	PhaseConfirmSave          // save confirmation prompt
	PhaseQuitting             // exiting
)

// MenuItem represents a selectable section in the main menu.
type MenuItem struct {
	Title       string
	Description string
	KeyPath     string
	ConfigFile  string // which config file this section writes to
}

// App is the root bubbletea model for the config editor.
type App struct {
	phase         Phase
	menuItems     []MenuItem
	allItems      []MenuItem // includes advanced
	primaryItems  []MenuItem
	showAdvanced  bool
	menuCursor    int
	section       *SectionModel
	editor        *FieldEditor
	width, height int
	styles        styles
	errMsg        string
}

type styles struct {
	title       lipgloss.Style
	selected    lipgloss.Style
	unselected  lipgloss.Style
	label       lipgloss.Style
	value       lipgloss.Style
	dirtyMarker lipgloss.Style
	help        lipgloss.Style
	breadcrumb  lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")),
		selected:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")),
		unselected:  lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
		label:       lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA")),
		value:       lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")),
		dirtyMarker: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B")),
		help:        lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")),
		breadcrumb:  lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")),
	}
}

// NewApp creates the config editor app.
func NewApp() *App {
	primary := []MenuItem{
		{Title: "daemon", Description: "socket path, PID file, log level, data dir", KeyPath: "daemon", ConfigFile: "meept.json5"},
		{Title: "transport", Description: "RPC/HTTP toggles, addresses, endpoints", KeyPath: "transport", ConfigFile: "meept.json5"},
		{Title: "llm", Description: "budget, broker, adaptive timeout, context firewall, cache", KeyPath: "llm", ConfigFile: "meept.json5"},
		{Title: "models", Description: "default model, providers, credentials, runtime", KeyPath: "models", ConfigFile: "models.json5"},
		{Title: "agents", Description: "agent definitions, tools, prompts", KeyPath: "agents", ConfigFile: "agents.json5"},
		{Title: "memory", Description: "backend, episodic/task/personality, embeddings, limits", KeyPath: "memory", ConfigFile: "meept.json5"},
		{Title: "security", Description: "sanitization, path restrictions, tirith, audit", KeyPath: "security", ConfigFile: "meept.json5"},
		{Title: "mcp servers", Description: "MCP server definitions (stdio/http)", KeyPath: "mcp_servers", ConfigFile: "mcp_servers.json5"},
		{Title: "client / tui", Description: "connection, keybindings, vim, rendering, chat", KeyPath: "client", ConfigFile: "client.json5"},
		{Title: "scheduler", Description: "timezone", KeyPath: "scheduler", ConfigFile: "meept.json5"},
	}

	advanced := []MenuItem{
		{Title: "multiagent", Description: "dispatcher/classifier models, memory refs", KeyPath: "multiagent", ConfigFile: "meept.json5"},
		{Title: "agent loop", Description: "progress, cache, errors, review, validation, watchdog, queues", KeyPath: "agent", ConfigFile: "meept.json5"},
		{Title: "queue", Description: "db path, max retries", KeyPath: "queue", ConfigFile: "meept.json5"},
		{Title: "workers", Description: "pool size, idle timeout, capabilities", KeyPath: "workers", ConfigFile: "meept.json5"},
		{Title: "isolation", Description: "sandbox dir, auto git init", KeyPath: "isolation", ConfigFile: "meept.json5"},
		{Title: "workspace", Description: "base dir, auto commit settings", KeyPath: "workspace", ConfigFile: "meept.json5"},
		{Title: "skills", Description: "search paths, auto reload", KeyPath: "skills", ConfigFile: "meept.json5"},
		{Title: "orchestrator", Description: "max plan steps, timeouts", KeyPath: "orchestrator", ConfigFile: "meept.json5"},
		{Title: "compaction", Description: "context compaction model, tokens, ratios", KeyPath: "compaction", ConfigFile: "meept.json5"},
		{Title: "session", Description: "persistence, branching, auto fork", KeyPath: "session", ConfigFile: "meept.json5"},
		{Title: "code intel", Description: "AST cache, LSP servers", KeyPath: "code_intel", ConfigFile: "meept.json5"},
		{Title: "telegram", Description: "bot token, allowed users", KeyPath: "telegram", ConfigFile: "meept.json5"},
		{Title: "web", Description: "host, port, secret key", KeyPath: "web", ConfigFile: "meept.json5"},
		{Title: "mcp toggle", Description: "MCP enabled, config file path", KeyPath: "mcp", ConfigFile: "meept.json5"},
		{Title: "plugins", Description: "enabled, directory", KeyPath: "plugins", ConfigFile: "meept.json5"},
		{Title: "self-improve", Description: "AI infra, sandbox, safety, detection", KeyPath: "selfimprove", ConfigFile: "meept.json5"},
		{Title: "shadow", Description: "shadowing, teacher, quality, adapters", KeyPath: "shadow", ConfigFile: "meept.json5"},
		{Title: "distributed memory", Description: "mode, sync, distillation", KeyPath: "distributed_memory", ConfigFile: "meept.json5"},
		{Title: "q agent", Description: "thresholds, notifications, analysis", KeyPath: "q_agent", ConfigFile: "meept.json5"},
		{Title: "tooling", Description: "sidecar agent config", KeyPath: "tooling", ConfigFile: "meept.json5"},
		{Title: "calendar", Description: "Google OAuth, reminders", KeyPath: "calendar", ConfigFile: "meept.json5"},
		{Title: "memvid", Description: "endpoint, data dir, timeout", KeyPath: "memvid", ConfigFile: "meept.json5"},
		{Title: "presets", Description: "temperature/preset profiles", KeyPath: "presets", ConfigFile: "presets.json5"},
	}

	all := append(primary, advanced...)

	return &App{
		phase:         PhaseMenu,
		primaryItems:  primary,
		allItems:      all,
		menuItems:     primary,
		showAdvanced:  false,
		menuCursor:    0,
		styles:        defaultStyles(),
	}
}

func (a *App) Phase() Phase          { return a.phase }
func (a *App) Section() *SectionModel { return a.section }

func (a *App) MenuItems() []MenuItem { return a.menuItems }
func (a *App) MenuCursor() int       { return a.menuCursor }

func (a *App) ToggleAdvanced() {
	a.showAdvanced = !a.showAdvanced
	if a.showAdvanced {
		a.menuItems = a.allItems
	} else {
		a.menuItems = a.primaryItems
	}
	if a.menuCursor >= len(a.menuItems) {
		a.menuCursor = len(a.menuItems) - 1
	}
}

func (a *App) SelectSection(idx int) {
	if idx < 0 || idx >= len(a.menuItems) {
		return
	}
	item := a.menuItems[idx]
	fields := BuildSectionFields(item.KeyPath)
	a.section = NewSectionModel(item.Title, item.KeyPath, item.ConfigFile, fields)
	a.phase = PhaseSection
}

func (a *App) BackToMenu() {
	a.section = nil
	a.editor = nil
	a.phase = PhaseMenu
}

// --- bubbletea.Model interface ---

func (a *App) Init() tea.Cmd {
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil
	case tea.KeyPressMsg:
		return a.handleKey(msg)
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch a.phase {
	case PhaseMenu:
		return a.handleMenuKey(msg)
	case PhaseSection:
		return a.handleSectionKey(msg)
	case PhaseEditor:
		return a.handleEditorKey(msg)
	case PhaseConfirmSave:
		return a.handleConfirmKey(msg)
	}
	return a, nil
}

func (a *App) handleMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		a.phase = PhaseQuitting
		return a, tea.Quit
	case "up", "k":
		if a.menuCursor > 0 {
			a.menuCursor--
		}
	case "down", "j":
		if a.menuCursor < len(a.menuItems)-1 {
			a.menuCursor++
		}
	case "enter":
		a.SelectSection(a.menuCursor)
	case "a":
		a.ToggleAdvanced()
	}
	return a, nil
}

func (a *App) handleSectionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		if a.section != nil && a.section.IsDirty() {
			a.phase = PhaseConfirmSave
			return a, nil
		}
		a.BackToMenu()
	case "up", "k":
		if a.section != nil {
			a.section.MoveUp()
		}
	case "down", "j":
		if a.section != nil {
			a.section.MoveDown()
		}
	case "enter":
		if a.section != nil {
			f := a.section.CurrentField()
			if f != nil && f.Type() != FieldDrilldown {
				a.editor = NewFieldEditor(f)
				a.phase = PhaseEditor
			}
			// TODO: handle drilldown
		}
	case "d":
		if a.section != nil {
			f := a.section.CurrentField()
			if f != nil {
				f.Reset()
			}
		}
	}
	return a, nil
}

func (a *App) handleEditorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if a.editor == nil {
		a.phase = PhaseSection
		return a, nil
	}
	f := a.editor.field
	switch f.Type() {
	case FieldToggle:
		switch msg.String() {
		case " ", "enter":
			a.editor.Toggle()
			a.phase = PhaseSection
			a.editor = nil
		case "q", "esc":
			a.editor.Cancel()
			a.phase = PhaseSection
			a.editor = nil
		}
	case FieldSelect:
		switch msg.String() {
		case "up", "k":
			a.editor.SelectUp()
		case "down", "j":
			a.editor.SelectDown()
		case "enter":
			a.editor.ConfirmSelect()
			a.phase = PhaseSection
			a.editor = nil
		case "q", "esc":
			a.editor.Cancel()
			a.phase = PhaseSection
			a.editor = nil
		}
	case FieldMultiSelect:
		switch msg.String() {
		case "up", "k":
			a.editor.MultiSelectUp()
		case "down", "j":
			a.editor.MultiSelectDown()
		case " ":
			a.editor.ToggleMultiSelectOption(a.editor.MultiSelectCursor())
		case "enter":
			a.phase = PhaseSection
			a.editor = nil
		case "q", "esc":
			a.editor.Cancel()
			a.phase = PhaseSection
			a.editor = nil
		}
	case FieldText, FieldMasked, FieldNumber, FieldFloat:
		switch msg.String() {
		case "enter":
			a.editor.ConfirmInput()
			a.phase = PhaseSection
			a.editor = nil
		case "esc":
			a.editor.Cancel()
			a.phase = PhaseSection
			a.editor = nil
		default:
			// Simplistic text input: accumulate chars, backspace removes last char.
			if msg.String() == "backspace" {
				if len(a.editor.input) > 0 {
					a.editor.input = a.editor.input[:len(a.editor.input)-1]
				}
			} else if len(msg.String()) == 1 {
				a.editor.input += msg.String()
			}
		}
	}
	return a, nil
}

func (a *App) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		if a.section != nil {
			if err := SaveSection(a.section); err != nil {
				a.errMsg = err.Error()
				return a, nil
			}
		}
		a.errMsg = ""
		a.BackToMenu()
	case "n":
		if a.section != nil {
			// Reset all fields
			for _, f := range a.section.Fields() {
				f.Reset()
			}
		}
		a.errMsg = ""
		a.BackToMenu()
	case "esc":
		a.phase = PhaseSection
	}
	return a, nil
}

func (a *App) View() tea.View {
	switch a.phase {
	case PhaseMenu:
		return tea.NewView(a.viewMenu())
	case PhaseSection:
		return tea.NewView(a.viewSection())
	case PhaseEditor:
		return tea.NewView(a.viewEditor())
	case PhaseConfirmSave:
		return tea.NewView(a.viewConfirm())
	case PhaseQuitting:
		return tea.NewView("saving...")
	}
	return tea.NewView("")
}

func (a *App) viewMenu() string {
	s := a.styles.title.Render("meept config") + "\n\n"
	for i, item := range a.menuItems {
		cursor := "  "
		style := a.styles.unselected
		if i == a.menuCursor {
			cursor = "> "
			style = a.styles.selected
		}
		s += cursor + style.Render(item.Title) + "  " + a.styles.label.Render(item.Description) + "\n"
	}
	s += "\n" + a.styles.help.Render("up/down navigate  enter select  a toggle advanced  q quit")
	return s
}

func (a *App) viewSection() string {
	if a.section == nil {
		return ""
	}
	s := a.styles.breadcrumb.Render("meept config > ") + a.styles.title.Render(a.section.Title()) + "\n\n"
	for i, f := range a.section.Fields() {
		cursor := "  "
		style := a.styles.unselected
		if i == a.section.Cursor() {
			cursor = "> "
			style = a.styles.selected
		}
		dirty := ""
		if f.IsDirty() {
			dirty = a.styles.dirtyMarker.Render(" *")
		}
		s += cursor + style.Render(f.Label()) + "  " + a.styles.value.Render(f.Display()) + dirty + "\n"
	}
	s += "\n" + a.styles.help.Render("up/down navigate  enter edit  d reset  esc back  q back")
	return s
}

func (a *App) viewEditor() string {
	if a.editor == nil || a.editor.field == nil {
		return ""
	}
	f := a.editor.field
	s := a.styles.breadcrumb.Render("meept config > "+a.section.Title()+" > ") + a.styles.title.Render(f.Label()) + "\n\n"

	switch f.Type() {
	case FieldToggle:
		cur := "[ ] disabled"
		if f.Get() == "true" {
			cur = "[*] enabled"
		}
		s += cur + "\n\n"
		s += a.styles.help.Render("space/enter toggle  esc cancel")
	case FieldSelect:
		sf := f.(*SelectField)
		for i, opt := range sf.Options {
			cursor := "  "
			if i == a.editor.SelectCursor() {
				cursor = "> "
			}
			prefix := "[ ] "
			if opt == f.Get() {
				prefix = "[*] "
			}
			s += cursor + prefix + opt + "\n"
		}
		s += "\n" + a.styles.help.Render("up/down navigate  enter confirm  esc cancel")
	case FieldText, FieldMasked, FieldNumber, FieldFloat:
		display := a.editor.InputValue()
		if f.Type() == FieldMasked && display != "" {
			display = "......"
		}
		s += "> " + display + "\n\n"
		s += a.styles.help.Render("type value  enter confirm  esc cancel")
	}

	return s
}

func (a *App) viewConfirm() string {
	s := a.styles.title.Render("save changes?") + "\n\n"
	if a.errMsg != "" {
		s += a.styles.dirtyMarker.Render("error: "+a.errMsg) + "\n\n"
	}
	s += "  y - save\n"
	s += "  n - discard\n"
	s += "  esc - cancel\n"
	return s
}

// RunApp launches the config editor TUI.
func RunApp() error {
	app := NewApp()
	p := tea.NewProgram(app)
	_, err := p.Run()
	return err
}
