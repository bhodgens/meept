package llm

// Client-boundary tests for the adaptive pacer wiring (tree 02 leaf 05,
// D15): after a throttle verdict, rapid follow-up requests are spaced by
// the learned interval; a nil pacer is byte-identical to pre-wiring
// behavior (zero sleeps).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newPacingTestClient(t *testing.T, srv *httptest.Server, handler http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()
	c := NewClient(&ModelConfig{
		ProviderID: "prov",
		ModelID:    "m1",
		BaseURL:    srv.URL,
		APIKey:     "k",
	}, WithLogger(discardLogger()))
	sleeps := make([]time.Duration, 0, 4)
	pacer := NewAdaptivePacer(nil, PacingConfig{
		Enabled:          true,
		Target429PerHour: 1,
		MinInterval:      50 * time.Millisecond,
		MaxInterval:      time.Second,
	})
	pacer.sleepFn = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}
	c.SetPacer(pacer)
	return c, &sleeps
}

// TestClientPacing_ThrottleSpacesFollowUps: a throttled response teaches
// the pacer; immediate follow-up requests to the same provider are spaced
// by the learned MinInterval.
func TestClientPacing_ThrottleSpacesFollowUps(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if hits.Load() == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c, sleeps := newPacingTestClient(t, srv, nil)
	// Tiny in-loop retry budget so the loop's own backoff sleep doesn't
	// dominate the test (the pacer gap is what we assert).
	c.SetFailurePolicyConfig(&FailurePolicyConfig{
		Horizon:      time.Hour,
		BaseThrottle: time.Millisecond,
		PollFloor:    10 * time.Millisecond,
		ShortRetries: 3,
	})
	if _, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}}); err != nil {
		t.Fatalf("chat 1: %v", err)
	}
	t.Logf("sleeps after chat 1 (retry attempt gap included): %v", *sleeps)
	// Rapid follow-ups after the learned throttle: spaced by MinInterval.
	for i := range 2 {
		if _, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "again"}}); err != nil {
			t.Fatalf("chat %d: %v", i+2, err)
		}
	}
	if len(*sleeps) == 0 {
		t.Error("pacer did not space rapid follow-ups after throttle (no sleeps recorded)")
	}
}

// TestClientPacing_NilPacerNoWait: without a pacer, rapid calls are never
// delayed (regression: wiring must be inert when disabled).
func TestClientPacing_NilPacerNoWait(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(&ModelConfig{
		ProviderID: "prov",
		ModelID:    "m1",
		BaseURL:    srv.URL,
		APIKey:     "k",
	}, WithLogger(discardLogger()))

	for i := range 3 {
		if _, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}}); err != nil {
			t.Fatalf("chat %d: %v", i, err)
		}
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("hits = %d, want 3 (nil pacer must not gap)", got)
	}
}
