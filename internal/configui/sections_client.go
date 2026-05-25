// internal/configui/sections_client.go
package configui

import (
	"sort"

	"github.com/caimlas/meept/internal/tui"
)

func buildClientFields() []Field {
	cfg, _ := tui.LoadClientConfig()
	kb := &cfg.Keybindings
	cp := &kb.CommandPalette
	sb := &cfg.Rendering.Sidebar
	rt := &cfg.Connection.Retry
	return []Field{
		NewSelectField("connection.transport", "connection transport", cfg.Connection.Transport, []string{"rpc", "http", "auto"}),
		NewTextField("connection.address", "connection address", cfg.Connection.Address),
		NewTextField("connection.timeout", "connection timeout", cfg.Connection.Timeout),
		NewDrilldownField("connection.retry", "connection retry", []DrilldownItem{
			{Name: "retry", Fields: []Field{
				NewNumberField("connection.retry.attempts", "retry attempts", rt.Attempts),
				NewTextField("connection.retry.delay", "retry delay", rt.Delay),
			}},
		}),
		NewToggleField("session.auto_resume", "session auto resume", cfg.Session.AutoResume),
		NewTextField("session.default_name", "session default name", cfg.Session.DefaultName),
		NewDrilldownField("keybindings", "keybindings", []DrilldownItem{
			{Name: "keybindings", Fields: []Field{
				NewTextField("keybindings.command_mode", "command mode", kb.CommandMode),
				NewTextField("keybindings.quit", "quit", kb.Quit),
				NewSelectField("keybindings.escape_behavior", "escape behavior", kb.EscapeBehavior, []string{"once", "twice", "off"}),
				NewTextField("keybindings.command_palette.view_chat", "view chat", cp.ViewChat),
				NewTextField("keybindings.command_palette.view_tasks", "view tasks", cp.ViewTasks),
				NewTextField("keybindings.command_palette.view_queue", "view queue", cp.ViewQueue),
				NewTextField("keybindings.command_palette.view_memory", "view memory", cp.ViewMemory),
				NewTextField("keybindings.command_palette.sidebar", "sidebar", cp.Sidebar),
				NewTextField("keybindings.command_palette.sessions", "sessions", cp.Sessions),
				NewTextField("keybindings.command_palette.new_session", "new session", cp.NewSession),
				NewTextField("keybindings.command_palette.rename_session", "rename session", cp.RenameSession),
			}},
		}),
		NewToggleField("vim.enabled", "vim enabled", cfg.Vim.Enabled),
		NewTextField("vim.escape_insert", "vim escape insert", cfg.Vim.EscapeInsert),
		NewTextField("vim.leader", "vim leader", cfg.Vim.Leader),
		NewMapStringStringDrilldownField("vim.normal", "vim normal bindings", "vim.normal", buildMapStringStringItems(cfg.Vim.Normal)),
		NewMapStringStringDrilldownField("vim.insert", "vim insert bindings", "vim.insert", buildMapStringStringItems(cfg.Vim.Insert)),
		NewMapStringStringDrilldownField("vim.visual", "vim visual bindings", "vim.visual", buildMapStringStringItems(cfg.Vim.Visual)),
		NewToggleField("rendering.markdown", "rendering markdown", cfg.Rendering.Markdown),
		NewToggleField("rendering.syntax_highlighting", "rendering syntax highlighting", cfg.Rendering.SyntaxHighlighting),
		NewTextField("rendering.theme", "rendering theme", cfg.Rendering.Theme),
		NewToggleField("rendering.word_wrap", "rendering word wrap", cfg.Rendering.WordWrap),
		NewToggleField("rendering.show_header", "rendering show header", cfg.Rendering.ShowHeader),
		NewToggleField("rendering.sidebar_animation", "rendering sidebar animation", cfg.Rendering.SidebarAnimation),
		NewToggleField("rendering.sidebar.show_metrics", "rendering sidebar show metrics", sb.ShowMetrics),
		NewToggleField("rendering.sidebar.show_activity_feed", "rendering sidebar show activity feed", sb.ShowActivityFeed),
		NewNumberField("rendering.sidebar.default_panel", "sidebar default panel", sb.DefaultPanel),
		NewNumberField("rendering.sidebar.metrics_history", "sidebar metrics history", sb.MetricsHistory),
		NewNumberField("rendering.sidebar.activity_feed_size", "sidebar activity feed size", sb.ActivityFeedSize),
		NewToggleField("chat.auto_copy_on_release", "chat auto copy on release", cfg.Chat.AutoCopyOnRelease),
		NewNumberField("chat.scroll_speed", "chat scroll speed", cfg.Chat.ScrollSpeed),
		NewSelectField("chat.verbosity", "chat verbosity", cfg.Chat.Verbosity, []string{"quiet", "normal", "verbose"}),
	}
}

// buildMapStringStringItems converts a map[string]string to DrilldownItems
// sorted by key. Each item has the map key as Name and a single text field
// holding the value.
func buildMapStringStringItems(m map[string]string) []DrilldownItem {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]DrilldownItem, 0, len(m))
	for _, k := range keys {
		items = append(items, DrilldownItem{
			Name: k,
			Fields: []Field{
				NewTextField("value", k, m[k]),
			},
		})
	}
	return items
}
