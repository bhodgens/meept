package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// codexUsercodeRecorder wraps an httptest server to count calls and serve
// canned responses in sequence.
type codexUsercodeRecorder struct {
	statuses    []int
	bodies      []string
	retryAfters []string
	calls       int
	lastBody    string
	lastHeader  http.Header
}

func (s *codexUsercodeRecorder) handler(w http.ResponseWriter, r *http.Request) {
	i := s.calls
	s.calls++
	body, _ := io.ReadAll(r.Body)
	s.lastBody = string(body)
	s.lastHeader = r.Header

	idx := i
	if idx >= len(s.statuses) {
		idx = len(s.statuses) - 1
	}
	status := s.statuses[idx]
	if idx < len(s.retryAfters) && s.retryAfters[idx] != "" {
		w.Header().Set("Retry-After", s.retryAfters[idx])
	}
	respBody := "{}"
	if idx < len(s.bodies) && s.bodies[idx] != "" {
		respBody = s.bodies[idx]
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, respBody)
}

func TestStartCodexDeviceFlow(t *testing.T) {
	t.Run("retry 429 then success", func(t *testing.T) {
		srv := &codexUsercodeRecorder{
			statuses:    []int{http.StatusTooManyRequests, http.StatusOK},
			retryAfters: []string{"1", ""},
			bodies: []string{
				"",
				`{"user_code":"WDJB-MJHT","device_auth_id":"da_123","interval":"5"}`,
			},
		}
		ts := httptest.NewServer(http.HandlerFunc(srv.handler))
		defer ts.Close()

		result, err := StartCodexDeviceFlow(context.Background(), ts.URL, "app_EMoamEEZ73f0CkXaXp7hrann")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if srv.calls != 2 {
			t.Errorf("calls = %d, want 2", srv.calls)
		}
		if result.UserCode != "WDJB-MJHT" {
			t.Errorf("UserCode = %q, want WDJB-MJHT", result.UserCode)
		}
		if result.DeviceAuthID != "da_123" {
			t.Errorf("DeviceAuthID = %q, want da_123", result.DeviceAuthID)
		}
		if result.VerifyURL != "https://auth.openai.com/codex/device" {
			t.Errorf("VerifyURL = %q, want codex/device constant", result.VerifyURL)
		}
		if result.Interval != 5*time.Second {
			t.Errorf("Interval = %v, want 5s", result.Interval)
		}
		// Request body must be JSON {"client_id": ...}.
		var parsed map[string]any
		if err := json.Unmarshal([]byte(srv.lastBody), &parsed); err != nil {
			t.Fatalf("request body not JSON: %v", err)
		}
		if parsed["client_id"] != "app_EMoamEEZ73f0CkXaXp7hrann" {
			t.Errorf("client_id = %v, want app_EMoamEEZ73f0CkXaXp7hrann", parsed["client_id"])
		}
	})

	t.Run("persistent 429 rate limited error", func(t *testing.T) {
		srv := &codexUsercodeRecorder{
			statuses:    []int{http.StatusTooManyRequests},
			retryAfters: []string{"0"},
		}
		ts := httptest.NewServer(http.HandlerFunc(srv.handler))
		defer ts.Close()

		start := time.Now()
		_, err := StartCodexDeviceFlow(context.Background(), ts.URL, "cid")
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if !strings.Contains(err.Error(), "rate limit") {
			t.Errorf("error %q should mention rate limit", err.Error())
		}
		// Retry-After: 0 clamps to 1s per sleep; 3 sleeps ≈ 3s.
		if elapsed := time.Since(start); elapsed > 6*time.Second {
			t.Errorf("elapsed %v too long for clamped retries", elapsed)
		}
	})

	t.Run("non-200 error mentions status", func(t *testing.T) {
		srv := &codexUsercodeRecorder{
			statuses: []int{http.StatusInternalServerError},
			bodies:   []string{`{"error":"boom"}`},
		}
		ts := httptest.NewServer(http.HandlerFunc(srv.handler))
		defer ts.Close()

		_, err := StartCodexDeviceFlow(context.Background(), ts.URL, "cid")
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error %q should mention 500", err.Error())
		}
	})

	t.Run("interval as number vs missing defaults to 3s", func(t *testing.T) {
		for name, body := range map[string]string{
			"number":    `{"user_code":"ABC","device_auth_id":"d1","interval":7}`,
			"missing":   `{"user_code":"ABC","device_auth_id":"d1"}`,
			"string":    `{"user_code":"ABC","device_auth_id":"d1","interval":"2"}`,
			"malformed": `{"user_code":"ABC","device_auth_id":"d1","interval":"x"}`,
		} {
			t.Run(name, func(t *testing.T) {
				srv := &codexUsercodeRecorder{
					statuses: []int{http.StatusOK},
					bodies:   []string{body},
				}
				ts := httptest.NewServer(http.HandlerFunc(srv.handler))
				defer ts.Close()

				result, err := StartCodexDeviceFlow(context.Background(), ts.URL, "cid")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				want := 3 * time.Second
				if name == "number" {
					want = 7 * time.Second
				}
				if result.Interval != want {
					t.Errorf("Interval = %v, want %v", result.Interval, want)
				}
				if result.Interval < 3*time.Second {
					t.Errorf("Interval %v below 3s floor", result.Interval)
				}
			})
		}
	})
}

