package llm

import (
	"net/http"
	"testing"
	"time"
)

var failurePolicyNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func TestClassify_StatusBuckets(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		header     http.Header
		body       []byte
		wantClass  FailureClass
		wantReason string
	}{
		{
			name:       "200 is none",
			status:     http.StatusOK,
			body:       []byte(`{"ok":true}`),
			wantClass:  FailureNone,
			wantReason: "",
		},
		{
			name:       "201 is none",
			status:     http.StatusCreated,
			body:       []byte(`{}`),
			wantClass:  FailureNone,
			wantReason: "",
		},
		{
			name:       "402 always quota (D5)",
			status:     http.StatusPaymentRequired,
			body:       []byte(`{}`),
			wantClass:  FailureQuota,
			wantReason: "status_402",
		},
		{
			name:       "402 with quota body stays quota with status_402 reason",
			status:     http.StatusPaymentRequired,
			body:       []byte(`{"error":{"type":"insufficient_quota"}}`),
			wantClass:  FailureQuota,
			wantReason: "status_402",
		},
		{
			name:       "bare 429 no quota signal is throttle (D7 core)",
			status:     http.StatusTooManyRequests,
			body:       []byte(`{"error":{"message":"too many requests"}}`),
			wantClass:  FailureThrottle,
			wantReason: "throttle_no_quota_signal",
		},
		{
			name:       "bare 429 plain-text body is throttle",
			status:     http.StatusTooManyRequests,
			body:       []byte("slow down"),
			wantClass:  FailureThrottle,
			wantReason: "throttle_no_quota_signal",
		},
		{
			name:       "429 retry-after alone is still throttle (retry-after is the schedule input, not a quota signal)",
			status:     http.StatusTooManyRequests,
			header:     http.Header{"Retry-After": []string{"120"}},
			body:       []byte("too many requests"),
			wantClass:  FailureThrottle,
			wantReason: "throttle_no_quota_signal",
		},
		{
			name:       "500 is server error",
			status:     http.StatusInternalServerError,
			body:       []byte(`{"error":"internal"}`),
			wantClass:  FailureServerError,
			wantReason: "server_error",
		},
		{
			name:       "502 is server error",
			status:     http.StatusBadGateway,
			body:       []byte("bad gateway"),
			wantClass:  FailureServerError,
			wantReason: "server_error",
		},
		{
			name:       "503 is server error",
			status:     http.StatusServiceUnavailable,
			body:       nil,
			wantClass:  FailureServerError,
			wantReason: "server_error",
		},
		{
			name:       "504 is server error",
			status:     http.StatusGatewayTimeout,
			body:       []byte("timeout"),
			wantClass:  FailureServerError,
			wantReason: "server_error",
		},
		{
			name:       "400 is fatal",
			status:     http.StatusBadRequest,
			body:       []byte(`{"error":{"message":"bad request"}}`),
			wantClass:  FailureFatal,
			wantReason: "client_error",
		},
		{
			name:       "401 is fatal",
			status:     http.StatusUnauthorized,
			body:       []byte(`{"error":{"type":"authentication_error"}}`),
			wantClass:  FailureFatal,
			wantReason: "client_error",
		},
		{
			name:       "403 is fatal",
			status:     http.StatusForbidden,
			body:       []byte(`{"error":{"type":"permission_error"}}`),
			wantClass:  FailureFatal,
			wantReason: "client_error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Classify(tt.status, tt.header, tt.body, failurePolicyNow)
			if v.Class != tt.wantClass {
				t.Errorf("Class = %v, want %v", v.Class, tt.wantClass)
			}
			if v.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", v.Reason, tt.wantReason)
			}
		})
	}
}

// TestClassify_VerdictDefaults pins SHARED-CONVENTIONS §4.1 leaf-01 scope:
// Classify schedules nothing and never parks or gives up yet.
func TestClassify_VerdictDefaults(t *testing.T) {
	none := time.Time{}
	cases := []struct {
		name   string
		status int
		header http.Header
		body   []byte
	}{
		{"200", http.StatusOK, nil, nil},
		{"402", http.StatusPaymentRequired, nil, []byte(`{}`)},
		{"bare 429", http.StatusTooManyRequests, nil, []byte("429")},
		{"500", http.StatusInternalServerError, nil, nil},
		{"400", http.StatusBadRequest, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := Classify(tc.status, tc.header, tc.body, failurePolicyNow)
			if !v.RetryAt.Equal(none) {
				t.Errorf("RetryAt = %v, want zero (leaf 01 schedules nothing)", v.RetryAt)
			}
			if v.Park {
				t.Errorf("Park = true, want false (leaf 01)")
			}
			if v.GiveUp {
				t.Errorf("GiveUp = true, want false (leaf 01)")
			}
		})
	}
}

