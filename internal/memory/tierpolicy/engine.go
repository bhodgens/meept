// Package tierpolicy implements the memory-tier policy engine (contract C5
// of the 20261031-context-memory-engine plan, tree 02 leaf 02): it
// continuously evaluates tier token loads against ceilings and emits
// eviction/compression actions, and executes them OFF the chat path.
//
// Design rules pinned by the parent plan and findings-memory-tiers.md:
//
//   - Two-threshold band shape (R-B2, MemGPT precedent): CompressOverRatio
//     0.7-ish soft band starts distillation/compression of the coldest
//     segments; EvictOverRatio 0.9-ish hard band ranks with
//     memory.PlanUsefulEviction usefulness + recency tiebreak and evicts a
//     batch sized ~50% of the overage. Compress precedes evict (R-B4):
//     compression is recoverable; eviction of un-distilled content is the
//     last resort. Defaults here are 0.8/0.9 per the leaf contract.
//   - Batching + cost bounds (R-B5): per pass min batch 5, max batch 100,
//     oldest first, skip (no-op sentinel) if under min. A per-tier
//     watermark (PassRecord) records the outcome of every pass, including
//     empty ones; a tier under the compress threshold costs ZERO Deps
//     calls (no-op sentinel, zero-cost when disabled or under ceiling).
//   - Compression REUSES the existing async distill pipeline: Execute
//     enqueues memory.l1_extract jobs through the same JobEnqueuer seam
//     memory.JobEnqueuer/queue.PersistentQueue already provide
//     (internal/memory/distill_async.go). NEVER runs LLM work inline and
//     never blocks the tick on it.
//   - Eviction REUSES memory.PlanUsefulEviction ranking via the same
//     inputs the consolidation pass uses (candidates + batched net votes,
//     internal/memory/consolidation.go ~:537-566). It is called through
//     the Deps interfaces below - the engine NEVER constructs a
//     Consolidator and NEVER imports internal/agent. Single-start
//     discipline follows the Consolidator tickerActive pattern
//     (consolidation.go:60,96): a started engine refuses a second Start
//     with ErrAlreadyStarted; Stop allows a later restart.
//
// Opt-in policy (docs/agents/opt-in-defaults.md): every knob ships
// default-off. TiersConfig.Enabled defaults false; ResolveTiersConfig
// supplies 0.9/0.8/300s ratios+interval only once Enabled flips on. The
// proposal to flip automated eviction to default-on lives in
// docs/agents/opt-in-defaults.md ("Automated memory-tier eviction") as a
// PRODUCT DECISION PROPOSAL - it is deliberately not implemented here.
//
// ASCII only. No internal/agent imports; no second Consolidator.
package tierpolicy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/working"
	"github.com/caimlas/meept/internal/queue"
)

// Action kinds emitted by Evaluate.
const (
	// KindCompress distills the target records via the async distill
	// pipeline (memory.l1_extract enqueue; never inline LLM work).
	KindCompress = "compress"
	// KindEvict deletes the target records from their store.
	KindEvict = "evict"
)

// Batch bounds and defaults (findings-memory-tiers.md R-B5, R-B2).
const (
	// DefaultMinBatch is the R-B5 minimum batch: a pass that cannot fill
	// it is skipped with a no-op sentinel (zero LLM calls).
	DefaultMinBatch = 5
	// DefaultMaxBatch is the R-B5 maximum batch per pass.
	DefaultMaxBatch = 100
	// DefaultEvictOverRatio is the hard eviction band (R-B2, MemGPT
	// 0.9 hard threshold).
	DefaultEvictOverRatio = 0.9
	// DefaultCompressOverRatio is the soft compression band (R-B2,
	// MemGPT 0.7 soft threshold; leaf contract pins 0.8).
	DefaultCompressOverRatio = 0.8
	// DefaultInterval is the background tick interval when unset (300s).
	DefaultInterval = 300 * time.Second
	// evictCandidateLimit mirrors the consolidator's GetOldMemories page
	// size (consolidation.go:528) so eviction ranking sees the same
	// candidate population.
	evictCandidateLimit = 500
	// floorPct mirrors ResolveUsefulEviction's default floor.
	floorPct = 0.05
)

