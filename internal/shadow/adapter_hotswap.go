package shadow

import (
	"context"
	"fmt"
)

// OllamaActivator is the narrow interface HotSwap needs from the Ollama
// adapter. *adapters.OllamaAdapter satisfies it via its CreateModelWithAdapter
// method when wrapped (the daemon wiring handles the wrapper).
type OllamaActivator interface {
	ActivateAdapter(ctx context.Context, baseName, adapterName, adapterPath string) error
}

// HotSwapCallback is invoked after a successful hot-swap. The argument is
// the baked model name (without provider prefix) — e.g.
// "qwen2.5:7b-shadow-v1abcd12". Implementations typically prepend a
// provider prefix (such as "ollama/") and call
// agentLoop.SetModelOverride on the result.
type HotSwapCallback func(bakedModelName string)

// hotSwapCoordinator orchestrates adapter activation across the Ollama
// backend (model recreation) and the agent loop (model-override callback).
type hotSwapCoordinator struct {
	activator OllamaActivator
	callback  HotSwapCallback
}

// Activate performs the end-to-end hot-swap:
//  1. ActivateAdapter in Ollama (bakes weights into a new model) — only if
//     an activator is registered.
//  2. Invoke the callback so the daemon can wrap the baked name with a
//     provider prefix and feed it to agentLoop.SetModelOverride.
//  3. Flip the DB flag via Manager.ActivateAdapter (passes eval gate).
//
// bakedName is constructed as "<base>-shadow-<adapterIDprefix8>". The
// daemon-side callback is responsible for prepending the provider (e.g.
// "ollama/") since the provider mapping is a daemon-level concern.
func (h *hotSwapCoordinator) Activate(ctx context.Context, m *Manager, adapter *Adapter) error {
	if h.activator != nil {
		// Truncate adapter ID to 8 chars for a short, stable suffix.
		idPrefix := adapter.ID
		if len(idPrefix) > 8 {
			idPrefix = idPrefix[:8]
		}
		bakedName := fmt.Sprintf("%s-shadow-%s", adapter.ModelBase, idPrefix)
		if err := h.activator.ActivateAdapter(ctx, adapter.ModelBase, bakedName, adapter.AdapterPath); err != nil {
			return fmt.Errorf("ollama activate: %w", err)
		}
		if h.callback != nil {
			h.callback(bakedName)
		}
	}
	// Flip DB flag (subject to eval gate).
	if err := m.ActivateAdapter(ctx, adapter.ID); err != nil {
		return fmt.Errorf("set active adapter: %w", err)
	}
	return nil
}