func TestClassify_QuotaSignals(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		header     http.Header
		body       []byte
		wantReason string
	}{
		{
			name:       "structured body code usage_limit_reached",
			status:     http.StatusTooManyRequests,
			body:       []byte(`{"error":{"type":"usage_limit_reached","resets_at":123}}`),
			wantReason: "structured_quota_body",
		},
		{
			name:       "structured body code insufficient_quota",
			status:     http.StatusTooManyRequests,
			body:       []byte(`{"error":{"type":"insufficient_quota","message":"billing"}}`),
			wantReason: "structured_quota_body",
		},
		{
			name:       "structured body code quota_exceeded",
			status:     http.StatusTooManyRequests,
			body:       []byte(`{"type":"error","error":{"type":"quota_exceeded","message":"exceeded"}}`),
			wantReason: "structured_quota_body",
		},
		{
			name:       "x-quota-reset header is keyword header quota signal",
			status:     http.StatusTooManyRequests,
			header:     http.Header{"X-Quota-Reset": []string{"2026-09-01T13:00:00Z"}},
			body:       []byte("limit"),
			wantReason: "quota_keyword_header",
		},
		{
			name:       "x-usage-limit header is keyword header quota signal",
			status:     http.StatusTooManyRequests,
			header:     http.Header{"X-Usage-Limit-Remaining": []string{"0"}},
			body:       []byte("429"),
			wantReason: "quota_keyword_header",
		},
		{
			name:       "body keyword usage limit",
			status:     http.StatusTooManyRequests,
			body:       []byte("you have exceeded your usage limit"),
			wantReason: "quota_keyword_body",
		},
		{
			name:       "body keyword quota",
			status:     http.StatusTooManyRequests,
			body:       []byte(`{"message":"your quota is exhausted"}`),
			wantReason: "quota_keyword_body",
		},
		{
			name:       "body keyword subscription limit",
			status:     http.StatusTooManyRequests,
			body:       []byte("your subscription limit has been reached"),
			wantReason: "quota_keyword_body",
		},
		{
			name:       "body keyword plan limit",
			status:     http.StatusTooManyRequests,
			body:       []byte("plan limit hit"),
			wantReason: "quota_keyword_body",
		},
		{
			name:       "body keyword rate plan",
			status:     http.StatusTooManyRequests,
			body:       []byte("rate plan cap exceeded"),
			wantReason: "quota_keyword_body",
		},
		{
			name:       "body keyword usage_limit underscore form",
			status:     http.StatusTooManyRequests,
			body:       []byte(`{"message":"usage_limit reached"}`),
			wantReason: "quota_keyword_body",
		},
		{
			name:       "retry-after plus quota keyword flips to quota",
			status:     http.StatusTooManyRequests,
			header:     http.Header{"Retry-After": []string{"120"}},
			body:       []byte("quota exceeded, retry after 120s"),
			wantReason: "quota_keyword_body",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Classify(tt.status, tt.header, tt.body, failurePolicyNow)
			if v.Class != FailureQuota {
				t.Errorf("Class = %v, want %v (reason %q)", v.Class, FailureQuota, tt.wantReason)
			}
			if v.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", v.Reason, tt.wantReason)
			}
		})
	}
}

// TestClassify_AnthropicShortCycle pins the leaf note: Anthropic's structured
// anthropic-ratelimit-*-reset headers are short-cycle rate-limit schedules,
// NOT quota signals — they must land Throttle, never Quota.
func TestClassify_AnthropicShortCycle(t *testing.T) {
	now := failurePolicyNow
	reset := now.Add(30 * time.Second).UTC().Format(time.RFC3339)
	header := http.Header{
		"Anthropic-Ratelimit-Requests-Reset": []string{reset},
		"Retry-After":                        []string{"30"},
	}
	v := Classify(http.StatusTooManyRequests, header, []byte("rate limit exceeded"), now)
	if v.Class != FailureThrottle {
		t.Errorf("Class = %v, want %v; anthropic short-cycle headers must not read as quota", v.Class, FailureThrottle)
	}
	if v.Reason != "throttle_no_quota_signal" {
		t.Errorf("Reason = %q, want %q", v.Reason, "throttle_no_quota_signal")
	}
}

