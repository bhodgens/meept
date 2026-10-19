package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubServer builds an httptest server with a per-route handler map plus
// request recording, so each test asserts on exactly what the client sent.
type stubServer struct {
	t        *testing.T
	srv      *httptest.Server
	mux      *http.ServeMux
	lastReq  *http.Request
	lastBody []byte
}

func newStubServer(t *testing.T) *stubServer {
	s := &stubServer{t: t, mux: http.NewServeMux()}
	s.srv = httptest.NewServer(s.mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubServer) handle(pattern string, status int, body string) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		s.lastReq = r
		// Snapshot the body: the request Body is consumed/closed by the
		// transport side, so tests must not read it lazily.
		if r.Body != nil {
			var buf [1 << 16]byte
			n, _ := r.Body.Read(buf[:])
			s.lastBody = buf[:n]
		}
		// Echo auth checks centrally: every route requires the bearer.
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func newTestClient(baseURL string) *Client {
	return NewClient(baseURL, "test-key", 5*time.Second)
}

// --- happy paths -----------------------------------------------------------

func TestClient_Health(t *testing.T) {
	s := newStubServer(t)
	s.handle("/health", http.StatusOK, `{"status":"ok"}`)
	c := newTestClient(s.srv.URL)
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestClient_Health_ErrorStatus(t *testing.T) {
	s := newStubServer(t)
	s.handle("/health", http.StatusInternalServerError, `{"error":"boom"}`)
	c := newTestClient(s.srv.URL)
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("want error on 500")
	}
	if !strings.Contains(err.Error(), "health") {
		t.Errorf("error %q should name the endpoint", err)
	}
}

func TestClient_Capabilities(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/capabilities", http.StatusOK, `{
	  "features": {"runs": true, "sessions": true, "steer": true},
	  "endpoints": {
	    "chat": {"method": "POST", "path": "/api/sessions/{id}/chat"},
	    "create_run": {"method": "POST", "path": "/v1/runs"}
	  }
	}`)
	c := newTestClient(s.srv.URL)
	caps, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if caps == nil {
		t.Fatal("nil caps")
	}
	if len(caps.Endpoints) == 0 {
		t.Error("endpoints map empty")
	}
}

func TestClient_Capabilities_TTL_SecondCallNoHTTP(t *testing.T) {
	calls := 0
	s := newStubServer(t)
	s.mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"features":{"runs":true},"endpoints":{}}`))
	})
	c := newTestClient(s.srv.URL)
	for i := 0; i < 3; i++ {
		if _, err := c.Capabilities(context.Background()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Errorf("capabilities hit server %d times, want 1 (60s TTL cache)", calls)
	}
}

