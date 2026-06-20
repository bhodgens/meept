package menus

import (
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/transport"
	"github.com/nsf/termbox-go"
)

// mcpServerConfig mirrors the JSON tags of mcp.ServerConfig for unmarshaling
// RPC responses. Kept local to avoid importing the daemon-side mcp package
// from the shared TUI client.
type mcpServerConfig struct {
	Name        string            `json:"name"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Description string            `json:"description,omitempty"`
	Category    string            `json:"category,omitempty"`
	Command     []string          `json:"command,omitempty"`
	URL         string            `json:"url,omitempty"`
	Type        string            `json:"type,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// mcpServerStats mirrors the JSON tags of mcp.ServerStats.
type mcpServerStats struct {
	State         string `json:"state"`
	Requests      int64  `json:"requests"`
	Errors        int64  `json:"errors"`
	LastError     string `json:"last_error"`
}

// mcpServerStatusEntry mirrors mcp.ServerStatusEntry.
type mcpServerStatusEntry struct {
	Config mcpServerConfig `json:"config"`
	Stats  mcpServerStats  `json:"stats"`
}

// isEnabled reports whether the server should be considered enabled.
// nil pointer (absent field) is treated as true for backward compat.
func (c mcpServerConfig) isEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// MCPMenu provides an MCP server management menu. It lists all configured MCP
// servers with their runtime state, and supports toggling the enabled flag
// via the mcp.set_enabled RPC method.
//
// Unlike MemoryMenu/QueueMenu which delegate rendering to the generic Modal
// helper, MCPMenu renders its own multi-column table directly — the existing
// Modal is a single-column key-shortcut list and does not fit the table layout
// from spec Section 6.
type MCPMenu struct {
	client    transport.Client
	servers   []mcpServerStatusEntry
	selected  int
	visible   bool
	statusMsg string // transient message shown inline (e.g. "[toggling...]")
	onDismiss func()
}

// NewMCPMenu creates a new MCP menu bound to the given transport client.
func NewMCPMenu(client transport.Client) *MCPMenu {
	return &MCPMenu{
		client: client,
	}
}

// Show loads the server list from the daemon and displays the menu.
// RPC errors are surfaced inline (via statusMsg) rather than crashing.
func (m *MCPMenu) Show() {
	m.visible = true
	m.selected = 0
	m.statusMsg = ""
	m.refresh()
}

// Hide dismisses the menu.
func (m *MCPMenu) Hide() {
	m.visible = false
	if m.onDismiss != nil {
		m.onDismiss()
	}
}

// IsVisible returns whether the menu is currently visible.
func (m *MCPMenu) IsVisible() bool {
	return m.visible
}

// HandleKey processes keyboard input. Returns true if the key was consumed.
func (m *MCPMenu) HandleKey(ch rune, key termbox.Key) bool {
	if !m.visible {
		return false
	}

	switch key {
	case termbox.KeyArrowUp, termbox.KeyCtrlP:
		if m.selected > 0 {
			m.selected--
		}
		return true
	case termbox.KeyArrowDown, termbox.KeyCtrlN:
		if m.selected < len(m.servers)-1 {
			m.selected++
		}
		return true
	case termbox.KeyEsc, termbox.KeyCtrlC, termbox.KeyCtrlG:
		m.Hide()
		return true
	}

	switch ch {
	case 'e':
		m.toggleSelected()
		return true
	case 'r':
		m.statusMsg = "[refreshing...]"
		m.refresh()
		return true
	}

	return false
}

// refresh fetches the current server list via mcp.list RPC.
// On error, sets statusMsg and clears the server list so the UI shows the
// error rather than stale data.
func (m *MCPMenu) refresh() {
	if m.client == nil {
		m.statusMsg = "[mcp not available]"
		m.servers = nil
		return
	}

	raw, err := m.client.Call("mcp.list", nil)
	if err != nil {
		m.statusMsg = fmt.Sprintf("[mcp not available: %v]", err)
		m.servers = nil
		return
	}

	var resp struct {
		Servers []mcpServerStatusEntry `json:"servers"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		m.statusMsg = fmt.Sprintf("[parse error: %v]", err)
		m.servers = nil
		return
	}
	m.servers = resp.Servers
	if m.statusMsg == "[refreshing...]" || m.statusMsg == "[toggling...]" {
		m.statusMsg = ""
	}
	if m.selected >= len(m.servers) {
		m.selected = max(0, len(m.servers)-1)
	}
}

// toggleSelected flips the Enabled state of the selected server via the
// mcp.set_enabled RPC. Synchronous (blocks the UI briefly) per spec —
// no goroutine spawned.
func (m *MCPMenu) toggleSelected() {
	if m.selected < 0 || m.selected >= len(m.servers) {
		return
	}
	entry := m.servers[m.selected]
	current := entry.Config.isEnabled()
	name := entry.Config.Name

	m.statusMsg = "[toggling...]"

	// Synchronous RPC call — no goroutine. The transport.Client.Call is
	// blocking; we deliberately do not spawn a goroutine to avoid races
	// on m.servers/m.selected/m.statusMsg.
	raw, err := m.client.Call("mcp.set_enabled", map[string]any{
		"name":    name,
		"enabled": !current,
	})
	if err != nil {
		m.statusMsg = fmt.Sprintf("[toggle failed: %v]", err)
		return
	}

	// Parse returned single ServerStatusEntry and update the local list
	// in place so the UI reflects the new state immediately.
	var updated mcpServerStatusEntry
	if err := json.Unmarshal(raw, &updated); err != nil {
		m.statusMsg = fmt.Sprintf("[toggle parse error: %v]", err)
		// Fall through to full refresh to recover consistent state.
		m.refresh()
		return
	}
	m.servers[m.selected] = updated
	m.statusMsg = ""
}

// Render draws the MCP menu to the terminal. Uses the Modal chrome (title +
// hints) for the header/footer and renders table rows inline.
func (m *MCPMenu) Render() {
	if !m.visible {
		return
	}

	width, height := termbox.Size()

	// Layout: 4 header rows (border, hints, separator, column header),
	// 1 row per server, 1 footer border. Cap height to terminal.
	headerRows := 4
	footerRows := 1
	bodyRows := len(m.servers)
	if bodyRows == 0 {
		// Reserve a row for the "no servers" / status message.
		bodyRows = 1
	}

	boxHeight := headerRows + bodyRows + footerRows
	if boxHeight > height-2 {
		boxHeight = height - 2
	}

	boxWidth := 64
	if boxWidth > width-2 {
		boxWidth = width - 2
	}

	x := (width - boxWidth) / 2
	y := (height - boxHeight) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	muted := termbox.Attribute(sharedclient.ColorMuted)
	white := termbox.Attribute(sharedclient.ColorWhite)
	orange := termbox.Attribute(sharedclient.ColorOrange) | termbox.AttrBold

	// Top border
	m.drawHLine(x, y, boxWidth, '-')
	// Title
	title := " mcp servers "
	for i, r := range title {
		if x+1+i >= x+boxWidth-1 {
			break
		}
		termbox.SetCell(x+1+i, y, r, orange, termbox.ColorDefault)
	}

	// Hint rows
	hint1 := "  e  toggle enabled on selected                            "
	hint2 := "  ↑↓ move   r refresh   esc close                          "
	m.drawText(x+1, y+1, hint1, boxWidth, muted)
	m.drawText(x+1, y+2, hint2, boxWidth, muted)

	// Separator + column header
	m.drawHLine(x, y+3, boxWidth, '-')
	m.drawText(x+1, y+3, " en server   status    reqs    errors description", boxWidth, muted)

	// Body rows
	bodyStart := y + headerRows
	if len(m.servers) == 0 {
		msg := m.statusMsg
		if msg == "" {
			msg = "[no servers configured]"
		}
		m.drawText(x+1, bodyStart, msg, boxWidth, muted)
	} else {
		for i, srv := range m.servers {
			rowY := bodyStart + i
			if rowY >= y+boxHeight-1 {
				break
			}
			row := m.formatRow(srv)
			fg := white
			if i == m.selected {
				fg = orange
				// Invert: draw selection marker at the start.
				termbox.SetCell(x+1, rowY, '>', fg, termbox.ColorDefault)
			} else {
				termbox.SetCell(x+1, rowY, ' ', muted, termbox.ColorDefault)
			}
			m.drawText(x+2, rowY, row, boxWidth-2, fg)
		}
	}

	// Footer border
	m.drawHLine(x, y+boxHeight-1, boxWidth, '-')

	// Status message (e.g. "[toggling...]") on the footer line.
	if m.statusMsg != "" {
		m.drawText(x+1, y+boxHeight-1, " "+m.statusMsg+" ", boxWidth, muted)
	}
}

// formatRow formats a server entry as a single-line row matching the column
// layout in spec Section 6.
func (m *MCPMenu) formatRow(srv mcpServerStatusEntry) string {
	en := "■"
	if !srv.Config.isEnabled() {
		en = "□"
	}
	name := srv.Config.Name
	if len(name) > 15 {
		name = name[:12] + "..."
	}
	status := srv.Stats.State
	if status == "" {
		status = "inactive"
	}
	if len(status) > 10 {
		status = status[:7] + "..."
	}
	desc := srv.Config.Description
	if len(desc) > 20 {
		desc = desc[:17] + "..."
	}
	return fmt.Sprintf("%-2s %-15s %-10s %8d %8d %s",
		en, name, status, srv.Stats.Requests, srv.Stats.Errors, desc)
}

// drawHLine draws a horizontal line of ch across the given width, with '+'
// corners.
func (m *MCPMenu) drawHLine(x, y, width int, fill rune) {
	muted := termbox.Attribute(sharedclient.ColorMuted)
	for dx := range width {
		ch := fill
		if dx == 0 || dx == width-1 {
			ch = '+'
		}
		termbox.SetCell(x+dx, y, ch, muted, termbox.ColorDefault)
	}
}

// drawText writes text starting at (x, y), truncating at maxWidth.
func (m *MCPMenu) drawText(x, y int, text string, maxWidth int, fg termbox.Attribute) {
	dx := 0
	for _, r := range text {
		if dx >= maxWidth-1 {
			break
		}
		termbox.SetCell(x+dx, y, r, fg, termbox.ColorDefault)
		dx++
	}
}

// SetCallbacks sets the dismiss callback.
func (m *MCPMenu) SetCallbacks(onDismiss func()) {
	m.onDismiss = onDismiss
}
