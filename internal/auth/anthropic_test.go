package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// withAnthropicEndpoints swaps the token endpoint list to the given URLs for
// the duration of a subtest and restores it afterwards.
func withAnthropicEndpoints(t *testing.T, endpoints ...string) {
	t.Helper()
	orig := anthropicTokenEndpoints
	anthropicTokenEndpoints = endpoints
	t.Cleanup(func() { anthropicTokenEndpoints = orig })
}

// tokenHandlerRecorder records requests and replies from a scripted queue.
type tokenHandlerRecorder struct {
	responses []tokenReply
	form      url.Values
	jsonBody  map[string]string
	ua        string
	accept    string
	ctype     string
	calls     int
}

type tokenReply struct {
	status int
	body   string
}

func (h *tokenHandlerRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.calls++
	h.ua = r.Header.Get("User-Agent")
	h.accept = r.Header.Get("Accept")
	h.ctype = r.Header.Get("Content-Type")
	_ = r.ParseForm()
	h.form = r.PostForm
	body := make([]byte, r.ContentLength)
	if n, _ := r.Body.Read(body); n > 0 {
		_ = json.Unmarshal(body, &h.jsonBody)
	}
	if h.calls <= len(h.responses) {
		rep := h.responses[h.calls-1]
		w.WriteHeader(rep.status)
		_, _ = w.Write([]byte(rep.body))
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{}`))
}

func TestBuildAnthropicAuthorizeURL(t *testing.T) {
	got := BuildAnthropicAuthorizeURL("client-abc", "challenge-xyz", "state-123")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", got, err)
	}
	if u.Scheme != "https" || u.Host != "claude.ai" {
		t.Errorf("scheme/host = %s://%s, want https://claude.ai", u.Scheme, u.Host)
	}
	if u.Path != "/oauth/authorize" {
		t.Errorf("path = %q, want /oauth/authorize", u.Path)
	}

	q := u.Query()
	want := map[string]string{
		"code":                  "true",
		"client_id":             "client-abc",
		"response_type":         "code",
		"redirect_uri":          "https://console.anthropic.com/oauth/code/callback",
		"scope":                 "org:create_api_key user:profile user:inference",
		"code_challenge":        "challenge-xyz",
		"code_challenge_method": "S256",
		"state":                 "state-123",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("query %s = %q, want %q", k, got, v)
		}
	}
	if len(q) != len(want) {
		t.Errorf("query has %d params, want %d", len(q), len(want))
	}
}

func TestExchangeAnthropicCode_PrimarySuccess(t *testing.T) {
	h := &tokenHandlerRecorder{responses: []tokenReply{
		{200, `{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600}`},
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	withAnthropicEndpoints(t, srv.URL)

	token, err := ExchangeAnthropicCode(context.Background(), "client-1", "code-1", "verifier-1")
	if err != nil {
		t.Fatalf("ExchangeAnthropicCode: %v", err)
	}
	if h.calls != 1 {
		t.Errorf("endpoint calls = %d, want 1", h.calls)
	}
	if token.AccessToken != "at-1" || token.RefreshToken != "rt-1" {
		t.Errorf("token = %q/%q, want at-1/rt-1", token.AccessToken, token.RefreshToken)
	}
	if time.Until(token.Expiry) <= 0 || time.Until(token.Expiry) > time.Hour {
		t.Errorf("expiry not ~now+3600s: %v", token.Expiry)
	}

	// Form fields.
	for k, v := range map[string]string{
		"grant_type":    "authorization_code",
		"code":          "code-1",
		"redirect_uri":  "https://console.anthropic.com/oauth/code/callback",
		"client_id":     "client-1",
		"code_verifier": "verifier-1",
	} {
		if got := h.form.Get(k); got != v {
			t.Errorf("form %s = %q, want %q", k, got, v)
		}
	}
	// Headers.
	if h.ua != "axios/1.7.9" {
		t.Errorf("User-Agent = %q, want axios/1.7.9", h.ua)
	}
	if h.ctype != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", h.ctype)
	}
	if h.accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", h.accept)
	}
}

func TestExchangeAnthropicCode_FallbackSuccess(t *testing.T) {
	primary := &tokenHandlerRecorder{responses: []tokenReply{{404, `{"error":"not_found"}`}}}
	fallback := &tokenHandlerRecorder{responses: []tokenReply{
		{200, `{"access_token":"at-2","expires_in":60}`},
	}}
	pSrv := httptest.NewServer(primary)
	fSrv := httptest.NewServer(fallback)
	t.Cleanup(pSrv.Close)
	t.Cleanup(fSrv.Close)
	withAnthropicEndpoints(t, pSrv.URL, fSrv.URL)

	token, err := ExchangeAnthropicCode(context.Background(), "client-1", "code-1", "verifier-1")
	if err != nil {
		t.Fatalf("ExchangeAnthropicCode: %v", err)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Errorf("calls = primary %d, fallback %d; want 1/1", primary.calls, fallback.calls)
	}
	if token.AccessToken != "at-2" {
		t.Errorf("access token = %q, want at-2", token.AccessToken)
	}
	if token.RefreshToken != "" {
		t.Errorf("refresh token = %q, want empty", token.RefreshToken)
	}
}

func TestExchangeAnthropicCode_AllFail(t *testing.T) {
	primary := &tokenHandlerRecorder{responses: []tokenReply{{404, `{}`}}}
	fallback := &tokenHandlerRecorder{responses: []tokenReply{{500, `{}`}}}
	pSrv := httptest.NewServer(primary)
	fSrv := httptest.NewServer(fallback)
	t.Cleanup(pSrv.Close)
	t.Cleanup(fSrv.Close)
	withAnthropicEndpoints(t, pSrv.URL, fSrv.URL)

	_, err := ExchangeAnthropicCode(context.Background(), "client-1", "code-1", "verifier-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention last status 500, got: %v", err)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Errorf("calls = primary %d, fallback %d; want 1/1", primary.calls, fallback.calls)
	}
}

func TestExchangeAnthropicCode_MissingAccessTokenRetries(t *testing.T) {
	primary := &tokenHandlerRecorder{responses: []tokenReply{{200, `{"refresh_token":"rt"}`}}}
	fallback := &tokenHandlerRecorder{responses: []tokenReply{
		{200, `{"access_token":"at-3","expires_in":60}`},
	}}
	pSrv := httptest.NewServer(primary)
	fSrv := httptest.NewServer(fallback)
	t.Cleanup(pSrv.Close)
	t.Cleanup(fSrv.Close)
	withAnthropicEndpoints(t, pSrv.URL, fSrv.URL)

	token, err := ExchangeAnthropicCode(context.Background(), "client-1", "code-1", "verifier-1")
	if err != nil {
		t.Fatalf("ExchangeAnthropicCode: %v", err)
	}
	if token.AccessToken != "at-3" {
		t.Errorf("access token = %q, want at-3", token.AccessToken)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Errorf("calls = primary %d, fallback %d; want 1/1", primary.calls, fallback.calls)
	}
}

func TestRefreshAnthropicToken_Form(t *testing.T) {
	h := &tokenHandlerRecorder{responses: []tokenReply{
		{200, `{"access_token":"at-r","refresh_token":"rt-new","expires_in":7200}`},
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	withAnthropicEndpoints(t, srv.URL)

	token, err := RefreshAnthropicToken(context.Background(), "client-r", "rt-old", false)
	if err != nil {
		t.Fatalf("RefreshAnthropicToken: %v", err)
	}
	if token.AccessToken != "at-r" || token.RefreshToken != "rt-new" {
		t.Errorf("token = %q/%q, want at-r/rt-new", token.AccessToken, token.RefreshToken)
	}
	if h.ctype != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want form", h.ctype)
	}
	for k, v := range map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": "rt-old",
		"client_id":     "client-r",
	} {
		if got := h.form.Get(k); got != v {
			t.Errorf("form %s = %q, want %q", k, got, v)
		}
	}
	if h.ua != "axios/1.7.9" {
		t.Errorf("User-Agent = %q, want axios/1.7.9", h.ua)
	}
}

func TestRefreshAnthropicToken_JSON(t *testing.T) {
	h := &tokenHandlerRecorder{responses: []tokenReply{
		{200, `{"access_token":"at-j","expires_in":60}`},
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	withAnthropicEndpoints(t, srv.URL)

	token, err := RefreshAnthropicToken(context.Background(), "client-j", "rt-old", true)
	if err != nil {
		t.Fatalf("RefreshAnthropicToken: %v", err)
	}
	if token.AccessToken != "at-j" {
		t.Errorf("access token = %q, want at-j", token.AccessToken)
	}
	if token.RefreshToken != "" {
		t.Errorf("refresh token = %q, want empty (caller keeps old)", token.RefreshToken)
	}
	if h.ctype != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", h.ctype)
	}
	if h.jsonBody == nil {
		t.Fatal("no JSON body decoded")
	}
	for k, v := range map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": "rt-old",
		"client_id":     "client-j",
	} {
		if h.jsonBody[k] != v {
			t.Errorf("json %s = %q, want %q", k, h.jsonBody[k], v)
		}
	}
	if h.ua != "axios/1.7.9" {
		t.Errorf("User-Agent = %q, want axios/1.7.9", h.ua)
	}
}

func TestRefreshAnthropicToken_Fallback(t *testing.T) {
	primary := &tokenHandlerRecorder{responses: []tokenReply{{404, `{}`}}}
	fallback := &tokenHandlerRecorder{responses: []tokenReply{
		{200, `{"access_token":"at-f","expires_in":60}`},
	}}
	pSrv := httptest.NewServer(primary)
	fSrv := httptest.NewServer(fallback)
	t.Cleanup(pSrv.Close)
	t.Cleanup(fSrv.Close)
	withAnthropicEndpoints(t, pSrv.URL, fSrv.URL)

	token, err := RefreshAnthropicToken(context.Background(), "client-f", "rt-old", false)
	if err != nil {
		t.Fatalf("RefreshAnthropicToken: %v", err)
	}
	if token.AccessToken != "at-f" {
		t.Errorf("access token = %q, want at-f", token.AccessToken)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Errorf("calls = primary %d, fallback %d; want 1/1", primary.calls, fallback.calls)
	}
}
