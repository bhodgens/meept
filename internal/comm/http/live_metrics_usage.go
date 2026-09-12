// Package http: model/agent usage enrichment for GET /api/v1/metrics/live.
package http

import (
	"context"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
)

// liveMetricsUsageWindow is the aggregation window for the model usage list.
const liveMetricsUsageWindow = 24 * time.Hour

// ModelAgentUsageProvider supplies the model/agent usage extension for
// GET /api/v1/metrics/live. Both *metrics.Store and *metrics.ReadOnlyStore
// implement it.
type ModelAgentUsageProvider interface {
	ModelUsageSince(ctx context.Context, since time.Time) ([]metrics.ModelUsage, error)
	AgentUsageSince(ctx context.Context, since time.Time) ([]metrics.AgentUsage, error)
}

// liveMetricsResponse is the JSON body of GET /api/v1/metrics/live: the
// existing LiveMetricsSnapshot fields (promoted through the embedded pointer,
// so every current key and type is preserved byte-for-byte) plus the
// model/agent usage extension. Models/Agents are always emitted as arrays
// (never null).
type liveMetricsResponse struct {
	*metrics.LiveMetricsSnapshot
	Models []metrics.ModelUsage `json:"models"`
	Agents []metrics.AgentUsage `json:"agents"`
	Totals metrics.UsageTotals  `json:"totals"`
}

// buildLiveMetricsResponse enriches a snapshot with model and agent usage.
// Any usage source failure degrades to empty arrays plus a warning — the
// existing metrics fields are always returned.
func (s *Server) buildLiveMetricsResponse(ctx context.Context, snap *metrics.LiveMetricsSnapshot) liveMetricsResponse {
	resp := liveMetricsResponse{
		LiveMetricsSnapshot: snap,
		Models:              []metrics.ModelUsage{},
		Agents:              []metrics.AgentUsage{},
	}
	provider := s.usageProvider()
	if provider == nil {
		return resp
	}
	since := time.Now().Add(-liveMetricsUsageWindow)
	if models, err := provider.ModelUsageSince(ctx, since); err != nil {
		s.logger.Warn("live metrics: model usage unavailable", "error", err)
	} else if models != nil {
		resp.Models = models
	}
	if agents, err := provider.AgentUsageSince(ctx, since); err != nil {
		s.logger.Warn("live metrics: agent usage unavailable", "error", err)
	} else if agents != nil {
		resp.Agents = agents
	}
	resp.Totals = metrics.SumModelUsage(resp.Models)
	return resp
}

// usageProvider resolves the usage source in priority order: an explicitly
// wired provider (WithMetricsUsageProvider), the MetricsService when it
// implements ModelAgentUsageProvider, then a read-only handle on
// <meept home>/metrics.db.
func (s *Server) usageProvider() ModelAgentUsageProvider {
	if s.metricsUsage != nil {
		return s.metricsUsage
	}
	if p, ok := s.metricsService.(ModelAgentUsageProvider); ok {
		return p
	}
	return s.readOnlyUsageStore()
}

// readOnlyUsageStore lazily opens and caches a read-only metrics.db handle.
// The open runs outside the mutex (no I/O under lock); a concurrent opener
// simply discards its extra handle.
func (s *Server) readOnlyUsageStore() ModelAgentUsageProvider {
	s.usageDBMu.Lock()
	cached := s.usageDB
	s.usageDBMu.Unlock()
	if cached != nil {
		return cached
	}

	path := config.MeeptPath("metrics.db")
	opened, err := metrics.OpenReadOnly(path)
	if err != nil {
		s.logger.Debug("live metrics: metrics database unavailable", "path", path, "error", err)
		return nil
	}

	// Publish the handle, or keep the caller's extra handle for a close
	// after the lock is released (no I/O under the mutex).
	var discard *metrics.ReadOnlyStore
	s.usageDBMu.Lock()
	if s.usageDB == nil {
		s.usageDB = opened
	} else {
		discard = opened
	}
	cached = s.usageDB
	s.usageDBMu.Unlock()

	if discard != nil {
		_ = discard.Close()
	}
	return cached
}
