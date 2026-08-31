package llm

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// QuotaWaitConfig holds the minimal settings for quota-aware wait+retry.
// Defined locally to avoid import cycles: internal/config transitively
// imports internal/llm via tools/mcp/client.go.
type QuotaWaitConfig struct {
	Enabled            bool
	MaxWait            time.Duration
	DefaultEstimate    time.Duration
	DeferCheckInterval time.Duration
}

// quotaWaitChatter wraps a Chatter with a single quota-aware wait+retry. On
// QuotaResetError it blocks (ctx-aware) until the computed wait elapses, then
// retries exactly once. Returns the error immediately when the wait would
// exceed cfg.MaxWait or when ctx has an earlier deadline.
type quotaWaitChatter struct {
	inner  Chatter
	cfg    QuotaWaitConfig
	logger *slog.Logger
}

func newQuotaWaitChatter(inner Chatter, cfg QuotaWaitConfig, logger *slog.Logger) Chatter {
	if logger == nil {
		logger = slog.Default()
	}
	return &quotaWaitChatter{inner: inner, cfg: cfg, logger: logger}
}

// ConfigFromSchema copies the canonical config.QuotaRetryConfig into a
// QuotaWaitConfig. Callers must hold any needed locks; this function is
// cheap (only copies fields).
func ConfigFromSchema(qrc interface{ GetEnabled() bool; GetMaxWait() time.Duration; GetDefaultEstimate() time.Duration; GetDeferCheckInterval() time.Duration }) QuotaWaitConfig {
	return QuotaWaitConfig{
		Enabled:            qrc.GetEnabled(),
		MaxWait:            qrc.GetMaxWait(),
		DefaultEstimate:    qrc.GetDefaultEstimate(),
		DeferCheckInterval: qrc.GetDeferCheckInterval(),
	}
}

func (w *quotaWaitChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	resp, err := w.inner.Chat(ctx, messages, opts...)
	if err != nil {
		if !w.cfg.Enabled {
			return resp, err
		}
		var qe *QuotaResetError
		if !errors.As(err, &qe) {
			return resp, err
		}
		wait := w.computeWait(qe)
		if wait <= 0 {
			return resp, err
		}
		if wait > w.cfg.MaxWait {
			w.logger.Info("quota wait exceeds max_wait, returning error",
				"provider", qe.ProviderID, "model", qe.ModelID,
				"wait", wait, "max_wait", w.cfg.MaxWait)
			return resp, err
		}
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) < wait {
			w.logger.Info("quota wait blocked by ctx deadline",
				"provider", qe.ProviderID, "model", qe.ModelID,
				"wait", wait, "deadline", dl)
			// Context deadline fires before wait elapses — return the
			// context error so callers can distinguish deadline-driven
			// abort from quota-blocked return.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return resp, err
			}
		}
		w.logger.Info("waiting for quota reset",
			"provider", qe.ProviderID, "model", qe.ModelID,
			"wait", wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return w.inner.Chat(ctx, messages, opts...)
	}
	return resp, nil
}

func (w *quotaWaitChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	resp, err := w.inner.ChatWithProgress(ctx, messages, progress, opts...)
	if err != nil {
		if !w.cfg.Enabled {
			return resp, err
		}
		var qe *QuotaResetError
		if !errors.As(err, &qe) {
			return resp, err
		}
		wait := w.computeWait(qe)
		if wait <= 0 {
			return resp, err
		}
		if wait > w.cfg.MaxWait {
			return resp, err
		}
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) < wait {
			return resp, err
		}
		w.logger.Info("waiting for quota reset (progress path)",
			"provider", qe.ProviderID, "model", qe.ModelID,
			"wait", wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return w.inner.ChatWithProgress(ctx, messages, progress, opts...)
	}
	return resp, nil
}

func (w *quotaWaitChatter) Config() *ModelConfig {
	if c, ok := w.inner.(interface{ Config() *ModelConfig }); ok {
		return c.Config()
	}
	return nil
}

func (w *quotaWaitChatter) computeWait(qe *QuotaResetError) time.Duration {
	if !qe.ResetAt.IsZero() {
		wait := time.Until(qe.ResetAt)
		if wait < 0 {
			return 0
		}
		return wait
	}
	if qe.RetryAfter > 0 {
		return qe.RetryAfter
	}
	return w.cfg.DefaultEstimate
}