// Sentinel outcomes recorded in the per-tier watermark (PassRecord).
const (
	OutcomeNoop          = "noop"
	OutcomeCompress      = "compress"
	OutcomeEvict         = "evict"
	OutcomeCompressEvict = "compress+evict"
)

// TierState is one tier's load snapshot (contract C5). Tokens is the
// trigger unit for prompt-reaching tiers, Items for semantic sets
// (R-B3); the engine evaluates on Tokens and carries Items for reasons.
type TierState struct {
	Tier    string
	Tokens  int
	Items   int
	Ceiling int
}

// Action is one policy decision (contract C5). Reason is machine-readable
// ("tier=<tier>;tokens=<n>;ceiling=<n>;...").
type Action struct {
	Kind      string
	TargetIDs []string
	Reason    string
}

// CandidateSource supplies eviction/compression candidates for a tier,
// OLDEST FIRST. The daemon adapts memory.Manager.GetRecent /
// EpisodicMemory.GetOldMemories to this; tests stub it.
type CandidateSource interface {
	Candidates(ctx context.Context, tier string, limit int) ([]memory.MemoryResult, error)
}

// VoteSource supplies batched net votes keyed by memory ID - the same
// input shape memory.Manager.NetVotes feeds PlanUsefulEviction in the
// consolidation pass (consolidation.go:549).
type VoteSource interface {
	NetVotes(ctx context.Context, ids []string) (map[string]int, error)
}

// Deleter applies evictions (EpisodicMemory.DeleteByIDs shape).
type Deleter interface {
	DeleteByIDs(ctx context.Context, ids []string) (int, error)
}

// JobEnqueuer is the queue seam for compression. Satisfied by
// *queue.PersistentQueue exactly like memory.JobEnqueuer
// (distill_async.go:58); jobs use the REAL memory.MemoryJobL1Extract
// job type so the existing distill worker consumes them.
type JobEnqueuer interface {
	Enqueue(ctx context.Context, job *queue.Job) error
}

// StateSource supplies the current tier states for the background tick.
// The daemon adapts working.Store (LiveTokens/QueuedTurns) and store
// stats to this; tests stub it.
type StateSource interface {
	TierStates(ctx context.Context) ([]TierState, error)
}

// Deps carries the engine's collaborators. Interfaces are defined
// in-package on purpose (contract C5): the engine never constructs a
// Consolidator and never imports internal/agent. Nil members degrade
// honestly: Candidates/Votes nil makes Evaluate emit band-noops;
// Deletes/Enqueuer nil makes Execute report an error per action.
type Deps struct {
	Candidates CandidateSource
	Votes      VoteSource
	Deletes    Deleter
	Enqueuer   JobEnqueuer
	States     StateSource
	// DemotionClaims supplies the semantic-claim candidate pool for
	// demotion planning (leaf 03, contract C6). Optional.
	DemotionClaims ClaimSource
	// Demotion applies the two-step demotion (episodic copy + demoted
	// flag). Optional; nil makes PlanDemotions an honest noop and
	// Execute report a per-action error.
	Demotion DemotionApplier
	// DemotedCheck is the optional standalone idempotency detector
	// (R-B7); when nil, demotion state is read from claim metadata.
	DemotedCheck DemotedDetector
	// CountTokens estimates a record's token cost for batch sizing.
	// Nil falls back to the chars/4 estimator used by the working-tier
	// tests. Optional.
	CountTokens func(string) int
	// Logger is optional; nil falls back to slog.Default().
	Logger *slog.Logger
}

// Config configures the engine (contract C5). Zero value is a disabled,
// inert engine.
type Config struct {
	// WorkingConfig is the working-tier ring configuration; its
	// MaxTokens is the default working-tier ceiling when a TierState
	// carries Ceiling == 0.
	WorkingConfig working.Config
	// EvictOverRatio is the hard eviction band start (fraction of
	// Ceiling). <= 0 or >= 1 resolves to DefaultEvictOverRatio.
	EvictOverRatio float64
	// CompressOverRatio is the soft compression band start. <= 0 or
	// >= 1 resolves to DefaultCompressOverRatio.
	CompressOverRatio float64
	// Interval is the background tick period. <= 0 resolves to
	// DefaultInterval.
	Interval time.Duration
	// Enabled gates everything. Default FALSE (opt-in-defaults rule):
	// a disabled engine's Evaluate returns nil without touching Deps
	// and Start refuses with ErrDisabled.
	Enabled bool
	// DryRun makes Execute report would-be counts without mutating
	// anything (T2). Default false.
	DryRun bool
	// MinBatch/MaxBatch are the R-B5 batch bounds; <= 0 resolves to the
	// defaults above.
	MinBatch int
	MaxBatch int
	// Demotion carries the [memory.tiers.demotion] thresholds (leaf 03,
	// contract C6). Zero/negative fields resolve to the documented
	// defaults via ResolveDemotionThresholds (30d staleness window,
	// 0.1 usefulness floor).
	Demotion DemotionConfig
}

