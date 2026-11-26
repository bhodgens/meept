package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWebhookEarlyDispatch_RateLimited pins bughunt H3/A1-3: the slack /
// voice / messaging early-dispatch branches returned BEFORE the rate-
// limited chain while their comments claimed "Rate limiting still applies".
// Each branch must now run through rateLimitMiddleware (kanban pairing
// precedent): with limit=1/min burst=1, the second request to each webhook
// path is 429 even though the wrapped handler itself always says 200.
func TestWebhookEarlyDispatch_RateLimited(t *testing.T) {
	cases := []struct {
		name string
		opts []ServerOption
		path string
	}{
		{
			name: "slack interactions",
			opts: []ServerOption{WithSlackInteractions(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))},
			path: "/api/v1/slack/interactions",
		},
		{
			name: "voice webhook",
			opts: func() []ServerOption {
				v, _, _, _, _ := enabledRoutes(t)
				return []ServerOption{WithVoiceRoutes(v)}
			}(),
			path: "/api/v1/voice/webhook",
		},
		{
			name: "messaging webhook",
			opts: func() []ServerOption {
				m, _, _ := enabledMessagingRoutes(t)
				return []ServerOption{WithMessagingRoutes(m)}
			}(),
			path: "/api/v1/voice/messaging/webhook",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(ServerConfig{RateLimitPerMinute: 1, RateLimitBurst: 1},
				nil, nil, nil, nil, nil, tc.opts...)
			handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("request fell through to the general mux: %s", r.URL.Path)
			}))

			// First request passes the limiter (burst=1) and reaches the
			// handler; for voice/messaging the handler rejects the unsigned
			// body, which is fine — the point is it was NOT 429.
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte("{}")))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusTooManyRequests {
				t.Fatalf("first request already throttled: %d", rec.Code)
			}

			// Immediate second request must be rate limited. Pre-fix, this
			// branch dispatched the raw handler and returned 200/4xx from
			// the handler itself, never 429.
			req2 := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte("{}")))
			rec2 := httptest.NewRecorder()
			handler.ServeHTTP(rec2, req2)
			if rec2.Code != http.StatusTooManyRequests {
				t.Fatalf("second %s request: status = %d, want 429 (rate limiter must wrap the early dispatch, bughunt H3/A1-3)", tc.name, rec2.Code)
			}
		})
	}
}
