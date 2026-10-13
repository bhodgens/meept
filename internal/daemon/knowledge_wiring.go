package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory/knowledge"
	"github.com/caimlas/meept/internal/memory/knowledge/codeindex"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// wireKnowledgeAssets wires the knowledge-asset consolidation passes
// (tree 04 leaf L4): the watched-dir markdown sync (knowledge.SyncDir over
// memory.knowledge.ingest_dir) and the Go code index (codeindex.IndexGoDirs
// over memory.knowledge.code_dirs) both re-run on the periodic
// consolidation tick. No new ticker is created — the consolidator's
// existing timer drives everything (the same seam SetL0Prune /
// SetBridgeVerifier use).
//
// The knowledge STORE itself is opened by registerBuiltinTools (T04-L2) for
// the knowledge_* tools; that store is lazily opened per tool registration
// and not reachable from here, so this wiring opens its OWN handle over the
// same data dir. SQLite WAL + _busy_timeout=5000 makes the two-handle
// layout safe; both handles live for the daemon's lifetime and are closed
// by Components.KnowledgeStore.Close in stopComponents.
//
// Everything here is best-effort and gated: a failed store open or a nil
// memory manager disables the passes with an honest log line — never a
// fatal boot error. The pass closure itself no-ops when neither ingest_dir
// nor code_dirs is configured (nothing to do on the tick).
func wireKnowledgeAssets(c *Components, cfg *config.Config, logger *slog.Logger) *knowledge.Store {
	kCfg := cfg.Memory.Knowledge
	if !kCfg.Enabled {
		logger.Debug("knowledge consolidation passes skipped: memory.knowledge.enabled is false")
		return nil
	}
	if c.MemoryManager == nil || !c.MemoryManager.IsInitialized() {
		logger.Warn("knowledge consolidation passes skipped: memory manager unavailable")
		return nil
	}
	if strings.TrimSpace(kCfg.IngestDir) == "" && len(kCfg.CodeDirs) == 0 {
		logger.Debug("knowledge consolidation passes skipped: no ingest_dir and no code_dirs configured")
		return nil
	}

	consolidator := c.MemoryManager.Consolidator()
	if consolidator == nil {
		logger.Warn("knowledge consolidation passes skipped: consolidator unavailable")
		return nil
	}

	store, err := knowledge.New(
		filepath.Join(cfg.Daemon.DataDir, "knowledge"),
		kCfg.MaxPages,
		logger.With("component", "knowledge-consolidation"),
	)
	if err != nil {
		logger.Warn("knowledge consolidation passes disabled: store open failed", "error", err)
		return nil
	}

	ingestDir := kCfg.IngestDir
	codeDirs := kCfg.CodeDirs
	kLogger := logger.With("component", "knowledge-consolidation")
	consolidator.SetKnowledgeDirIngest(func(ctx context.Context) {
		// Watched-dir markdown sync (tree 04 L2 surface, wired here in
		// L4). Best-effort: failures are logged, never abort
		// consolidation (the consolidator's pass contract).
		if strings.TrimSpace(ingestDir) != "" {
			rep, err := knowledge.SyncDir(ctx, store, ingestDir)
			if err != nil {
				kLogger.Warn("knowledge dir ingest failed", "dir", ingestDir, "error", err)
			} else if rep.Ingested > 0 || len(rep.Errors) > 0 {
				kLogger.Info("knowledge dir ingest complete",
					"dir", ingestDir,
					"ingested", rep.Ingested,
					"skipped", rep.Skipped,
					"errors", len(rep.Errors),
				)
			}
		}
		// Go code index (tree 04 L3 surface). Same closure so one tick
		// drives both passes; equally best-effort.
		if len(codeDirs) > 0 {
			rep, err := codeindex.IndexGoDirs(ctx, store, codeDirs, kLogger)
			if err != nil {
				kLogger.Warn("knowledge code index failed", "dirs", codeDirs, "error", err)
			} else if rep.FilesIndexed > 0 || len(rep.Errors) > 0 {
				kLogger.Info("knowledge code index complete",
					"files_indexed", rep.FilesIndexed,
					"files_skipped", rep.FilesSkipped,
					"symbols", rep.SymbolsFound,
					"edges", rep.EdgesFound,
					"errors", len(rep.Errors),
				)
			}
		}
	})

	kLogger.Info("knowledge consolidation passes wired",
		"ingest_dir", ingestDir,
		"code_dirs", len(codeDirs),
		"max_pages", kCfg.MaxPages,
	)
	return store
}

// registerKnowledgeImpactTool registers the knowledge_impact tool against
// the knowledge store opened by wireKnowledgeAssets. Split from the store
// wiring so the tool can be registered at the same site as its three
// siblings (registerBuiltinTools gates the family on enabled) while the
// store handle lives on Components. Called only when both the config gate
// and the store open succeeded.
func registerKnowledgeImpactTool(registry *tools.Registry, store *knowledge.Store) {
	if registry == nil || store == nil {
		return
	}
	registry.Register(builtin.NewKnowledgeImpactTool(func(ctx context.Context, symbol string) ([]string, error) {
		return codeindex.ImpactedBy(ctx, store, symbol)
	}))
}