// Engine evaluates tier states into actions and executes them. Safe for
// concurrent use. Start/Stop follow the Consolidator single-start
// discipline (restartable, never double-ticking).
type Engine struct {
	cfg    Config
	deps   Deps
	logger *slog.Logger

	// mu guards tickerActive/stopChan (Consolidator pattern,
	// consolidation.go:58-66) and the watermark map.
	mu           sync.Mutex
	tickerActive bool
	stopChan     chan struct{}
	watermarks   map[string]PassRecord
	now          func() time.Time
}

// PassRecord is the per-tier watermark: the outcome of the most recent
// Evaluate pass for that tier. Outcome "noop" on an under-ceiling tier
// is the no-op sentinel - the pass cost zero Deps calls (R-B5).
type PassRecord struct {
	Tier    string
	At      time.Time
	Outcome string
	Tokens  int
	Ceiling int
}

// Start errors.
var (
	// ErrAlreadyStarted is returned by Start when the background tick is
	// already running (single-start guarantee, T4).
	ErrAlreadyStarted = errors.New("tierpolicy: engine already started")
	// ErrDisabled is returned by Start on a disabled engine.
	ErrDisabled = errors.New("tierpolicy: engine disabled (memory.tiers.enabled=false)")
)

// New constructs an engine. It NEVER constructs a Consolidator and never
// performs I/O; all side effects go through deps.
func New(cfg Config, deps Deps) *Engine {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if cfg.EvictOverRatio <= 0 || cfg.EvictOverRatio >= 1 {
		cfg.EvictOverRatio = DefaultEvictOverRatio
	}
	if cfg.CompressOverRatio <= 0 || cfg.CompressOverRatio >= 1 {
		cfg.CompressOverRatio = DefaultCompressOverRatio
	}
	// The compress band must sit inside the evict band (R-B4 ordering
	// depends on it); clamp defensively.
	if cfg.CompressOverRatio > cfg.EvictOverRatio {
		cfg.CompressOverRatio = cfg.EvictOverRatio
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.MinBatch <= 0 {
		cfg.MinBatch = DefaultMinBatch
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = DefaultMaxBatch
	}
	return &Engine{
		cfg:        cfg,
		deps:       deps,
		logger:     deps.Logger,
		watermarks: make(map[string]PassRecord),
		now:        time.Now,
	}
}

// SetClock injects the wall-clock source (deterministic tests). Nil and
// typed-nil functions are ignored (repo setter guard rule).
func (e *Engine) SetClock(now func() time.Time) {
	if e == nil || isNilFunc(now) {
		return
	}
	e.now = now
}

func isNilFunc(f any) bool {
	if f == nil {
		return true
	}
	v := reflect.ValueOf(f)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// LastPass returns the watermark for a tier (false when never evaluated).
func (e *Engine) LastPass(tier string) (PassRecord, bool) {
	if e == nil {
		return PassRecord{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.watermarks[tier]
	return rec, ok
}

func (e *Engine) setPass(rec PassRecord) {
	e.mu.Lock()
	e.watermarks[rec.Tier] = rec
	e.mu.Unlock()
}

func (e *Engine) countTokens(s string) int {
	if e.deps.CountTokens != nil {
		return e.deps.CountTokens(s)
	}
	return (len(s) + 3) / 4
}

// Evaluate turns tier states into actions (contract C5). Tiers are
// processed in input order (deterministic); within a tier the compress
// action precedes the evict action (R-B4). A disabled engine returns
// nil. A tier at or under the compress threshold records the noop
// sentinel and performs ZERO Deps calls.
func (e *Engine) Evaluate(ctx context.Context, states []TierState) ([]Action, error) {
	if e == nil {
		return nil, errors.New("tierpolicy: nil engine")
	}
	if !e.cfg.Enabled {
		return nil, nil
	}
	var (
		actions []Action
		errs    []error
	)
	// Demotion planning runs FIRST and demote actions are PREPENDED so
	// every returned plan orders demote -> compress -> evict (leaf T4,
	// R-B4 extended: content-preserving reclamation precedes destructive
	// actions). Demotions are planned against the claim pool, not the
	// tier bands: any stale/low-utility claim demotes regardless of the
	// ceiling posture, while band actions stay tier-scoped below.
	demoteActs, err := e.PlanDemotions(ctx, nil)
	if err != nil {
		errs = append(errs, err)
	}
	actions = append(actions, demoteActs...)
	for _, st := range states {
		rec := PassRecord{Tier: st.Tier, At: e.now(), Tokens: st.Tokens, Ceiling: st.Ceiling}
		compressAt, evictAt := e.thresholds(st)
		// Guard: a meaningless ceiling (unset) can never be "over".
		if st.Ceiling <= 0 || float64(st.Tokens) <= compressAt {
			rec.Outcome = OutcomeNoop // no-op sentinel; zero Deps calls
			e.setPass(rec)
			continue
		}
		if float64(st.Tokens) <= evictAt {
			// Middle band: compress the overage only (R-B2 soft band).
			overage := float64(st.Tokens) - compressAt
			act, err := e.planCompress(ctx, st, overage)
			if err != nil {
				errs = append(errs, err)
			}
			if act == nil {
				rec.Outcome = OutcomeNoop
			} else {
				actions = append(actions, *act)
				rec.Outcome = OutcomeCompress
			}
			e.setPass(rec)
			continue
		}
		// Hard band (R-B2/R-B4): compress FIRST, down to the evict
		// threshold; evict only what compression cannot fit, in a
		// batch sized ~50% of the overage.
		overage := float64(st.Tokens) - evictAt
		rec.Outcome = OutcomeNoop
		compressAct, err := e.planCompress(ctx, st, overage)
		if err != nil {
			errs = append(errs, err)
		}
		if compressAct != nil {
			actions = append(actions, *compressAct)
			rec.Outcome = OutcomeCompress
		}
		evictAct, err := e.planEvict(ctx, st, overage/2)
		if err != nil {
			errs = append(errs, err)
		}
		if evictAct != nil {
			actions = append(actions, *evictAct)
			if rec.Outcome == OutcomeCompress {
				rec.Outcome = OutcomeCompressEvict
			} else {
				rec.Outcome = OutcomeEvict
			}
		}
		e.setPass(rec)
	}
	return actions, errors.Join(errs...)
}

// thresholds returns the absolute compress/evict token levels for a
// state. A zero Ceiling falls back to the working-ring MaxTokens.
func (e *Engine) thresholds(st TierState) (compressAt, evictAt float64) {
	ceiling := float64(st.Ceiling)
	if ceiling <= 0 {
		ceiling = float64(e.cfg.WorkingConfig.MaxTokens)
	}
	return e.cfg.CompressOverRatio * ceiling, e.cfg.EvictOverRatio * ceiling
}

// planCompress selects the oldest candidates (R-B5 oldest-first) worth
// ~targetTokens of reclamation, bounded by MaxBatch, skipping (nil, no
// action) when the selection is under MinBatch.
func (e *Engine) planCompress(ctx context.Context, st TierState, targetTokens float64) (*Action, error) {
	if e.deps.Candidates == nil {
		return nil, nil
	}
	cands, err := e.deps.Candidates.Candidates(ctx, st.Tier, e.cfg.MaxBatch)
	if err != nil {
		return nil, fmt.Errorf("tierpolicy: candidates for tier %q: %w", st.Tier, err)
	}
	var (
		ids  []string
		toks int
	)
	for _, c := range cands {
		if len(ids) >= e.cfg.MaxBatch {
			break
		}
		ids = append(ids, c.Memory.ID)
		toks += e.countTokens(c.Memory.Content)
		if float64(toks) >= targetTokens {
			break
		}
	}
	if len(ids) < e.cfg.MinBatch {
		return nil, nil // R-B5: skip if under min batch
	}
	return &Action{
		Kind:      KindCompress,
		TargetIDs: ids,
		Reason:    reason(st, targetTokens),
	}, nil
}

// planEvict ranks candidates with the SAME PlanUsefulEviction inputs the
// consolidation pass uses (candidates + batched net votes,
// consolidation.go:549-551) and selects a batch worth ~targetTokens
// (~50% of the hard-band overage, R-B2): harmful IDs always, then the
// floor, then survivors in ascending usefulness (PlanUsefulEviction's
// Survivors order reversed), bounded by MaxBatch. Under MinBatch with
// nothing harmful, the pass is skipped (nil).
func (e *Engine) planEvict(ctx context.Context, st TierState, targetTokens float64) (*Action, error) {
	if e.deps.Candidates == nil || e.deps.Votes == nil {
		return nil, nil
	}
	cands, err := e.deps.Candidates.Candidates(ctx, st.Tier, evictCandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("tierpolicy: candidates for tier %q: %w", st.Tier, err)
	}
	if len(cands) == 0 {
		return nil, nil
	}
	ids := make([]string, len(cands))
	for i, c := range cands {
		ids[i] = c.Memory.ID
	}
	votes, err := e.deps.Votes.NetVotes(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("tierpolicy: net votes for tier %q: %w", st.Tier, err)
	}
	now := e.now()
	plan := memory.PlanUsefulEviction(cands, votes, now, memory.UsefulEvictionConfig{
		Enabled:  true,
		FloorPct: floorPct,
		Weights:  memory.DefaultWeights(),
	})
	// Pool order: harmful (evict regardless), floor (worst-first),
	// survivors reversed to ascending usefulness.
	pool := make([]string, 0, len(plan.Harmful)+len(plan.Floor)+len(plan.Survivors))
	pool = append(pool, plan.Harmful...)
	pool = append(pool, plan.Floor...)
	for i := len(plan.Survivors) - 1; i >= 0; i-- {
		pool = append(pool, plan.Survivors[i].Memory.ID)
	}
	byID := make(map[string]memory.MemoryResult, len(cands))
	for _, c := range cands {
		byID[c.Memory.ID] = c
	}
	var (
		selected []string
		toks     int
		harmful  bool
	)
	for _, id := range pool {
		if len(selected) >= e.cfg.MaxBatch {
			break
		}
		selected = append(selected, id)
		if votes[id] <= memory.HarmfulVoteThreshold {
			harmful = true
		}
		if c, ok := byID[id]; ok {
			toks += e.countTokens(c.Memory.Content)
		}
		if float64(toks) >= targetTokens && len(selected) >= e.cfg.MinBatch {
			break
		}
	}
	if len(selected) < e.cfg.MinBatch && !harmful {
		return nil, nil // R-B5: skip if under min batch
	}
	return &Action{
		Kind:      KindEvict,
		TargetIDs: selected,
		Reason:    reason(st, targetTokens),
	}, nil
}

// reason builds the machine-readable reason string (contract C5).
func reason(st TierState, targetTokens float64) string {
	return fmt.Sprintf("tier=%s;tokens=%d;ceiling=%d;items=%d;target_tokens=%.0f",
		st.Tier, st.Tokens, st.Ceiling, st.Items, targetTokens)
}

// Start launches the background tick loop. Single-start guarantee: a
// second call while running returns ErrAlreadyStarted (T4); after Stop a
// restart is allowed (Consolidator tickerActive pattern,
// consolidation.go:933-965). Disabled engines refuse with ErrDisabled -
// the daemon wiring checks Enabled first, this is the belt to that
// braces. The loop stops on ctx cancel or Stop.
func (e *Engine) Start(ctx context.Context) error {
	if e == nil {
		return errors.New("tierpolicy: nil engine")
	}
	if !e.cfg.Enabled {
		return ErrDisabled
	}
	e.mu.Lock()
	if e.tickerActive {
		e.mu.Unlock()
		return ErrAlreadyStarted
	}
	e.tickerActive = true
	e.stopChan = make(chan struct{})
	stopCh := e.stopChan
	e.mu.Unlock()

	go e.loop(ctx, stopCh)
	return nil
}

func (e *Engine) loop(ctx context.Context, stopCh chan struct{}) {
	ticker := time.NewTicker(e.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-ticker.C:
			e.tickOnce(ctx)
		}
	}
}

// tickOnce runs one evaluate+execute pass off the chat path. Errors are
// logged, never fatal - the next tick retries.
func (e *Engine) tickOnce(ctx context.Context) {
	if e.deps.States == nil {
		return
	}
	states, err := e.deps.States.TierStates(ctx)
	if err != nil {
		e.logger.Warn("tierpolicy: tier state snapshot failed", "error", err)
		return
	}
	actions, err := e.Evaluate(ctx, states)
	if err != nil {
		e.logger.Warn("tierpolicy: evaluate pass had errors", "error", err)
	}
	if len(actions) == 0 {
		return
	}
	rep, err := e.Execute(ctx, actions)
	if err != nil {
		e.logger.Warn("tierpolicy: execute pass failed", "error", err)
		return
	}
	e.logger.Info("tierpolicy: pass complete",
		"evict_requested", rep.EvictRequested,
		"evictions_deleted", rep.EvictionsDeleted,
		"compress_requested", rep.CompressRequested,
		"compress_enqueued", rep.CompressionEnqueued,
		"demote_requested", rep.DemoteRequested,
		"demoted", rep.Demoted,
		"dry_run", rep.DryRun,
		"errors", len(rep.Errors),
	)
}

// Stop stops the background tick. Safe to call multiple times and
// before any Start; a later Start restarts the loop.
func (e *Engine) Stop() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.tickerActive {
		return
	}
	e.tickerActive = false
	close(e.stopChan)
}

// Running reports whether the background tick is active.
func (e *Engine) Running() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tickerActive
}

// TiersConfig is the user-facing [memory.tiers] knob set. It is a plain
// struct on purpose: the config-schema plumbing (internal/config/schema.go
// MemoryConfig.Tiers) is owned by the orchestrator merge; this package
// owns only the shape, its defaults, and the resolver so schema plumbing
// stays mechanical. ALL KNOBS OPT-IN: Enabled defaults false
// (docs/agents/opt-in-defaults.md).
type TiersConfig struct {
	// Enabled turns on the automated evict/compress policy engine.
	// Default false.
	Enabled bool
	// EvictOverRatio is the hard eviction band (fraction of tier
	// ceiling). 0/unset resolves to DefaultEvictOverRatio (0.9) once
	// Enabled.
	EvictOverRatio float64
	// CompressOverRatio is the soft compression band. 0/unset resolves
	// to DefaultCompressOverRatio (0.8) once Enabled.
	CompressOverRatio float64
	// IntervalSeconds is the background tick period. 0/unset resolves
	// to 300 once Enabled.
	IntervalSeconds int
}

// ResolveTiersConfig applies defaults to unset fields. A zero-value
// config stays disabled; an enabled-but-unset config resolves to the
// R-B2/R-B5 defaults (0.9 / 0.8 / 300s). Mirrors
// NormalizeTierPolicyDefaults (llm.tiers, leaf 01) so both tier
// packages behave identically at the load boundary.
func ResolveTiersConfig(cfg TiersConfig) TiersConfig {
	if !cfg.Enabled {
		return TiersConfig{Enabled: false}
	}
	if cfg.EvictOverRatio <= 0 || cfg.EvictOverRatio >= 1 {
		cfg.EvictOverRatio = DefaultEvictOverRatio
	}
	if cfg.CompressOverRatio <= 0 || cfg.CompressOverRatio >= 1 {
		cfg.CompressOverRatio = DefaultCompressOverRatio
	}
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = int(DefaultInterval / time.Second)
	}
	return cfg
}

// ToEngineConfig maps resolved TiersConfig onto the engine Config.
func (t TiersConfig) ToEngineConfig(workCfg working.Config) Config {
	return Config{
		WorkingConfig:     workCfg,
		EvictOverRatio:    t.EvictOverRatio,
		CompressOverRatio: t.CompressOverRatio,
		Interval:          time.Duration(t.IntervalSeconds) * time.Second,
		Enabled:           t.Enabled,
	}
}
