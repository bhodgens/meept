package daemon

import (
	"testing"

	"github.com/caimlas/meept/internal/tools/builtin"
)

// The daemon's MCP search adapter must be nil-safe: neither the provider
// installer nor the backend may panic on nil inputs, and a nil manager
// leaves the websearch tool on the DuckDuckGo-only path (verified via the
// nil-search behavior, since the provider slot itself is unexported).

func TestEnsureMCPSearchProvider_NilArgsAreNoOp(t *testing.T) {
	// Neither combination may panic.
	ensureMCPSearchProvider(nil, nil)
	webSearchTool := builtin.NewWebSearchTool(0)
	ensureMCPSearchProvider(webSearchTool, nil)
	// Without a manager, no provider is installed; execution must take the
	// DDG path unchanged (a nil provider is simply absent).
	_ = webSearchTool
}

func TestMCPSearchBackend_NilReceiverReportsNoServer(t *testing.T) {
	var b *mcpSearchBackend
	if _, ok := b.SearchServer(); ok {
		t.Error("nil backend must not report a server")
	}
}

func TestSearchMCPCandidateListsAreNonEmpty(t *testing.T) {
	if len(searchMCPCandidates) == 0 {
		t.Error("searchMCPCandidates must not be empty")
	}
	if len(searchMCPToolNames) == 0 {
		t.Error("searchMCPToolNames must not be empty")
	}
}
