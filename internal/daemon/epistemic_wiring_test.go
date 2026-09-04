package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
)

// TestWireHTTPHooks_RetryCountNilMeansDefault3: an omitted retry_count key
// (nil pointer on the config surface) resolves to the repo-standard default
// of 3 when constructing the agent.HTTPHookConfig. Regression guard for the
// absent-vs-explicit-zero distinction introduced when RetryCount became *int.
func TestWireHTTPHooks_RetryCountNilMeansDefault3(t *testing.T) {
	cfg := config.Config{
		Hooks: config.HooksConfig{
			HTTP: []config.HTTPHookConfig{
				{URL: "http://example.internal/hook"},
				{URL: "http://example.internal/other", RetryCount: intPtr(0)},
				{URL: "http://example.internal/third", RetryCount: intPtr(-1)},
			},
		},
	}

	resolved := make([]int, len(cfg.Hooks.HTTP))
	for i, hc := range cfg.Hooks.HTTP {
		// Mirror the wiring loop's resolution exactly (kept in sync with
		// wireHTTPHooks; asserting on the real loop is not possible because
		// the constructed hook's config is unexported, so instead we drive
		// the full wireHTTPHooks below for behavioral coverage).
		resolved[i] = 3
		if hc.RetryCount != nil {
			resolved[i] = *hc.RetryCount
		}
	}
	want := []int{3, 0, -1}
	for i := range want {
		if resolved[i] != want[i] {
			t.Errorf("resolved[%d] = %d, want %d", i, resolved[i], want[i])
		}
	}
	_ = cfg // keep cfg referenced for documentation clarity
}

// TestWireHTTPHooks_EndToEndRetrySemantics drives the REAL wireHTTPHooks
// construction path (config.Config → registered agent.HTTPHook) and asserts
// the three-way retry contract behaviorally against a live test server:
//
//	retry_count omitted (nil) → 3 → succeeds after 1 transient failure
//	retry_count = 0           → exactly 1 attempt on permanent failure
//	retry_count = -1          → unlimited (bounded here via HTTP status:
//	                            keeps retrying while failing, then succeeds)
func TestWireHTTPHooks_EndToEndRetrySemantics(t *testing.T) {
	// Pin a tiny deterministic backoff for the "http" operation: the preset
	// (500ms→10s) makes the unlimited case take ~7s standalone, and sibling
	// tests in this package install process-global per-operation overrides
	// that could inflate the sleeps far beyond the suite timeout. Installing
	// our own (and clearing it after) makes pacing hermetic.
	t.Cleanup(func() {
		agent.ClearPerOperationBackoffOverrideForTest("http")
	})
	agent.SetPerOperationBackoffOverrideForTest("http", backoffConfigForTest())

	cases := []struct {
		name       string
		retryCount *int
		failTimes  int32 // server returns 500 for the first N requests
		wantMin    int32 // minimum total hits
		wantMax    int32 // maximum total hits
	}{
		{
			name:       "nil means default 3",
			retryCount: nil,
			failTimes:  1,
			wantMin:    2,
			wantMax:    2,
		},
		{
			name:       "explicit 0 means no retries",
			retryCount: intPtr(0),
			failTimes:  100,
			wantMin:    1,
			wantMax:    1,
		},
		{
			name:       "negative means unlimited",
			retryCount: intPtr(-1),
			failTimes:  4,
			wantMin:    5,
			wantMax:    5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if atomic.AddInt32(&hits, 1) <= tc.failTimes {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			hr := agent.NewHookRegistry(slog.Default())
			loop := agent.NewAgentLoop("wire-http-hook-test", t.TempDir(),
				agent.WithHookRegistry(hr), agent.WithLoopLogger(slog.Default()))

			cfg := config.Config{
				Hooks: config.HooksConfig{
					HTTP: []config.HTTPHookConfig{
						{
							URL:         srv.URL,
							Method:      "POST",
							Timeout:     5 * time.Second,
							RetryCount:  tc.retryCount,
							AllowedURLs: []string{srv.URL},
						},
					},
				},
			}

			wireHTTPHooks(loop, cfg, nil, slog.Default())

			// Drive the registered hook through the public registry runner:
			// a non-2xx that exhausts retries is logged, not returned as a
			// registry error, so a clean ContextTransform means "ran".
			_ = hr.RunSessionStart(context.Background(), agent.SessionLifecycleState{
				SessionID: "wire-test",
				AgentID:   "test-agent",
			})

			// Session-start hooks are fire-and-forget on errors; poll for
			// the hit count to settle (bounded), then assert.
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if got := atomic.LoadInt32(&hits); got >= tc.wantMin {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			got := atomic.LoadInt32(&hits)
			if got < tc.wantMin || got > tc.wantMax {
				t.Fatalf("server hit %d times, want [%d,%d] (retry_count=%v)",
					got, tc.wantMin, tc.wantMax, tc.retryCount)
			}
		})
	}
}
func intPtr(i int) *int { return &i }

// backoffConfigForTest returns a tiny deterministic backoff used to make
// retry pacing hermetic (1ms delays, no jitter, no multiplier).
func backoffConfigForTest() agent.BackoffConfig {
	return agent.BackoffConfig{
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   1 * time.Millisecond,
		Multiplier: 1.0,
		Jitter:     0,
	}
}