// TestClassify_QuotaParityWithLegacyClassifier is the Task 3 parity guard: it
// replays the fixture corpus of TestQuotaClient_ClassifyQuotaDecision
// (quota_client_test.go) through both classifiers and pins the equivalence
// classifyQuotaDecision(...) == (Classify(...).Class == FailureQuota) for
// every row. The legacy classifier is frozen; Classify must generalize it.
func TestClassify_QuotaParityWithLegacyClassifier(t *testing.T) {
	quotaBody := []byte(`{"error":{"type":"usage_limit_reached","resets_at":1893456000}}`)
	openRouterShort := []byte(`{"error":{"message":"429): {\"error\":{\"type\":\"tpm\",\"retry_after\":2.0,\"retriable\":true}}","code":429}}`)
	openRouterShortDetail := &ProviderError{Retriable: true, RetryAfter: 2 * time.Second}

	tests := []struct {
		name   string
		status int
		body   []byte
		detail *ProviderError
	}{
		{name: "429 quota body", status: http.StatusTooManyRequests, body: quotaBody},
		{name: "402 any body", status: http.StatusPaymentRequired, body: []byte(`{}`)},
		{name: "429 openrouter short retriable", status: http.StatusTooManyRequests, body: openRouterShort, detail: openRouterShortDetail},
		{name: "500 never quota", status: http.StatusInternalServerError, body: quotaBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.status, nil, tt.body, failurePolicyNow).Class == FailureQuota
			want := classifyQuotaDecision(tt.status, tt.body, tt.detail)
			if got != want {
				t.Errorf("Classify == Quota (%v) but classifyQuotaDecision = %v", got, want)
			}
		})
	}
}

// --- Tree 02 leaf 02: BackoffPlan schedule (DECISIONS.md D8) ---

// TestBackoffPlan_NextAttempt pins the deterministic schedule: exponential
// 30s, 60s, 120s, ... until a step would exceed Max (1h), then all
// subsequent attempts exactly 1h (the polling floor, exact — no jitter;
// jitter stays at call sites). attempt=0 is the first retry.
func TestBackoffPlan_NextAttempt(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	plan := BackoffPlan{
		Base:     30 * time.Second,
		Max:      time.Hour,
		GiveUpAt: now.Add(24 * time.Hour),
	}

	tests := []struct {
		name    string
		attempt int
		want    time.Duration // delay from now
	}{
		{name: "attempt 0 first retry", attempt: 0, want: 30 * time.Second},
		{name: "attempt 1", attempt: 1, want: time.Minute},
		{name: "attempt 2", attempt: 2, want: 2 * time.Minute},
		{name: "attempt 3", attempt: 3, want: 4 * time.Minute},
		{name: "attempt 4", attempt: 4, want: 8 * time.Minute},
		{name: "attempt 5", attempt: 5, want: 16 * time.Minute},
		{name: "attempt 6", attempt: 6, want: 32 * time.Minute},
		{name: "attempt 7 capped to floor", attempt: 7, want: time.Hour},    // 64m > 1h -> floor
		{name: "attempt 8 stays at floor", attempt: 8, want: time.Hour},     // exact 1h, no jitter
		{name: "attempt 20 stays at floor", attempt: 20, want: time.Hour},   // long horizon stays exact
		{name: "attempt 100 stays at floor", attempt: 100, want: time.Hour}, // overflow-safe
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plan.NextAttempt(now, tt.attempt, time.Time{})
			if want := now.Add(tt.want); !got.Equal(want) {
				t.Errorf("NextAttempt(attempt=%d) = %v, want %v (delay %v)",
					tt.attempt, got, want, tt.want)
			}
		})
	}
}