func TestClient_ChatSync(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/sess-1/chat", http.StatusOK, `{
	  "type": "hermes.session.chat.completion",
	  "message": {"role": "assistant", "content": "hello from hermes"},
	  "usage": {"input_tokens": 10, "output_tokens": 5}
	}`)
	c := newTestClient(s.srv.URL)
	opts := ChatOptions{Provider: "zai", Model: "glm-5.3-flash"}
	res, err := c.ChatSync(context.Background(), "sess-1", "hi", opts)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if res.Content() != "hello from hermes" {
		t.Errorf("content = %q", res.Content())
	}
	// Request body must carry model override + input.
	body := map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(s.lastBody))
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if body["input"] != "hi" {
		t.Errorf("request input = %v", body["input"])
	}
	if body["model"] != "glm-5.3-flash" {
		t.Errorf("request model = %v", body["model"])
	}
	if body["provider"] != "zai" {
		t.Errorf("request provider = %v", body["provider"])
	}
}

func TestClient_CreateRun_202(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs", http.StatusAccepted, `{"run_id": "run-abc", "status": "queued"}`)
	c := newTestClient(s.srv.URL)
	runID, err := c.CreateRun(context.Background(), "do the thing", RunOptions{})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if runID != "run-abc" {
		t.Errorf("runID = %q, want run-abc", runID)
	}
}

func TestClient_RunStatus(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-abc", http.StatusOK, `{"run_id": "run-abc", "status": "completed", "result": "done!"}`)
	c := newTestClient(s.srv.URL)
	st, err := c.RunStatus(context.Background(), "run-abc")
	if err != nil {
		t.Fatalf("run status: %v", err)
	}
	if st.Status != "completed" || st.Result != "done!" {
		t.Errorf("status = %+v", st)
	}
}

func TestClient_SteerRun(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-abc/steer", http.StatusOK, `{"ok": true}`)
	c := newTestClient(s.srv.URL)
	if err := c.SteerRun(context.Background(), "run-abc", "use the fast path"); err != nil {
		t.Fatalf("steer: %v", err)
	}
}

func TestClient_StopRun(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-abc/stop", http.StatusOK, `{"ok": true}`)
	c := newTestClient(s.srv.URL)
	if err := c.StopRun(context.Background(), "run-abc"); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestClient_ListSessions(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions", http.StatusOK, `{
	  "sessions": [{"id": "s1", "created_at": "2026-10-18T00:00:00Z"}],
	  "total": 1
	}`)
	c := newTestClient(s.srv.URL)
	if sessions, err := c.ListSessions(context.Background(), 10, 0); err != nil {
		t.Fatalf("list sessions: %v", err)
	} else {
		if len(sessions) != 1 || sessions[0].ID != "s1" {
			t.Errorf("sessions = %+v", sessions)
		}
	}
	// offset=0 is the default; the client omits it rather than sending an
	// explicit zero (leaf 02 relies on the gateway default for page 1).
	if q := s.lastReq.URL.RawQuery; !strings.Contains(q, "limit=10") {
		t.Errorf("pagination query missing limit: %q", q)
	}
}

func TestClient_SessionMessages(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/s1/messages", http.StatusOK, `{
	  "messages": [{"role": "user", "content": "hey"}, {"role": "assistant", "content": "yo"}]
	}`)
	c := newTestClient(s.srv.URL)
	msgs, err := c.SessionMessages(context.Background(), "s1", 50)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if !strings.Contains(s.lastReq.URL.RawQuery, "limit=50") {
		t.Errorf("limit query missing: %q", s.lastReq.URL.RawQuery)
	}
}

// --- CreateSession model lock + SetSessionModel (session/model extension) --

func TestClient_CreateSession_ExplicitIDWithModelLock(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions", http.StatusCreated, `{"object":"hermes.session","session":{"id":"mine","model":"glm-5.3"}}`)
	c := newTestClient(s.srv.URL)
	detail, err := c.CreateSession(context.Background(), CreateSessionOptions{
		ID: "mine", Model: "glm-5.3", Provider: "zai", RequireModelLock: true,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if detail.ID != "mine" || detail.Model != "glm-5.3" {
		t.Errorf("detail = %+v", detail)
	}
	var body map[string]any
	if err := json.NewDecoder(bytes.NewReader(s.lastBody)).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["id"] != "mine" || body["require_model_lock"] != true {
		t.Errorf("body = %v, want id + require_model_lock", body)
	}
	if body["model"] != "glm-5.3" || body["provider"] != "zai" {
		t.Errorf("body = %v, want model + provider", body)
	}
}

func TestClient_CreateSession_LockUnavailable409(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions", http.StatusConflict, `{"error":{"message":"Requested Browser model lock cannot be routed","code":"model_lock_unavailable"}}`)
	c := newTestClient(s.srv.URL)
	_, err := c.CreateSession(context.Background(), CreateSessionOptions{ID: "x", Model: "bogus", RequireModelLock: true})
	// 409s that are NOT code=session_exists reuse the phase-1 conflict
	// sentinel (pinned in client_ext_test.go for approval_not_pending).
	// The upstream body — which names model_lock_unavailable — rides in
	// the wrapped error text for the operator to read.
	if !errors.Is(err, ErrHermesSessionForeign) {
		t.Errorf("model_lock_unavailable 409 should reuse the conflict sentinel, got %v", err)
	}
	if errors.Is(err, ErrHermesSessionExists) {
		t.Errorf("model_lock_unavailable must not classify as session_exists")
	}
	if !strings.Contains(err.Error(), "model_lock_unavailable") {
		t.Errorf("error text must carry the upstream code, got %v", err)
	}
}

func TestClient_SetSessionModel(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/s1/model", http.StatusOK, `{"object":"hermes.session.model_lock","session_id":"s1","runtime":{"provider":"zai","model":"glm-5.3","route_source":"model_routes","model_lock":"accepted"}}`)
	c := newTestClient(s.srv.URL)
	res, err := c.SetSessionModel(context.Background(), "s1", "zai", "glm-5.3")
	if err != nil {
		t.Fatalf("set model: %v", err)
	}
	if res.SessionID != "s1" || res.Runtime.Model != "glm-5.3" || res.Runtime.ModelLock != "accepted" {
		t.Errorf("res = %+v", res)
	}
	var body map[string]any
	if err := json.NewDecoder(bytes.NewReader(s.lastBody)).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["require_model_lock"] != true || body["model"] != "glm-5.3" || body["provider"] != "zai" {
		t.Errorf("body = %v", body)
	}
}

func TestClient_SetSessionModel_Validation(t *testing.T) {
	c := newTestClient("http://127.0.0.1:1")
	if _, err := c.SetSessionModel(context.Background(), "", "p", "m"); err == nil {
		t.Error("want error for empty session id")
	}
	if _, err := c.SetSessionModel(context.Background(), "s1", "", ""); err == nil {
		t.Error("want error when neither provider nor model given")
	}
}

func TestClient_ListSessions_UpstreamDataShape(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions", http.StatusOK, `{"object":"list","data":[{"id":"s1","started_at":1760000000.5,"model":"glm","source":"api_server","title":"t"}],"limit":50,"offset":0,"has_more":false}`)
	c := newTestClient(s.srv.URL)
	list, err := c.ListSessions(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "s1" || list[0].Model != "glm" {
		t.Fatalf("list = %+v", list)
	}
	if !strings.Contains(list[0].Created(), "1760000000") {
		t.Errorf("Created() = %q, want started_at passthrough", list[0].Created())
	}
}

func TestClient_SessionMessages_UpstreamDataShape(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/s1/messages", http.StatusOK, `{"object":"list","session_id":"s1","data":[{"role":"user","content":"hey"}]}`)
	c := newTestClient(s.srv.URL)
	msgs, err := c.SessionMessages(context.Background(), "s1", 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

// --- typed errors ------------------------------------------------------------

func TestClient_ErrHermesBusy_429(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs", http.StatusTooManyRequests, `{"error": "Too many concurrent runs (max 10)"}`)
	c := newTestClient(s.srv.URL)
	_, err := c.CreateRun(context.Background(), "x", RunOptions{})
	if err == nil {
		t.Fatal("want error on 429")
	}
	if !isBusy(err) {
		t.Errorf("429 should classify ErrHermesBusy, got %v", err)
	}
}

func TestClient_ErrHermesSessionForeign_409(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/foreign/chat", http.StatusConflict, `{"error": "permission_denied"}`)
	c := newTestClient(s.srv.URL)
	_, err := c.ChatSync(context.Background(), "foreign", "hi", ChatOptions{})
	if err == nil {
		t.Fatal("want error on 409")
	}
	if !isForeignSession(err) {
		t.Errorf("409 should classify ErrHermesSessionForeign, got %v", err)
	}
}

func TestClient_ErrHermesAuth_401(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "wrong-key", time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	c = NewClient(srv.URL, "wrong-key", time.Second)
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("want error on 401")
	}
	if !isAuth(err) {
		t.Errorf("401 should classify ErrHermesAuth, got %v", err)
	}
}

func TestClient_NetworkError_WrapsEndpoint(t *testing.T) {
	// Port 1 on localhost: nothing listens; connection refused.
	c := newTestClient("http://127.0.0.1:1")
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("want error on refused connection")
	}
	if !strings.Contains(err.Error(), "health") {
		t.Errorf("network error %q should name the endpoint", err)
	}
}

// --- security: the key never leaks ------------------------------------------

func TestClient_KeyNeverInErrorStrings(t *testing.T) {
	s := newStubServer(t)
	s.handle("/health", http.StatusInternalServerError, `{"error":"boom"}`)
	c := newTestClient(s.srv.URL)
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Error("API key leaked into error string")
	}
	// Network-level failure path too.
	c2 := newTestClient("http://127.0.0.1:1")
	err2 := c2.Health(context.Background())
	if err2 != nil && strings.Contains(err2.Error(), "test-key") {
		t.Error("API key leaked into network error string")
	}
}

func TestClient_BearerHeaderSent(t *testing.T) {
	s := newStubServer(t)
	s.handle("/health", http.StatusOK, `{"status":"ok"}`)
	c := newTestClient(s.srv.URL)
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
	if got := s.lastReq.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("authorization = %q", got)
	}
}

// helpers over errors.Is so test failures name the sentinel, not the wrapper.

func isBusy(err error) bool {
	for e := err; e != nil; e = unwrap(e) {
		if e == ErrHermesBusy {
			return true
		}
	}
	return false
}

func isForeignSession(err error) bool {
	for e := err; e != nil; e = unwrap(e) {
		if e == ErrHermesSessionForeign {
			return true
		}
	}
	return false
}

func isAuth(err error) bool {
	for e := err; e != nil; e = unwrap(e) {
		if e == ErrHermesAuth {
			return true
		}
	}
	return false
}

func unwrap(err error) error {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}
