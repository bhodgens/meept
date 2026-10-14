package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/agent"
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

// wireL0Distill wires the L0 raw-conversation tier (tree 01 leaf L3) when
// memory.l0_enabled is set. Three parts, all best-effort:
//
//  1. A session-end hook on the agent loop appends the turn's dialogue to
//     <dataDir>/l0/YYYY-MM-DD.jsonl (append errors logged, never fail the
//     turn) and enqueues ONE memory.l1_extract job per turn with the
//     payload position = the marker-free start (""). The worker's own
//     l0_progress marker (memory-distill side, advanced only on full
//     success) is what suppresses duplicate/re-run work — the enqueue
//     itself stays marker-free so a killed worker's window re-runs.
//  2. l0.Prune is wired onto the Manager's consolidator so retention runs
//     on the existing consolidation ticker (no new goroutine).
//  3. A DistillWorker is returned for the queue-layer registration
//     (components.go appends it to the worker pool's processor set).
//
// The hook fires per TURN (not only when ambient extraction is enabled):
// L0 recording is the raw tier and must not silently depend on the
// epistemic extraction gate. Memory-manager nil (memory disabled) is a
// no-op — no recorder, no prune, no worker.
func wireL0Distill(agentLoop *agent.AgentLoop, memoryMgr *memory.Manager, enqueuer memory.JobEnqueuer, chatter llm.Chatter, memCfg config.MemoryConfig, logger *slog.Logger) *memory.DistillWorker {
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

	if agentLoop != nil {
		agentLoop.HookRegistry().RegisterSessionEndHook("l0_record", agent.HookPriorityMonitor+2, &l0RecordHook{
			recorder: recorder,
			l0Dir:    l0Dir,
			enqueuer: enqueuer,
			logger:   logger.With("component", "l0-record"),
		})
	}

	worker := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager: memoryMgr,
		LLM:     newDistillLLMAdapter(chatter),
		DataDir: dataDir,
		// Enqueuer: L1 jobs chain one L2 scene job per success. The
		// persistent queue (same instance as the enqueue side) satisfies
		// memory.JobEnqueuer.
		Enqueuer: enqueuer,
		// Conflict: live, configured detector when memory.conflict.enabled
		// is set (bughunt M1); nil otherwise keeps the pre-detector path.
		Conflict: conflictWiring(memCfg, chatter, logger),
		Logger:   logger.With("component", "distill-worker"),
	})
	logger.Info("l0 distill wired", "data_dir", dataDir, "retention_days", memCfg.L0RetentionDays)
	return worker
}

// l0RecordHook is the session-end hook that persists the turn's dialogue as
// L0 lines and kicks off async distillation. Implements agent.SessionEndHook
// (the same seam as memoryFactExtractHook). Best-effort: append/enqueue
// errors are logged and never propagate — a failed L0 write must not fail
// the user's turn.
type l0RecordHook struct {
	recorder *l0.Recorder
	l0Dir    string // <dataDir>/l0 — read side for window positioning
	enqueuer memory.JobEnqueuer
	logger   *slog.Logger
}

func (h *l0RecordHook) OnSessionEnd(ctx context.Context, state agent.SessionLifecycleState, result agent.SessionLifecycleResult) error {
	if h.recorder == nil {
		return nil
	}
	sessionID := state.SessionID
	if sessionID == "" {
		return nil
	}
	if result.UserMessage != "" {
		if err := h.recorder.Append(sessionID, "user", result.UserMessage, nil); err != nil {
			h.logger.Warn("l0 append failed", "session_id", sessionID, "error", err)
		}
	}
	if result.AssistantMessage != "" {
		if err := h.recorder.Append(sessionID, "assistant", result.AssistantMessage, nil); err != nil {
			h.logger.Warn("l0 append failed", "session_id", sessionID, "error", err)
		}
	}

	// Async distill: one L1 job per turn with new L0 lines. since_msg_id is
	// today's last record BEFORE this append ("" on the day's first turn):
	// the window is "today's lines after that position". The worker's
	// l0_progress marker (advanced only on full success) suppresses
	// duplicate/re-run work — when the marker already sits at the payload
	// position the run short-circuits with status "suppressed", and on
	// other shapes the worker re-reads only the undistilled tail. Enqueue
	// failure is logged, never fails the turn.
	if h.enqueuer == nil || result.UserMessage == "" && result.AssistantMessage == "" {
		return nil
	}
	date := time.Now().Format("2006-01-02")
	l0Path := h.recorder.DatePath(date)
	sinceID := ""
	if recs, err := l0.ReadSession(h.l0Dir, date, sessionID); err == nil && len(recs) > 1 {
		// >1: the just-appended line is the last one; everything before it
		// was already covered by earlier turns' jobs.
		sinceID = recs[len(recs)-2].ID
	}
	payload := memory.L1ExtractPayload{
		SessionID:  sessionID,
		L0Path:     l0Path,
		SinceMsgID: sinceID,
	}
	if _, err := memory.EnqueueL1(ctx, h.enqueuer, payload); err != nil {
		h.logger.Warn("l1 enqueue failed", "session_id", sessionID, "error", err)
	}
	return nil
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