// TestBackoffPlan_ShouldGiveUp pins the give-up boundary (D8 cap: the
// schedule ends at GiveUpAt; surfacing the user error is the caller's job).
func TestBackoffPlan_ShouldGiveUp(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	plan := BackoffPlan{
		Base:     30 * time.Second,
		Max:      time.Hour,
		GiveUpAt: now.Add(24 * time.Hour),
	}

	if plan.ShouldGiveUp(now) {
		t.Error("ShouldGiveUp(now) = true, want false before GiveUpAt")
	}
	if plan.ShouldGiveUp(now.Add(23 * time.Hour)) {
		t.Error("ShouldGiveUp(23h) = true, want false before GiveUpAt")
	}
	if !plan.ShouldGiveUp(now.Add(24 * time.Hour)) {
		t.Error("ShouldGiveUp(24h) = false, want true AT GiveUpAt")
	}
	if !plan.ShouldGiveUp(now.Add(25 * time.Hour)) {
		t.Error("ShouldGiveUp(25h) = false, want true after GiveUpAt")
	}
}

// TestBackoffPlan_DefaultBackoffPlan pins DefaultBackoffPlan per class:
// throttle 30s base (D8), quota-402 = throttle base + 5m (D5), other
// classes fall back to the throttle base; horizon/Max from config.
func TestBackoffPlan_DefaultBackoffPlan(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	cfg := FailurePolicyConfig{
		Horizon:           24 * time.Hour,
		BaseThrottle:      30 * time.Second,
		BaseQuota402Extra: 5 * time.Minute,
		PollFloor:         time.Hour,
	}

	tests := []struct {
		name     string
		class    FailureClass
		wantBase time.Duration
	}{
		{name: "throttle base", class: FailureThrottle, wantBase: 30 * time.Second},
		{name: "quota 402 base = throttle + 5m (D5)", class: FailureQuota, wantBase: 30*time.Second + 5*time.Minute},
		{name: "server error falls back to throttle base", class: FailureServerError, wantBase: 30 * time.Second},
		{name: "none falls back to throttle base", class: FailureNone, wantBase: 30 * time.Second},
		{name: "fatal falls back to throttle base", class: FailureFatal, wantBase: 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := DefaultBackoffPlan(tt.class, now, cfg)
			if plan.Base != tt.wantBase {
				t.Errorf("Base = %v, want %v", plan.Base, tt.wantBase)
			}
			if plan.Max != cfg.PollFloor {
				t.Errorf("Max = %v, want PollFloor %v", plan.Max, cfg.PollFloor)
			}
			if want := now.Add(cfg.Horizon); !plan.GiveUpAt.Equal(want) {
				t.Errorf("GiveUpAt = %v, want now+horizon = %v", plan.GiveUpAt, want)
			}
		})
	}
}

// TestBackoffPlan_PriorDateRespected pins the prior-wins rule: an earlier
// server-provided retry time AFTER the computed step is honored (never
// retry before the server says), but CAPPED at GiveUpAt (never wait longer
// than the horizon, D8).
func TestBackoffPlan_PriorDateRespected(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	giveUpAt := now.Add(24 * time.Hour)
	plan := BackoffPlan{Base: 30 * time.Second, Max: time.Hour, GiveUpAt: giveUpAt}

	t.Run("zero prior ignored", func(t *testing.T) {
		got := plan.NextAttempt(now, 0, time.Time{})
		if want := now.Add(30 * time.Second); !got.Equal(want) {
			t.Errorf("got %v, want computed step %v", got, want)
		}
	})

	t.Run("later prior wins over computed step", func(t *testing.T) {
		prior := now.Add(10 * time.Minute)
		got := plan.NextAttempt(now, 0, prior)
		if !got.Equal(prior) {
			t.Errorf("got %v, want prior %v (server says wait longer)", got, prior)
		}
	})

	t.Run("earlier prior loses to computed step", func(t *testing.T) {
		// attempt 6 computes 32m; prior at +40m wins; prior at +10m loses.
		prior := now.Add(10 * time.Minute)
		got := plan.NextAttempt(now, 6, prior)
		if want := now.Add(32 * time.Minute); !got.Equal(want) {
			t.Errorf("got %v, want computed step %v", got, want)
		}
	})

	t.Run("prior capped at GiveUpAt", func(t *testing.T) {
		prior := now.Add(48 * time.Hour)
		got := plan.NextAttempt(now, 0, prior)
		if !got.Equal(giveUpAt) {
			t.Errorf("got %v, want capped at GiveUpAt %v", got, giveUpAt)
		}
	})

	t.Run("computed step capped at GiveUpAt", func(t *testing.T) {
		got := plan.NextAttempt(now, 7, time.Time{})
		if want := now.Add(time.Hour); !got.Equal(want) {
			t.Errorf("got %v, want 1h floor step %v (inside horizon)", got, want)
		}
	})
}
