package session

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// EmbeddingProvider generates embeddings for text strings.
// Re-declared here (mirroring memory.EmbeddingProvider and vector.Provider)
// to avoid coupling the session package to either of those packages.
// Any concrete provider satisfying this interface (e.g. vector.Provider)
// can be passed to NewEmbeddingWorker.
type EmbeddingProvider interface {
	// GenerateEmbeddings produces a batch of embeddings, one per input text.
	// The returned slice length must match the input length.
	GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbeddingWorkerConfig configures an EmbeddingWorker.
type EmbeddingWorkerConfig struct {
	Batch    int           // maintenance-mode messages per tick, default 20
	Interval time.Duration // maintenance-mode tick interval, default 60s

	// CatchUpBatch is the batch size used when the unembedded-message queue is
	// large. Default 200. When a tick returns at least CatchUpBatch results the
	// worker assumes more messages remain and switches to CatchUpInterval.
	CatchUpBatch int
	// CatchUpInterval is the tick interval used in catch-up mode. Default 5s.
	CatchUpInterval time.Duration
	// MaintenanceThreshold is the queue size above which catch-up mode kicks in
	// on the next tick. Default 50. Kept for configuration completeness; the
	// primary catch-up trigger is the previous tick saturating CatchUpBatch.
	MaintenanceThreshold int
}

// EmbeddingWorker periodically embeds session messages that have no embedding
// yet, using the configured EmbeddingProvider. It is nil-safe: constructors
// return nil when a required dependency is missing, and the daemon checks for
// nil before calling Start.
//
// The worker has two modes:
//   - maintenance (default): runs every Interval with Batch messages.
//   - catch-up: runs every CatchUpInterval with CatchUpBatch messages, used
//     when the previous tick saturated the catch-up batch size (i.e. the
//     queue likely still has many entries).
type EmbeddingWorker struct {
	store     Store
	embedder  EmbeddingProvider
	logger    *slog.Logger
	batch     int
	interval  time.Duration

	catchUpBatch         int
	catchUpInterval      time.Duration
	maintenanceThreshold int

	// modeFlag is 0 for maintenance, 1 for catch-up. Read atomically by mode();
	// written atomically inside tickWithBatch.
	modeFlag atomic.Int32

	stopChan  chan struct{}
	stopped   chan struct{}
	parentCtx context.Context // parent context for cancellation on shutdown
	ctx       context.Context // child context derived from parentCtx, cancelled in Stop
	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
	started   atomic.Bool
}

// NewEmbeddingWorker constructs a worker. Returns nil if store or embedder is
// nil (caller should check before calling Start). parentCtx is used as the
// ancestor of the worker's internal context so that daemon-level cancellation
// propagates to in-flight embedding HTTP calls.
func NewEmbeddingWorker(parentCtx context.Context, store Store, embedder EmbeddingProvider, logger *slog.Logger, cfg EmbeddingWorkerConfig) *EmbeddingWorker {
	if store == nil || embedder == nil {
		return nil
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 20
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.CatchUpBatch <= 0 {
		cfg.CatchUpBatch = 200
	}
	if cfg.CatchUpInterval <= 0 {
		cfg.CatchUpInterval = 5 * time.Second
	}
	if cfg.MaintenanceThreshold <= 0 {
		cfg.MaintenanceThreshold = 50
	}
	if logger == nil {
		logger = slog.Default()
	}
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return &EmbeddingWorker{
		store:                store,
		embedder:             embedder,
		logger:               logger,
		batch:                cfg.Batch,
		interval:             cfg.Interval,
		catchUpBatch:         cfg.CatchUpBatch,
		catchUpInterval:      cfg.CatchUpInterval,
		maintenanceThreshold: cfg.MaintenanceThreshold,
		stopChan:             make(chan struct{}),
		stopped:              make(chan struct{}),
		parentCtx:            parentCtx,
	}
}

// Start launches the worker goroutine. Safe to call multiple times; only the
// first call launches the goroutine.
func (w *EmbeddingWorker) Start() {
	w.startOnce.Do(func() {
		w.started.Store(true)
		w.ctx, w.cancel = context.WithCancel(w.parentCtx)
		go w.run()
	})
}

// Stop signals the worker to stop and waits for it to finish. Safe to call
// multiple times; subsequent calls are no-ops. Safe to call without Start.
func (w *EmbeddingWorker) Stop() {
	w.stopOnce.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
		close(w.stopChan)
	})
	// Only wait if run() was started — it's the goroutine that closes w.stopped.
	if w.started.Load() {
		<-w.stopped
	}
}

// mode returns the current worker mode: "catch_up" when the previous tick
// saturated the catch-up batch (more messages likely queued) or "maintenance"
// otherwise. Safe for concurrent use.
func (w *EmbeddingWorker) mode() string {
	if w.modeFlag.Load() == 1 {
		return "catch_up"
	}
	return "maintenance"
}

func (w *EmbeddingWorker) run() {
	defer close(w.stopped)
	// Run once at startup so we don't wait an interval for the first batch.
	processed := w.tick(w.ctx)
	for {
		interval := w.interval
		batch := w.batch
		// If the last tick saturated the catch-up batch, assume more remain
		// and run the next tick in catch-up mode (shorter interval, larger
		// batch).
		if processed >= w.catchUpBatch {
			interval = w.catchUpInterval
			batch = w.catchUpBatch
		}
		select {
		case <-w.stopChan:
			return
		case <-w.ctx.Done():
			return
		case <-time.After(interval):
			processed = w.tickWithBatch(w.ctx, batch)
		}
	}
}

// tick processes one maintenance-sized batch and returns the number of messages
// it attempted to embed. Kept for backwards-compatibility with tests that call
// it directly.
func (w *EmbeddingWorker) tick(ctx context.Context) int {
	return w.tickWithBatch(ctx, w.batch)
}

// tickWithBatch processes up to `batch` unembedded messages and returns the
// number of messages it attempted to embed. A return value >= catchUpBatch
// signals "more likely remain in the queue".
func (w *EmbeddingWorker) tickWithBatch(ctx context.Context, batch int) int {
	if batch <= 0 {
		batch = w.batch
	}
	results, err := w.store.UnembeddedMessages(ctx, batch)
	if err != nil {
		w.logger.Warn("embedding_worker: unembedded query failed", "error", err)
		w.modeFlag.Store(0)
		return 0
	}
	if len(results) == 0 {
		w.modeFlag.Store(0)
		return 0
	}
	texts := make([]string, len(results))
	for i, r := range results {
		texts[i] = r.Content
	}
	embeddings, err := w.embedder.GenerateEmbeddings(ctx, texts)
	if err != nil {
		w.logger.Warn("embedding_worker: generate failed (will retry next tick)", "error", err, "count", len(texts))
		w.modeFlag.Store(0)
		return 0
	}
	if len(embeddings) != len(results) {
		w.logger.Warn("embedding_worker: embedding count mismatch", "expected", len(results), "got", len(embeddings))
		w.modeFlag.Store(0)
		return 0
	}
	for i, r := range results {
		if i >= len(embeddings) {
			break
		}
		if err := w.store.StoreEmbedding(ctx, r.MessageID, embeddings[i]); err != nil {
			w.logger.Warn("embedding_worker: store failed", "message_id", r.MessageID, "error", err)
		}
	}
	catchUp := len(results) >= w.catchUpBatch
	if catchUp {
		w.modeFlag.Store(1)
	} else {
		w.modeFlag.Store(0)
	}
	w.logger.Info("embedding_worker: embedded batch",
		"count", len(results),
		"mode", w.mode(),
		"catch_up", catchUp,
	)
	return len(results)
}
