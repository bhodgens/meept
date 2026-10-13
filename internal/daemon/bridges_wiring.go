package daemon

import (
	"log/slog"
	"path/filepath"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
)

// wireBridges wires the bridge subsystem (branch 09) into a booted memory
// manager: the BridgeStore (so recall + invalidation + visibility have a
// store to talk to), and the consolidator's generation/verification
// components (so the offline pass can produce bridges). Everything is
// defence-in-depth gated: memory.bridges.enabled must be true AND the store
// + generator must be non-nil for generation to ever run — the default
// config keeps the pass dormant with zero LLM calls.
//
// Failure is non-fatal: bridges are an optional quality feature, so a store
// that cannot open logs a warning and leaves the manager bridge-less
// (generation no-ops, invalidation no-ops, the CLI reports no bridges).
func wireBridges(memoryMgr *memory.Manager, chatter llm.Chatter, memCfg config.MemoryConfig, dataDir string, logger *slog.Logger) {
	if memoryMgr == nil || !memoryMgr.IsInitialized() {
		return
	}
	if !memCfg.Bridges.Enabled {
		logger.Debug("bridge store skipped: memory.bridges.enabled is false")
		return
	}

	bridgePath := filepath.Join(dataDir, "bridge.vec.db")
	store, err := memory.NewBridgeStore(bridgePath)
	if err != nil {
		logger.Warn("bridge store open failed; bridge generation/recall/visibility disabled",
			"path", bridgePath, "error", err)
		return
	}
	store = store.WithLogger(logger.With("component", "bridge-store"))
	memoryMgr.SetBridgeStore(store)
	logger.Info("bridge store wired", "path", bridgePath)

	// Generation + verification are consolidator-pass concerns (offline
	// only, never the chat path). They need an LLM chatter; without one the
	// pass stays dormant regardless of config (SetBridgeComponents guards
	// on non-nil). The verifier is optional even when wired: with verify on
	// but no verifier available, bridges persist unverified.
	if chatter == nil {
		logger.Debug("bridge generation skipped: no llm chatter available")
		return
	}
	consolidator := memoryMgr.Consolidator()
	if consolidator == nil {
		return
	}
	consolidator.SetBridgeComponents(store, memory.NewBridgeGenerator(chatter))
	if memCfg.Bridges.Verify {
		consolidator.SetBridgeVerifier(memory.NewBridgeEvidenceVerifier(chatter))
	}
	logger.Info("bridge generation wired",
		"kb", memCfg.Bridges.KB,
		"bridge_df_max", memCfg.Bridges.BridgeDFMax,
		"verify", memCfg.Bridges.Verify,
	)
}
