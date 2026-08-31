package config

import (
	"testing"
	"time"
)

// fakeQuotaGetter mirrors the anonymous parameter interface of
// llm.ConfigFromSchema. Existing only in this test file, it proves the
// canonical config.QuotaRetryConfig satisfies the interface llm reads
// through (no internal/config -> internal/llm import is needed).
type fakeQuotaGetter struct {
	QuotaRetryConfig
}

// TestQuotaRetryConfigGetters verifies the four getters llm.ConfigFromSchema
// requires exist on *QuotaRetryConfig and return their fields, and that
// *QuotaRetryConfig itself satisfies the same interface (roundtrip shape).
func TestQuotaRetryConfigGetters(t *testing.T) {
	q := QuotaRetryConfig{
		Enabled:            true,
		MaxWait:            24 * time.Hour,
		DefaultEstimate:    time.Hour,
		DeferCheckInterval: 10 * time.Minute,
	}

	if got := q.GetEnabled(); got != true {
		t.Errorf("GetEnabled() = %v, want true", got)
	}
	if got := q.GetMaxWait(); got != 24*time.Hour {
		t.Errorf("GetMaxWait() = %v, want %v", got, 24*time.Hour)
	}
	if got := q.GetDefaultEstimate(); got != time.Hour {
		t.Errorf("GetDefaultEstimate() = %v, want %v", got, time.Hour)
	}
	if got := q.GetDeferCheckInterval(); got != 10*time.Minute {
		t.Errorf("GetDeferCheckInterval() = %v, want %v", got, 10*time.Minute)
	}

	// The canonical type must satisfy ConfigFromSchema's parameter
	// interface directly (checked via the fake's embedding to keep the
	// assertion compile-time without importing internal/llm).
	var _ interface {
		GetEnabled() bool
		GetMaxWait() time.Duration
		GetDefaultEstimate() time.Duration
		GetDeferCheckInterval() time.Duration
	} = &q

	var _ interface {
		GetEnabled() bool
		GetMaxWait() time.Duration
		GetDefaultEstimate() time.Duration
		GetDeferCheckInterval() time.Duration
	} = &fakeQuotaGetter{QuotaRetryConfig: q}
}
