// internal/configui/sections_client.go
package configui

import "github.com/caimlas/meept/internal/tui"

func buildClientFields() []Field {
	cfg, _ := tui.LoadClientConfig()
	return []Field{
		NewSelectField("connection.transport", "connection transport", cfg.Connection.Transport, []string{"rpc", "http", "auto"}),
		NewTextField("connection.address", "connection address", cfg.Connection.Address),
		NewTextField("connection.timeout", "connection timeout", cfg.Connection.Timeout),
		NewToggleField("session.auto_resume", "session auto resume", cfg.Session.AutoResume),
		NewTextField("session.default_name", "session default name", cfg.Session.DefaultName),
		NewToggleField("vim.enabled", "vim enabled", cfg.Vim.Enabled),
		NewTextField("vim.escape_insert", "vim escape insert", cfg.Vim.EscapeInsert),
		NewTextField("vim.leader", "vim leader", cfg.Vim.Leader),
		NewToggleField("rendering.markdown", "rendering markdown", cfg.Rendering.Markdown),
		NewToggleField("rendering.syntax_highlighting", "rendering syntax highlighting", cfg.Rendering.SyntaxHighlighting),
		NewTextField("rendering.theme", "rendering theme", cfg.Rendering.Theme),
		NewToggleField("rendering.word_wrap", "rendering word wrap", cfg.Rendering.WordWrap),
		NewToggleField("rendering.show_header", "rendering show header", cfg.Rendering.ShowHeader),
		NewToggleField("rendering.sidebar_animation", "rendering sidebar animation", cfg.Rendering.SidebarAnimation),
		NewToggleField("rendering.sidebar.show_metrics", "rendering sidebar show metrics", cfg.Rendering.Sidebar.ShowMetrics),
		NewToggleField("rendering.sidebar.show_activity_feed", "rendering sidebar show activity feed", cfg.Rendering.Sidebar.ShowActivityFeed),
		NewToggleField("chat.auto_copy_on_release", "chat auto copy on release", cfg.Chat.AutoCopyOnRelease),
		NewNumberField("chat.scroll_speed", "chat scroll speed", cfg.Chat.ScrollSpeed),
		NewSelectField("chat.verbosity", "chat verbosity", cfg.Chat.Verbosity, []string{"quiet", "normal", "verbose"}),
	}
}
