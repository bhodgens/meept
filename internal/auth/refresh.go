package auth

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RefreshManager periodically checks stored tokens and refreshes any that
// are approaching expiry. Tokens expiring within the refresh margin are
// refreshed proactively.
type RefreshManager struct {
	store   *TokenStore
	margin  time.Duration // refresh tokens expiring within this window
	mu      sync.Mutex
	fails   map[string]int // consecutive refresh failures per provider
	done    chan struct{}
	stopped bool
	wg      sync.WaitGroup
}

// RefreshManagerOption configures the RefreshManager.
type RefreshManagerOption func(*RefreshManager)

// WithRefreshMargin sets how far before expiry tokens should be refreshed.
// Default: 10 minutes.
func WithRefreshMargin(d time.Duration) RefreshManagerOption {
	return func(rm *RefreshManager) {
		rm.margin = d
	}
}

// NewRefreshManager creates a background token refresh manager.
// The manager must be started with Start and stopped with Stop.
func NewRefreshManager(store *TokenStore, opts ...RefreshManagerOption) *RefreshManager {
	rm := &RefreshManager{
		store:  store,
		margin: 10 * time.Minute,
		fails:  make(map[string]int),
		done:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(rm)
	}
	return rm
}

// Start begins the background refresh loop. It runs until Stop is called
// or the context is cancelled.
func (rm *RefreshManager) Start(ctx context.Context, interval time.Duration) {
	rm.wg.Add(1)
	go func() {
		defer rm.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		slog.Info("token refresh manager started",
			"interval", interval,
			"margin", rm.margin,
		)

		// Run once immediately on start.
		rm.refreshAll(ctx)

		for {
			select {
			case <-ctx.Done():
				slog.Info("token refresh manager stopped: context cancelled")
				return
			case <-rm.done:
				slog.Info("token refresh manager stopped")
				return
			case <-ticker.C:
				rm.refreshAll(ctx)
			}
		}
	}()
}

// Stop gracefully stops the refresh manager, waiting for the goroutine
// to finish. Safe to call multiple times.
func (rm *RefreshManager) Stop() {
	rm.mu.Lock()
	if rm.stopped {
		rm.mu.Unlock()
		return
	}
	rm.stopped = true
	close(rm.done)
	rm.mu.Unlock()
	rm.wg.Wait()
}

// refreshAll iterates over all stored tokens and refreshes those that are
// within the refresh margin.
func (rm *RefreshManager) refreshAll(ctx context.Context) {
	tokens, err := rm.store.List()
	if err != nil {
		slog.Warn("token refresh: failed to list tokens", "error", err)
		return
	}

	for _, info := range tokens {
		if info.HasRefresh && time.Now().Add(rm.margin).After(info.Expiry) {
			rm.refreshOne(ctx, info.Provider)
		}
	}
}

// refreshOne attempts to refresh a single provider's token by loading it,
// calling the refresh endpoint directly, and saving the result.
// Consecutive failures are tracked; after 3 consecutive failures, a stale
// warning is logged and the failure count is reset.
func (rm *RefreshManager) refreshOne(ctx context.Context, provider string) {
	providerCfg, err := ResolveProviderConfig(provider)
	if err != nil {
		slog.Warn("token refresh: unknown provider", "provider", provider)
		return
	}

	// Load the stored token to get the refresh token.
	token, err := rm.store.Load(provider)
	if err != nil {
		slog.Warn("token refresh: failed to load token", "provider", provider, "error", err)
		return
	}
	if token.RefreshToken == "" {
		return
	}

	flowCfg := providerCfg.DeviceFlowConfig()
	refreshed, err := RefreshTokenRequest(ctx, flowCfg, token.RefreshToken)
	if err != nil {
		rm.mu.Lock()
		rm.fails[provider]++
		count := rm.fails[provider]
		rm.mu.Unlock()

		if count >= 3 {
			slog.Warn("token refresh: provider marked as stale after consecutive failures",
				"provider", provider,
				"failures", count,
			)
			rm.mu.Lock()
			delete(rm.fails, provider)
			rm.mu.Unlock()
		} else {
			slog.Warn("token refresh failed",
				"provider", provider,
				"failure", fmt.Sprintf("%d/3", count),
				"error", err,
			)
		}
		return
	}

	// If the refresh response omits a new refresh token, keep the old one.
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = token.RefreshToken
	}

	if err := rm.store.Save(provider, refreshed); err != nil {
		slog.Warn("token refresh: failed to save refreshed token",
			"provider", provider, "error", err)
		return
	}

	// Success: reset failure counter.
	rm.mu.Lock()
	delete(rm.fails, provider)
	rm.mu.Unlock()

	slog.Debug("token refreshed successfully", "provider", provider)
}

// Failures returns the current consecutive failure count for a provider.
func (rm *RefreshManager) Failures(provider string) int {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.fails[provider]
}
