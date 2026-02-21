package shadow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"golang.org/x/time/rate"
)

// TeacherClient orchestrates teacher model responses.
type TeacherClient struct {
	primary       *llm.Client
	fallback      *llm.Client
	config        *TeacherConfig
	trainingStore *SQLiteTrainingStore
	logger        *slog.Logger

	// Rate limiting
	limiter *rate.Limiter

	// Daily usage tracking
	mu            sync.RWMutex
	dailyQueries  int
	dailyCost     float64
	lastResetDate string
}

// TeacherClientOption is a functional option for TeacherClient.
type TeacherClientOption func(*TeacherClient)

// WithTeacherLogger sets the logger.
func WithTeacherLogger(logger *slog.Logger) TeacherClientOption {
	return func(t *TeacherClient) {
		t.logger = logger
	}
}

// WithTrainingStore sets the training store for usage tracking.
func WithTrainingStore(store *SQLiteTrainingStore) TeacherClientOption {
	return func(t *TeacherClient) {
		t.trainingStore = store
	}
}

// NewTeacherClient creates a new teacher client.
func NewTeacherClient(primary *llm.Client, fallback *llm.Client, config *TeacherConfig, opts ...TeacherClientOption) *TeacherClient {
	t := &TeacherClient{
		primary:  primary,
		fallback: fallback,
		config:   config,
		logger:   slog.Default(),
		limiter:  rate.NewLimiter(rate.Limit(float64(config.RequestsPerMinute)/60.0), 1),
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// GetResponse gets a teacher response for the given messages.
func (t *TeacherClient) GetResponse(ctx context.Context, messages []llm.ChatMessage) (string, string, error) {
	// Check daily limits
	if err := t.checkLimits(ctx); err != nil {
		return "", "", err
	}

	// Wait for rate limit
	if err := t.limiter.Wait(ctx); err != nil {
		return "", "", fmt.Errorf("rate limit wait cancelled: %w", err)
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, t.config.Timeout())
	defer cancel()

	// Try primary
	response, err := t.callTeacher(timeoutCtx, t.primary, messages)
	if err == nil {
		t.recordUsage(ctx, t.primary.Config())
		return response.Content, t.primary.Config().ModelID, nil
	}

	t.logger.Warn("Primary teacher failed, trying fallback",
		"error", err,
		"primary_model", t.primary.Config().ModelID,
	)

	// Try fallback if available
	if t.fallback != nil {
		response, err = t.callTeacher(timeoutCtx, t.fallback, messages)
		if err == nil {
			t.recordUsage(ctx, t.fallback.Config())
			return response.Content, t.fallback.Config().ModelID, nil
		}

		t.logger.Error("Fallback teacher also failed",
			"error", err,
			"fallback_model", t.fallback.Config().ModelID,
		)
	}

	return "", "", fmt.Errorf("teacher request failed: %w", err)
}

func (t *TeacherClient) callTeacher(ctx context.Context, client *llm.Client, messages []llm.ChatMessage) (*llm.Response, error) {
	opts := []llm.ChatOption{
		llm.WithTemperature(t.config.Temperature),
		llm.WithMaxTokens(t.config.MaxTokens),
	}

	return client.Chat(ctx, messages, opts...)
}

func (t *TeacherClient) checkLimits(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Reset daily counters if needed
	today := time.Now().UTC().Format("2006-01-02")
	if t.lastResetDate != today {
		t.dailyQueries = 0
		t.dailyCost = 0
		t.lastResetDate = today

		// Load from database if available
		if t.trainingStore != nil {
			queries, cost, err := t.trainingStore.GetTeacherUsageToday(ctx)
			if err == nil {
				t.dailyQueries = queries
				t.dailyCost = cost
			}
		}
	}

	// Check query limit
	if t.config.MaxDailyQueries > 0 && t.dailyQueries >= t.config.MaxDailyQueries {
		return fmt.Errorf("daily teacher query limit reached (%d)", t.config.MaxDailyQueries)
	}

	// Check cost limit
	if t.config.MaxDailyCost > 0 && t.dailyCost >= t.config.MaxDailyCost {
		return fmt.Errorf("daily teacher cost limit reached ($%.2f)", t.config.MaxDailyCost)
	}

	return nil
}

func (t *TeacherClient) recordUsage(ctx context.Context, cfg *llm.ModelConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.dailyQueries++

	// Estimate cost (rough approximation)
	estimatedCost := (cfg.CostPerMillionInput + cfg.CostPerMillionOutput) / 1000.0
	t.dailyCost += estimatedCost

	// Persist to database if available
	if t.trainingStore != nil {
		if err := t.trainingStore.RecordTeacherUsage(ctx, 1, estimatedCost); err != nil {
			t.logger.Warn("Failed to record teacher usage", "error", err)
		}
	}

	t.logger.Debug("Teacher usage recorded",
		"daily_queries", t.dailyQueries,
		"daily_cost", t.dailyCost,
	)
}

// GetUsageStats returns current daily usage statistics.
func (t *TeacherClient) GetUsageStats() (queries int, cost float64) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.dailyQueries, t.dailyCost
}

// IsAvailable returns true if the teacher is available (within limits).
func (t *TeacherClient) IsAvailable(ctx context.Context) bool {
	return t.checkLimits(ctx) == nil
}
