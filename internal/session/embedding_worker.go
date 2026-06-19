package session

import (
	"context"
	"log/slog"
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
	Batch    int           // messages per tick, default 20
	Interval time.Duration // default 60s
}

// EmbeddingWorker periodically embeds session messages that have no embedding
// yet, using the configured EmbeddingProvider. It is nil-safe: constructors
// return nil when a required dependency is missing, and the daemon checks for
// nil before calling Start.
type EmbeddingWorker struct {
	store    Store
	embedder EmbeddingProvider
	logger   *slog.Logger
	batch    int
	interval time.Duration
	stopChan chan struct{}
	stopped  chan struct{}
}

// NewEmbeddingWorker constructs a worker. Returns nil if store or embedder is
// nil (caller should check before calling Start).
func NewEmbeddingWorker(store Store, embedder EmbeddingProvider, logger *slog.Logger, cfg EmbeddingWorkerConfig) *EmbeddingWorker {
	if store == nil || embedder == nil {
		return nil
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 20
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &EmbeddingWorker{
		store:    store,
		embedder: embedder,
		logger:   logger,
		batch:    cfg.Batch,
		interval: cfg.Interval,
		stopChan: make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// Start launches the worker goroutine. It is safe to call once.
func (w *EmbeddingWorker) Start() {
	go w.run()
}

// Stop signals the worker to stop and waits for it to finish.
func (w *EmbeddingWorker) Stop() {
	close(w.stopChan)
	<-w.stopped
}

func (w *EmbeddingWorker) run() {
	defer close(w.stopped)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	// Run once at startup so we don't wait an interval for the first batch.
	w.tick(context.Background())
	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.tick(context.Background())
		}
	}
}

func (w *EmbeddingWorker) tick(ctx context.Context) {
	results, err := w.store.UnembeddedMessages(ctx, w.batch)
	if err != nil {
		w.logger.Warn("embedding_worker: unembedded query failed", "error", err)
		return
	}
	if len(results) == 0 {
		return
	}
	texts := make([]string, len(results))
	for i, r := range results {
		texts[i] = r.Content
	}
	embeddings, err := w.embedder.GenerateEmbeddings(ctx, texts)
	if err != nil {
		w.logger.Warn("embedding_worker: generate failed (will retry next tick)", "error", err, "count", len(texts))
		return
	}
	if len(embeddings) != len(results) {
		w.logger.Warn("embedding_worker: embedding count mismatch", "expected", len(results), "got", len(embeddings))
		return
	}
	for i, r := range results {
		if i >= len(embeddings) {
			break
		}
		if err := w.store.StoreEmbedding(ctx, r.MessageID, embeddings[i]); err != nil {
			w.logger.Warn("embedding_worker: store failed", "message_id", r.MessageID, "error", err)
		}
	}
	w.logger.Info("embedding_worker: embedded batch", "count", len(results))
}
