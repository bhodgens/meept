package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

// wireSkillKnowledgeStores constructs the TraceStore + WikiStore when
// [skills.wiki] is enabled (05-config-wiring.md Task 2), creating the wiki
// directory, and attaches the wiki store to the learning pipeline (before
// Initialize — SetWikiStore's contract) when lp is non-nil. A disabled wiki
// yields (nil, nil) and creates nothing; disabled ⇒ byte-identical runtime
// behavior. Extracted as a testable unit-shaped helper so the wiring can be
// exercised without constructing full daemon Components.
//
// The dir supports "~/" prefixes via os.UserHomeDir — expandConfigPaths does
// not cover the skills section.
func wireSkillKnowledgeStores(cfg *config.Config, lp *selfimprove.LearningPipeline, logger *slog.Logger) (*selfimprove.TraceStore, *selfimprove.WikiStore) {
	if logger == nil {
		logger = slog.Default()
	}
	if !cfg.Skills.Wiki.Enabled {
		return nil, nil
	}
	dir := cfg.Skills.Wiki.Dir
	if strings.HasPrefix(dir, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if dir == "~" {
				dir = home
			} else {
				dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
			}
		}
	}
	//nolint:gosec // wiki dirs are user-readable project data
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("Failed to create wiki directory", "dir", dir, "error", err)
		return nil, nil
	}
	ts := selfimprove.NewTraceStore(dir, logger.With("component", "wiki-trace-store"))
	ws := selfimprove.NewWikiStore(dir, logger.With("component", "wiki-store"))
	if lp != nil {
		lp.SetWikiStore(ws)
		logger.Info("Wiki knowledge stores wired", "dir", dir)
	}
	return ts, ws
}

// traceStoreProvider adapts *selfimprove.TraceStore to lifecycle.TraceProvider.
// The interface is satisfied structurally; this explicit 5-line adapter keeps
// the daemon's import surface visible at the wiring site (deliberately kept
// in the daemon package per 05-config-wiring.md).
type traceStoreProvider struct {
	ts *selfimprove.TraceStore
}

// Sample delegates pass-through to TraceStore.Sample.
func (p *traceStoreProvider) Sample(maxFails, maxPasses, maxChars int) ([]selfimprove.TraceRecord, error) {
	return p.ts.Sample(maxFails, maxPasses, maxChars)
}

// evolverKnowledgeOptions appends the wiki-era EvolverOptions when the
// knowledge stores exist: the execution-trace source (Pass A sampling) and
// the wiki store (Pass A context + skill-impact ledger). Only called from
// inside the evolver-construction block (evolver enabled), per master
// §Contract 5 — wiki.enabled=true with evolver.enabled=false must not
// change runtime behavior.
func evolverKnowledgeOptions(ts *selfimprove.TraceStore, ws *selfimprove.WikiStore) []lifecycle.EvolverOption {
	var opts []lifecycle.EvolverOption
	if ts != nil {
		opts = append(opts, lifecycle.WithTraceProvider(&traceStoreProvider{ts: ts}))
	}
	if ws != nil {
		opts = append(opts, lifecycle.WithWikiStore(ws))
	}
	return opts
}