func TestPollCodexAuthorization(t *testing.T) {
	newServer := func(statuses []int, bodies []string, calls *int, lastBody *string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			i := *calls
			*calls++
			b, _ := io.ReadAll(r.Body)
			*lastBody = string(b)

			idx := i
			if idx >= len(statuses) {
				idx = len(statuses) - 1
			}
			respBody := "{}"
			if idx < len(bodies) && bodies[idx] != "" {
				respBody = bodies[idx]
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statuses[idx])
			fmt.Fprint(w, respBody)
		}))
	}

	t.Run("404 twice then grant", func(t *testing.T) {
		calls := 0
		var lastBody string
		ts := newServer(
			[]int{http.StatusNotFound, http.StatusNotFound, http.StatusOK},
			[]string{"", "", `{"authorization_code":"ac_1","code_verifier":"cv_1"}`},
			&calls, &lastBody,
		)
		defer ts.Close()

		grant, err := PollCodexAuthorization(context.Background(), ts.URL, "da_9", "WDJB", 10*time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3", calls)
		}
		if grant.AuthorizationCode != "ac_1" || grant.CodeVerifier != "cv_1" {
			t.Errorf("grant = %+v, want ac_1/cv_1", grant)
		}
		var payload map[string]string
		if err := json.Unmarshal([]byte(lastBody), &payload); err != nil {
			t.Fatalf("poll body not JSON: %v", err)
		}
		if payload["device_auth_id"] != "da_9" || payload["user_code"] != "WDJB" {
			t.Errorf("poll body = %v", payload)
		}
	})

	t.Run("timeout when always 404", func(t *testing.T) {
		calls := 0
		var lastBody string
		ts := newServer([]int{http.StatusNotFound}, nil, &calls, &lastBody)
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err := PollCodexAuthorization(ctx, ts.URL, "da", "UC", 10*time.Millisecond)
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
	})

	t.Run("cancelled context returns ctx error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := PollCodexAuthorization(ctx, "http://unused.invalid", "da", "UC", 10*time.Millisecond)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestExchangeCodexToken(t *testing.T) {
	t.Run("happy path form fields and token result", func(t *testing.T) {
		var gotForm url.Values
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotForm = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"at_1","refresh_token":"rt_1","token_type":"Bearer","expires_in":3600,"scope":"read write"}`)
		}))
		defer ts.Close()

		grant := &CodexAuthGrant{AuthorizationCode: "ac_9", CodeVerifier: "cv_9"}
		token, err := ExchangeCodexToken(context.Background(), ts.URL, "app_EMoamEEZ73f0CkXaXp7hrann", grant)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {"ac_9"},
			"redirect_uri":  {"https://auth.openai.com/deviceauth/callback"},
			"client_id":     {"app_EMoamEEZ73f0CkXaXp7hrann"},
			"code_verifier": {"cv_9"},
		}
		if gotForm.Encode() != want.Encode() {
			t.Errorf("form = %v, want %v", gotForm, want)
		}
		if token.AccessToken != "at_1" {
			t.Errorf("AccessToken = %q", token.AccessToken)
		}
		if token.RefreshToken != "rt_1" {
			t.Errorf("RefreshToken = %q", token.RefreshToken)
		}
		if token.TokenType != "Bearer" {
			t.Errorf("TokenType = %q", token.TokenType)
		}
		if len(token.Scopes) != 2 || token.Scopes[0] != "read" || token.Scopes[1] != "write" {
			t.Errorf("Scopes = %v", token.Scopes)
		}
		if !token.Expiry.After(time.Now()) {
			t.Errorf("Expiry %v not in the future", token.Expiry)
		}
	})

	t.Run("missing access_token error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"refresh_token":"rt_1"}`)
		}))
		defer ts.Close()

		_, err := ExchangeCodexToken(context.Background(), ts.URL, "cid", &CodexAuthGrant{AuthorizationCode: "ac", CodeVerifier: "cv"})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if !strings.Contains(err.Error(), "access_token") {
			t.Errorf("error %q should mention access_token", err.Error())
		}
	})
}
