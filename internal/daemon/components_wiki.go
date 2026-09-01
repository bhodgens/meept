package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
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

// traceStorePersist converts an agent.TraceRecordPayload into a store record
// and writes it to ts. Shared by both trace wirings (loop turn traces via
// agent.WithTraceWriter and state-run traces via the state runtime) so the
// payload→record mapping exists exactly once.
func traceStorePersist(ts *selfimprove.TraceStore, p agent.TraceRecordPayload) (string, error) {
	steps := make([]selfimprove.TraceStep, len(p.Steps))
	for i, s := range p.Steps {
		steps[i] = selfimprove.TraceStep{
			Action:  s.Action,
			Input:   s.Input,
			Output:  s.Output,
			Success: s.Success,
		}
	}
	return ts.Write(&selfimprove.TraceRecord{
		ID:             p.ID,
		SessionID:      p.SessionID,
		Domain:         p.Domain,
		Outcome:        p.Outcome,
		Error:          p.Error,
		InjectedSkills: p.InjectedSkills,
		Steps:          steps,
		Summary:        p.Summary,
		CreatedAt:      p.CreatedAt,
	})
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

// initializeSkillEvolver constructs the skill evolver + scheduler (closed-loop
// skill evolution). Extracted from the old inline block in initializeSkills:
// that block executed BEFORE NewComponents assigned c.SkillUsageTracker /
// c.SkillWriter, so its nil-gate was always false and the evolver never
// constructed (found by the wiki smoke test, 2026-08-29). Callers must invoke
// this AFTER the tracker, writer, learning pipeline, and PlanManager exist —
// daemon.go calls it in the plan-system wiring section.
// Gated on cfg.Skills.Evolver.Enabled.
func (c *Components) initializeSkillEvolver(cfg *config.Config, logger *slog.Logger) {
	if !cfg.Skills.Evolver.Enabled || c.SkillUsageTracker == nil || c.SkillWriter == nil {
		return
	}
	verifier := lifecycle.NewVerifier(
		c.LLMClient,
		logger.With("component", "skill-verifier"),
	)
	// Build evolver options. When the reflection collector is wired,
	// bridge it to the evolver's ReflectionProposer interface via the
	// reflectionProposerAdapter. This closes the self-improvement loop:
	// per-turn reflection proposals queued by ReflectionCollector are
	// drained at the start of each evolver cycle and routed through the
	// verifier as skill creates/updates.
	var evolverOpts []lifecycle.EvolverOption
	if c.ReflectionCollector != nil {
		evolverOpts = append(evolverOpts, lifecycle.WithReflectionProposer(
			&reflectionProposerAdapter{rc: c.ReflectionCollector},
		))
		logger.Info("Skill evolver wired to reflection collector",
			"component", "skill-evolver",
		)
	}
	// Wire the wiki-era knowledge sources (execution-trace sampling +
	// wiki store for Pass A context and the skill-impact ledger). Only
	// inside this block: wiki.enabled=true with the evolver disabled
	// must not change evolver behavior (master §Contract 5).
	if knowledgeOpts := evolverKnowledgeOptions(c.TraceStore, c.WikiStore); len(knowledgeOpts) > 0 {
		evolverOpts = append(evolverOpts, knowledgeOpts...)
		logger.Info("Skill evolver wired to knowledge stores",
			"trace_provider", c.TraceStore != nil,
			"wiki_store", c.WikiStore != nil,
		)
	}
	// Construct the evolver-DEDICATED PlanManager pinned to the
	// skills.evolver.plan_dir sink, so machine-originated plans land under
	// ~/.meept/plans/evolver (or the configured override) instead of the
	// daemon CWD's docs/plans. The shared c.PlanManager is untouched —
	// human-authored plans keep landing in the repo (evolver plan-sink
	// leaf 01 invariant). Falls back to the shared manager only when the
	// sink store cannot be created.
	evolverPlanMgr := c.PlanManager
	c.EvolverPlanManager = c.PlanManager
	// Sink lifecycle context: derived from the components lifecycle so a
	// Stop() cancels it (via EvolverPlanCancel in stopComponents). Stored
	// on the component for the approval actuator (leaf 03); it is
	// deliberately created here so the ownership chain is established at
	// wiring time.
	sinkCtx, sinkCancel := context.WithCancel(c.ctx)
	c.EvolverPlanCtx = sinkCtx
	evolverPlanStore, err := plan.NewSQLiteStore(
		filepath.Join(c.Config.Daemon.DataDir, "plans-evolver.db"), logger)
	if err != nil {
		sinkCancel()
		logger.Warn("Skill evolver plan sink store unavailable; falling back to shared plan manager",
			"error", err)
	} else {
		c.EvolverPlanStore = evolverPlanStore
		c.EvolverPlanCancel = sinkCancel
		if sinkMgr, err := c.newEvolverPlanManager(evolverPlanStore, cfg, logger); err == nil {
			evolverPlanMgr = sinkMgr
			c.EvolverPlanManager = sinkMgr
			logger.Info("Skill evolver wired to dedicated plan sink",
				"plan_dir", cfg.Skills.Evolver.PlanDir,
			)
		} else {
			sinkCancel()
			logger.Warn("Skill evolver plan sink unavailable; falling back to shared plan manager",
				"error", err)
		}
	}
	c.SkillEvolver = lifecycle.NewEvolver(
		c.SkillUsageTracker,
		c.LearningPipeline,
		c.SkillWriter,
		c.SkillRegistry,
		c.CapabilityIndex,
		verifier,
		c.LLMClient,
		evolverPlanMgr,
		cfg.Skills.Evolver,
		logger,
		evolverOpts...,
	)
	c.SkillEvolverSched = lifecycle.NewEvolverScheduler(
		c.SkillEvolver,
		cfg.Skills.Evolver.Interval,
		logger.With("component", "skill-evolver-scheduler"),
	)
	logger.Info("Skill evolver + scheduler initialized",
		"interval", cfg.Skills.Evolver.Interval,
		"auto_apply", cfg.Skills.Evolver.AutoApply,
	)
}

// newEvolverPlanManager constructs the evolver-DEDICATED PlanManager: same
// bus/task-creator wiring as the shared human PlanManager, but with
// Storage.ExternalPath pinned to the resolved skills.evolver.plan_dir sink so
// machine-originated plans land under ~/.meept/plans/evolver (or the
// configured override) instead of the daemon CWD's docs/plans. The shared
// manager is NOT repointed — human-authored plans keep their repo-relative
// default (evolver plan-sink leaf 01 invariant).
//
// store must be non-nil; the message bus and task creator may be nil (the
// PlanManager degrades gracefully). The sink directory itself is created
// lazily by the first plan write (plan.WritePlanMarkdown does MkdirAll),
// not here.
func (c *Components) newEvolverPlanManager(store plan.PlanStore, cfg *config.Config, logger *slog.Logger) (*plan.PlanManager, error) {
	return c.newEvolverPlanManagerWithCreator(store, cfg, logger, c.taskCreatorAdapterForEvolver())
}

// newEvolverPlanManagerWithCreator is newEvolverPlanManager with an explicit
// TaskCreator, so tests can inject a stub (the approval actuator wiring
// exercises the full approve → synthesize path). Production passes the
// shared task-creator adapter.
func (c *Components) newEvolverPlanManagerWithCreator(store plan.PlanStore, cfg *config.Config, logger *slog.Logger, creator plan.TaskCreator) (*plan.PlanManager, error) {
	if store == nil {
		return nil, fmt.Errorf("evolver plan sink: nil plan store")
	}
	sink := cfg.Skills.Evolver.PlanDir
	if sink == "" {
		// Load-path normalization normally guarantees a value; belt-and-
		// suspenders for direct constructions (tests, embedded daemons).
		if err := config.NormalizeEvolverDefaults(&cfg.Skills.Evolver); err != nil {
			return nil, fmt.Errorf("evolver plan sink: %w", err)
		}
		sink = cfg.Skills.Evolver.PlanDir
	}
	if !filepath.IsAbs(sink) {
		return nil, fmt.Errorf("evolver plan sink: plan_dir %q is not absolute", sink)
	}
	evolverPlans := cfg.Plans
	evolverPlans.Storage.ExternalPath = sink
	return plan.NewPlanManager(store, c.msgBus, evolverPlans, creator, logger), nil
}

// taskCreatorAdapterForEvolver returns the shared TaskCreator adapter (or nil
// when the task registry is absent), so the evolver's PlanManager gets the
// same task-synthesis capability as the human one.
func (c *Components) taskCreatorAdapterForEvolver() plan.TaskCreator {
	if c == nil || c.TaskRegistry == nil {
		return nil
	}
	return newTaskCreatorAdapter(c.TaskRegistry)
}
