package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/conflict"
	"github.com/caimlas/meept/internal/memory/l0"
	"github.com/caimlas/meept/internal/queue"
)

// l0ChatterAdapter adapts llm.Chatter to memory.DistillLLM: a distill job
// sends one prompt and consumes the raw body, mirroring the ambient
// classifier adapter's shape (single user message, low temperature,
// DisableThinking — mechanical extraction gains nothing from reasoning, and
// the LFM2.5 <think> template otherwise fights JSON parsing).
type l0ChatterAdapter struct {
	chatter llm.Chatter
}

var _ memory.DistillLLM = (*l0ChatterAdapter)(nil)

// l0DirFor returns the L0 directory for a memory data dir (frozen layout:
// <dataDir>/l0).
func l0DirFor(dataDir string) string {
	return filepath.Join(dataDir, "l0")
}

func newDistillLLMAdapter(chatter llm.Chatter) memory.DistillLLM {
	if chatter == nil {
		return nil
	}
	return &l0ChatterAdapter{chatter: chatter}
}

func (a *l0ChatterAdapter) DistillComplete(ctx context.Context, prompt string) ([]byte, error) {
	resp, err := a.chatter.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleUser, Content: prompt},
	}, llm.WithTemperature(0.2), llm.DisableThinking())
	if err != nil {
		return nil, fmt.Errorf("distill chatter: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("distill chatter returned nil response")
	}
	return []byte(resp.Content), nil
}

// conflictChatterAdapter adapts llm.Chatter to the conflict package's LLM seam
// (memory.DistillLLM twin): identical single-prompt low-temperature shape, so
// the memory_model slot serves both L0 distill and conflict detection.
type conflictChatterAdapter struct {
	chatter llm.Chatter
}

var _ conflict.LLM = (*conflictChatterAdapter)(nil)

func newConflictLLMAdapter(chatter llm.Chatter) conflict.LLM {
	if chatter == nil {
		return nil
	}
	return &conflictChatterAdapter{chatter: chatter}
}

func (a *conflictChatterAdapter) Complete(ctx context.Context, prompt string) ([]byte, error) {
	resp, err := a.chatter.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleUser, Content: prompt},
	}, llm.WithTemperature(0.2), llm.DisableThinking())
	if err != nil {
		return nil, fmt.Errorf("conflict chatter: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("conflict chatter returned nil response")
	}
	return []byte(resp.Content), nil
}

// conflictWiring returns the DistillWorker's conflict-detection wiring when
// the opt-in memory.conflict.enabled is set, or nil otherwise. This is what
// makes the memory.conflict.* config actually reach a live detector: before
// this, DistillWorkerConfig.Conflict was never constructed anywhere, so the
// flag was a no-op and memory.listConflicts could only ever be empty
// (bughunt M1).
func conflictWiring(memCfg config.MemoryConfig, chatter llm.Chatter, logger *slog.Logger) *memory.ConflictWiring {
	if !memCfg.Conflict.Enabled {
		return nil
	}
	topK := memCfg.Conflict.TopK
	if topK <= 0 {
		topK = conflict.DefaultRecallPerCandidate
	}
	maxBatch := memCfg.Conflict.MaxBatch
	if maxBatch <= 0 {
		maxBatch = conflict.DefaultMaxBatch
	}
	return &memory.ConflictWiring{
		Enabled:  true,
		TopK:     topK,
		MaxBatch: maxBatch,
		LLM:      newConflictLLMAdapter(chatter),
		Logger:   logger.With("component", "conflict-detection"),
	}
}

// l0Components holds the L0 recording pieces the turn-terminal memory-capture
// subscriber needs (plan docs/plans/20261013-turn-terminal-capture/plan.md —
// the session-end l0_record hook is retired; capture moved to the
// turn.terminal subscriber).
type l0Components struct {
	recorder *l0.Recorder
	enqueuer memory.JobEnqueuer
	dataDir  string
}

