package daemon

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// mcpSearchBackend adapts *mcp.Manager to builtin.SearchMCPBackend
// (internal/tools/builtin cannot import internal/tools/mcp — import cycle —
// so this adapter lives on the daemon side and is wired in at startup).
type mcpSearchBackend struct {
	manager *mcp.Manager
}

// SearchServer picks the search MCP tool to serve websearch's MCP-first
// chain. Known SearXNG MCP servers expose their search tool under one of a
// few names (`searxng.search`, `searxng.search_web`,
// `searxng.searxng_web_search`); a server that exposes none of them is
// passed over so the caller falls back to the DuckDuckGo scraper. Also
// requires a live connection and an active health state.
func (b *mcpSearchBackend) SearchServer() (string, bool) {
	if b == nil || b.manager == nil {
		return "", false
	}
	for _, name := range searchMCPCandidates {
		cfg, stats, ok := b.manager.ServerStatus(name)
		if !ok || !cfg.IsEnabled() || stats.State != mcp.StateActive {
			continue
		}
		client := b.manager.GetClient(name)
		if client == nil || !client.IsConnected() {
			continue
		}
		for _, toolName := range searchMCPToolNames {
			full := fmt.Sprintf("%s.%s", name, toolName)
			for _, info := range client.ListTools() {
				if info.Name == toolName {
					return full, true
				}
			}
		}
	}
	return "", false
}

// Call dispatches a fully-qualified MCP tool call through the manager (rate
// limiting, stats, and sanitization apply as for any MCP call).
func (b *mcpSearchBackend) Call(ctx context.Context, fullName string, args map[string]any) (*tools.ToolResult, error) {
	if b == nil || b.manager == nil {
		return nil, fmt.Errorf("search mcp backend has no manager")
	}
	return b.manager.CallTool(ctx, fullName, args)
}

// ensureMCPSearchProvider installs an MCP-first search provider on the
// websearch tool when an MCP manager exists. Called from
// registerBuiltinTools' caller after the MCP manager is built; a nil
// manager leaves the tool on the DuckDuckGo-only path.
func ensureMCPSearchProvider(webSearchTool *builtin.WebSearchTool, manager *mcp.Manager) {
	if webSearchTool == nil || manager == nil {
		return
	}
	provider := builtin.NewMCPSearchProvider(builtin.DefaultSearchTimeout)
	provider.SetBackend(&mcpSearchBackend{manager: manager})
	webSearchTool.SetSearchProvider(provider)
}

// searchMCPCandidates are the MCP server names consulted, in order.
var searchMCPCandidates = []string{"searxng"}

// searchMCPToolNames are the tool names known SearXNG MCP servers expose
// for general web search (zatevakhin/searxng-mcp exposes `search`,
// knucklessg1/searxng-mcp and ihor-sokoliuk/mcp-searxng expose the other
// spellings depending on version).
var searchMCPToolNames = []string{"search", "search_web", "searxng_web_search"}
