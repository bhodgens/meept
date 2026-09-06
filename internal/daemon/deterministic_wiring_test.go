package daemon

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

func TestApplyDeterministicToolsWrapsWebTools(t *testing.T) {
	t.Setenv("MEEPT_DETERMINISTIC_TOOLS", "")

	registry := tools.NewRegistry(slog.New(slog.DiscardHandler))
	registry.Register(builtin.NewWebFetchTool(0, 0))
	registry.Register(builtin.NewWebSearchTool(0))

	applyDeterministicToolsFromConfig(registry, config.AgentToolsConfig{DeterministicTools: true})

	if _, wrapped := registry.Get("web_fetch").(*builtin.CachedFetchTool); !wrapped {
		t.Error("web_fetch not wrapped under deterministic_tools=true")
	}
	// The search tool registers under "web_search" (snake_case); the
	// "websearch" spelling only appears in config lists like
	// DefaultAlwaysFullTools.
	if _, wrapped := registry.Get("web_search").(*builtin.CachedFetchTool); !wrapped {
		t.Error("web_search not wrapped under deterministic_tools=true")
	}
	if len(lastDeterministicWiring.wrapped) != 2 {
		t.Errorf("wiring state = %v, want 2 wrapped tools", lastDeterministicWiring.wrapped)
	}
	// The wrapper must still satisfy StreamingTool, or the executor's
	// streaming path would bypass the gate live.
	if _, ok := registry.Get("web_fetch").(interface {
		ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(tools.ProgressUpdate)) (any, error)
	}); !ok {
		t.Error("wrapped web_fetch lost StreamingTool")
	}
}

func TestApplyDeterministicToolsNoopWhenOff(t *testing.T) {
	t.Setenv("MEEPT_DETERMINISTIC_TOOLS", "")

	registry := tools.NewRegistry(slog.New(slog.DiscardHandler))
	registry.Register(builtin.NewWebFetchTool(0, 0))

	applyDeterministicToolsFromConfig(registry, config.AgentToolsConfig{})

	if _, wrapped := registry.Get("web_fetch").(*builtin.CachedFetchTool); wrapped {
		t.Error("web_fetch wrapped with gate off; default must be no-op")
	}
	if len(lastDeterministicWiring.wrapped) != 0 {
		t.Errorf("wiring state = %v, want empty", lastDeterministicWiring.wrapped)
	}
}

func TestApplyDeterministicToolsIdempotent(t *testing.T) {
	t.Setenv("MEEPT_DETERMINISTIC_TOOLS", "")

	registry := tools.NewRegistry(slog.New(slog.DiscardHandler))
	registry.Register(builtin.NewWebFetchTool(0, 0))

	applyDeterministicToolsFromConfig(registry, config.AgentToolsConfig{DeterministicTools: true})
	applyDeterministicToolsFromConfig(registry, config.AgentToolsConfig{DeterministicTools: true})

	wrapped, ok := registry.Get("web_fetch").(*builtin.CachedFetchTool)
	if !ok {
		t.Fatal("web_fetch not wrapped after two applies")
	}
	if _, inner := wrapped.Inner().(*builtin.WebFetchTool); !inner {
		t.Error("double-wrapped: inner tool is not the original WebFetchTool")
	}
}

func TestApplyDeterministicToolsEnvOverrideWrapsEvenWithConfigOff(t *testing.T) {
	t.Setenv("MEEPT_DETERMINISTIC_TOOLS", "1")

	registry := tools.NewRegistry(slog.New(slog.DiscardHandler))
	registry.Register(builtin.NewWebFetchTool(0, 0))

	applyDeterministicToolsFromConfig(registry, config.AgentToolsConfig{})

	if _, wrapped := registry.Get("web_fetch").(*builtin.CachedFetchTool); !wrapped {
		t.Error("env override must enable wrapping even with config off")
	}
}

func TestDeterministicStatusDisclosed(t *testing.T) {
	// The status handler surfaces the gate for meept-bench preflight.
	h := NewStatusHandler(nil, slog.New(slog.DiscardHandler))
	if h.deterministicTools {
		t.Error("default StatusHandler must have deterministicTools=false")
	}
	h.deterministicTools = true
}