// buildL0Components wires the L0 raw-conversation tier's recording pieces when
// memory.l0_enabled is set: the recorder, retention pruning, and the enqueuer
// handle. Returns nil when L0 is disabled (memory manager nil, flag off, or no
// data dir) — no recorder, no prune.
func buildL0Components(memoryMgr *memory.Manager, enqueuer memory.JobEnqueuer, memCfg config.MemoryConfig, logger *slog.Logger) *l0Components {
	if memoryMgr == nil || !memCfg.L0Enabled {
		return nil
	}
	dataDir := memoryMgr.DataDir()
	if dataDir == "" {
		logger.Warn("l0 enabled but memory manager has no data dir; l0 disabled")
		return nil
	}
	recorder := l0.NewRecorder(dataDir, nil)
	l0Dir := l0DirFor(dataDir)

	// Retention: whole daily files past l0_retention_days are pruned on
	// the consolidation tick (no new ticker). A non-positive window
	// disables pruning entirely.
	if memCfg.L0RetentionDays > 0 {
		days := memCfg.L0RetentionDays
		memoryMgr.SetL0Prune(func(ctx context.Context) {
			removed, err := l0.Prune(l0Dir, days)
			if err != nil {
				logger.Warn("l0 prune failed", "error", err)
				return
			}
			if removed > 0 {
				logger.Info("l0 prune removed daily files", "count", removed, "retention_days", days)
			}
		})
	}

	return &l0Components{recorder: recorder, enqueuer: enqueuer, dataDir: dataDir}
}

// l0cRecorder / l0cEnqueuer are nil-safe accessors: a nil l0Components means
// L0 is disabled and the memory-capture subscriber gets a nil recorder/enqueuer
// (nil-safe pieces in WireMemoryCapture).
func l0cRecorder(l0c *l0Components) *l0.Recorder {
	if l0c == nil {
		return nil
	}
	return l0c.recorder
}

func l0cEnqueuer(l0c *l0Components) memory.JobEnqueuer {
	if l0c == nil {
		return nil
	}
	return l0c.enqueuer
}

// wireL0Distill wires the L0 distillation worker when memory.l0_enabled is
// set, for queue-layer registration (components.go appends it to the worker
// pool's processor set). Turn recording itself no longer happens here: the
// session-end l0_record hook was retired in favor of the turn-terminal
// memory-capture subscriber (memory_capture.go), which captures EVERY turn
// shape — direct, dispatched/task-scoped, relayed — where the old
// hook-registry seam missed task-scoped loops entirely
// (.github/ISSUES/task-loops-no-hook-registry-l0-ambient.md). Memory-manager
// nil (memory disabled) stays a no-op — no worker.
func wireL0Distill(l0c *l0Components, memoryMgr *memory.Manager, chatter llm.Chatter, memCfg config.MemoryConfig, logger *slog.Logger) *memory.DistillWorker {
	if l0c == nil || memoryMgr == nil || !memCfg.L0Enabled {
		return nil
	}
	dataDir := l0c.dataDir

	worker := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager: memoryMgr,
		LLM:     newDistillLLMAdapter(chatter),
		DataDir: dataDir,
		// Enqueuer: L1 jobs chain one L2 scene job per success. The
		// persistent queue (same instance as the enqueue side) satisfies
		// memory.JobEnqueuer.
		Enqueuer: l0c.enqueuer,
		// Conflict: live, configured detector when memory.conflict.enabled
		// is set (bughunt M1); nil otherwise keeps the pre-detector path.
		Conflict: conflictWiring(memCfg, chatter, logger),
		Logger:   logger.With("component", "distill-worker"),
	})
	logger.Info("l0 distill wired", "data_dir", dataDir, "retention_days", memCfg.L0RetentionDays)
	return worker
}

// appendDistillProcessor composes the distill processor with the daemon's
// agent job processor: distill job types route to the DistillWorker, all
// other jobs fall through to the agent processor. Compiled into a
// worker.JobProcessor for the queue-layer worker pool.
type distillRouter struct {
	agent   workerJobProcessor
	distill *memory.DistillWorker
}

// workerJobProcessor mirrors internal/worker.JobProcessor locally so this
// file need not import internal/worker (the daemon package already does —
// the alias just documents the seam).
type workerJobProcessor interface {
	Process(ctx context.Context, job *queue.Job) (any, error)
}

var _ workerJobProcessor = (*distillRouter)(nil)

func (r *distillRouter) Process(ctx context.Context, job *queue.Job) (any, error) {
	if job != nil && r.distill != nil {
		switch job.Type {
		case memory.MemoryJobL1Extract, memory.MemoryJobL2Scene:
			return r.distill.Process(ctx, job)
		}
	}
	return r.agent.Process(ctx, job)
}
