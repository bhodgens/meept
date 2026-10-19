package hermes

// Tests for the phase-2 client REST extensions (hermes-interop p2-01,
// Contract P2-A): CreateSession, SessionKey header on chat/runs,
// RunApproval, and the /api/jobs CRUD verbs. Wire shapes pinned against
// upstream hermes-agent gateway/platforms/api_server.py +
// api_server_runs.py + cron/jobs.py (2026-10-19).

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

// clientTestTimeout bounds requests in tests that must fail fast (validation
// errors, dead ports) without waiting on a real network timeout.
const clientTestTimeout = time.Second

// newAuthFailServer returns a stub that answers every route with 401.
func newAuthFailServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- CreateSession ----------------------------------------------------------

func TestClient_CreateSession(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions", http.StatusCreated, `{
	  "object": "hermes.session",
	  "session": {"id": "api_1760000000_deadbeef", "source": "api_server", "title": "meept"}
	}`)
	c := newTestClient(s.srv.URL)
	detail, err := c.CreateSession(context.Background(), CreateSessionOptions{Title: "meept"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if detail.ID != "api_1760000000_deadbeef" {
		t.Errorf("session id = %q", detail.ID)
	}
	if s.lastReq.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", s.lastReq.Method)
	}
	var body map[string]any
	if err := json.NewDecoder(bytes.NewReader(s.lastBody)).Decode(&body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if body["title"] != "meept" {
		t.Errorf("request title = %v", body["title"])
	}
}

func TestClient_CreateSession_MissingID(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions", http.StatusCreated, `{"object":"hermes.session","session":{}}`)
	c := newTestClient(s.srv.URL)
	if _, err := c.CreateSession(context.Background(), CreateSessionOptions{Title: "t"}); err == nil {
		t.Fatal("want error when 201 response carries no session id")
	}
}

func TestClient_CreateSession_ConflictSessionExists(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions", http.StatusConflict, `{"error":{"message":"session_exists: s1","type":"invalid_request_error","code":"session_exists"}}`)
	c := newTestClient(s.srv.URL)
	_, err := c.CreateSession(context.Background(), CreateSessionOptions{ID: "s1"})
	if err == nil {
		t.Fatal("want error on 409")
	}
	if !errors.Is(err, ErrHermesSessionExists) {
		t.Errorf("409 session_exists should classify ErrHermesSessionExists, got %v", err)
	}
	if errors.Is(err, ErrHermesSessionForeign) {
		t.Errorf("session_exists must NOT classify as foreign-session: %v", err)
	}
}

func TestClient_CreateSession_ConflictForeignStillForeign(t *testing.T) {
	// A 409 without the session_exists code keeps the phase-1 foreign
	// classification (chat path pinned in client_test.go).
	s := newStubServer(t)
	s.handle("/api/sessions", http.StatusConflict, `{"error":{"message":"permission_denied"}}`)
	c := newTestClient(s.srv.URL)
	_, err := c.CreateSession(context.Background(), CreateSessionOptions{Title: "x"})
	if !errors.Is(err, ErrHermesSessionForeign) {
		t.Errorf("plain 409 should stay ErrHermesSessionForeign, got %v", err)
	}
}

func TestClient_CreateSession_Auth401(t *testing.T) {
	srv := newAuthFailServer(t)
	c := NewClient(srv.URL, "bad", clientTestTimeout)
	_, err := c.CreateSession(context.Background(), CreateSessionOptions{Title: "t"})
	if !errors.Is(err, ErrHermesAuth) {
		t.Errorf("401 should classify ErrHermesAuth, got %v", err)
	}
}

// --- SessionKey header --------------------------------------------------------

func TestClient_ChatSync_SessionKeyHeader(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/s1/chat", http.StatusOK, `{"type":"hermes.session.chat.completion","message":{"role":"assistant","content":"ok"}}`)
	c := newTestClient(s.srv.URL)
	_, err := c.ChatSync(context.Background(), "s1", "hi", ChatOptions{SessionKey: "scope-1"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := s.lastReq.Header.Get("X-Hermes-Session-Key"); got != "scope-1" {
		t.Errorf("X-Hermes-Session-Key = %q, want scope-1", got)
	}
}

func TestClient_ChatSync_NoSessionKeyHeaderWhenEmpty(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/s1/chat", http.StatusOK, `{"type":"hermes.session.chat.completion","message":{"role":"assistant","content":"ok"}}`)
	c := newTestClient(s.srv.URL)
	if _, err := c.ChatSync(context.Background(), "s1", "hi", ChatOptions{}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if _, ok := s.lastReq.Header["X-Hermes-Session-Key"]; ok {
		t.Error("empty SessionKey must send NO X-Hermes-Session-Key header (never an empty value)")
	}
}

func TestClient_CreateRun_SessionKeyHeader(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs", http.StatusAccepted, `{"run_id":"run-1","status":"queued"}`)
	c := newTestClient(s.srv.URL)
	if _, err := c.CreateRun(context.Background(), "go", RunOptions{SessionKey: "scope-2"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := s.lastReq.Header.Get("X-Hermes-Session-Key"); got != "scope-2" {
		t.Errorf("X-Hermes-Session-Key = %q, want scope-2", got)
	}
}

func TestClient_CreateRun_NoSessionKeyHeaderWhenEmpty(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs", http.StatusAccepted, `{"run_id":"run-1","status":"queued"}`)
	c := newTestClient(s.srv.URL)
	if _, err := c.CreateRun(context.Background(), "go", RunOptions{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := s.lastReq.Header["X-Hermes-Session-Key"]; ok {
		t.Error("empty SessionKey must send NO X-Hermes-Session-Key header")
	}
}

func TestClient_SessionKey_RejectsControlChars(t *testing.T) {
	// Upstream rejects CR/LF/NUL in the header (400); never put such a
	// value on the wire — the client refuses locally.
	c := newTestClient("http://127.0.0.1:1")
	_, err := c.ChatSync(context.Background(), "s1", "hi", ChatOptions{SessionKey: "bad\rkey"})
	if err == nil {
		t.Fatal("want local error for control-char session key")
	}
	if !strings.Contains(err.Error(), "session key") {
		t.Errorf("error %q should mention session key", err)
	}
}

// --- RunApproval --------------------------------------------------------------

func TestClient_RunApproval_Approve(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-1/approval", http.StatusOK, `{"object":"hermes.run.approval_response","run_id":"run-1","choice":"once","resolved":1}`)
	c := newTestClient(s.srv.URL)
	if err := c.RunApproval(context.Background(), "run-1", "approve", ""); err != nil {
		t.Fatalf("approval: %v", err)
	}
	var body map[string]any
	if err := json.NewDecoder(bytes.NewReader(s.lastBody)).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Pinned upstream: the field is "choice" (aliases approve->once live
	// server-side, but we send the canonical vocabulary).
	if body["choice"] != "once" {
		t.Errorf("body choice = %v, want once", body["choice"])
	}
	if _, present := body["reason"]; present {
		t.Errorf("empty reason must be omitted, body = %v", body)
	}
}

func TestClient_RunApproval_DenyWithReason(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-1/approval", http.StatusOK, `{"object":"hermes.run.approval_response","run_id":"run-1","choice":"deny","reason":"too risky","resolved":1}`)
	c := newTestClient(s.srv.URL)
	if err := c.RunApproval(context.Background(), "run-1", "deny", "too risky"); err != nil {
		t.Fatalf("approval: %v", err)
	}
	var body map[string]any
	if err := json.NewDecoder(bytes.NewReader(s.lastBody)).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["choice"] != "deny" || body["reason"] != "too risky" {
		t.Errorf("body = %v", body)
	}
}

func TestClient_RunApproval_InvalidDecision(t *testing.T) {
	c := newTestClient("http://127.0.0.1:1")
	err := c.RunApproval(context.Background(), "run-1", "maybe", "")
	if err == nil {
		t.Fatal("want local validation error for unknown decision")
	}
}

func TestClient_RunApproval_RequiresRunID(t *testing.T) {
	c := newTestClient("http://127.0.0.1:1")
	if err := c.RunApproval(context.Background(), "", "approve", ""); err == nil {
		t.Fatal("want error for empty run id")
	}
}

func TestClient_RunApproval_NotPending409(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-1/approval", http.StatusConflict, `{"error":{"message":"Run has no pending approval: run-1","code":"approval_not_pending"}}`)
	c := newTestClient(s.srv.URL)
	err := c.RunApproval(context.Background(), "run-1", "approve", "")
	if err == nil {
		t.Fatal("want error on 409")
	}
	// 409 reuses the phase-1 conflict sentinel (shared classifier).
	if !errors.Is(err, ErrHermesSessionForeign) {
		t.Errorf("409 should reuse the phase-1 conflict sentinel, got %v", err)
	}
}

func TestClient_RunApproval_Busy429(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-1/approval", http.StatusTooManyRequests, `{"error":"slow down"}`)
	c := newTestClient(s.srv.URL)
	if err := c.RunApproval(context.Background(), "run-1", "approve", ""); !errors.Is(err, ErrHermesBusy) {
		t.Errorf("429 should classify ErrHermesBusy, got %v", err)
	}
}

// --- Jobs ---------------------------------------------------------------------

const jobJSON = `{
  "id": "a1b2c3d4e5f6",
  "name": "nightly report",
  "prompt": "summarize the day",
  "schedule": {"kind": "cron", "expr": "0 9 * * *", "display": "every day at 9am"},
  "schedule_display": "every day at 9am",
  "state": "scheduled",
  "enabled": true,
  "deliver": "local",
  "next_run_at": "2026-10-20T09:00:00-06:00",
  "last_run_at": null,
  "last_status": null
}`

func TestClient_ListJobs(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/jobs", http.StatusOK, `{"jobs": [`+jobJSON+`]}`)
	c := newTestClient(s.srv.URL)
	jobs, err := c.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(jobs))
	}
	j := jobs[0]
	if j.ID != "a1b2c3d4e5f6" || j.Name != "nightly report" || j.Prompt != "summarize the day" {
		t.Errorf("job = %+v", j)
	}
	if j.Schedule != "every day at 9am" {
		t.Errorf("schedule display = %q", j.Schedule)
	}
	if j.Status != "scheduled" {
		t.Errorf("status = %q, want scheduled", j.Status)
	}
}

func TestClient_ListJobs_Empty(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/jobs", http.StatusOK, `{}`)
	c := newTestClient(s.srv.URL)
	jobs, err := c.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if jobs == nil || len(jobs) != 0 {
		t.Errorf("want empty non-nil slice, got %#v", jobs)
	}
}

func TestClient_CreateJob(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/jobs", http.StatusOK, `{"job": `+jobJSON+`}`)
	c := newTestClient(s.srv.URL)
	j, err := c.CreateJob(context.Background(), Job{
		Name:     "nightly report",
		Schedule: "every day at 9am",
		Prompt:   "summarize the day",
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if j.ID != "a1b2c3d4e5f6" {
		t.Errorf("job = %+v", j)
	}
	var body map[string]any
	if err := json.NewDecoder(bytes.NewReader(s.lastBody)).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Upstream requires name + schedule; prompt is the payload.
	if body["name"] != "nightly report" || body["schedule"] != "every day at 9am" || body["prompt"] != "summarize the day" {
		t.Errorf("body = %v", body)
	}
}

func TestClient_CreateJob_Validation(t *testing.T) {
	c := newTestClient("http://127.0.0.1:1")
	if _, err := c.CreateJob(context.Background(), Job{Schedule: "30m", Prompt: "x"}); err == nil {
		t.Error("want error for missing name")
	}
	if _, err := c.CreateJob(context.Background(), Job{Name: "n", Prompt: "x"}); err == nil {
		t.Error("want error for missing schedule")
	}
}

func TestClient_CreateJob_BadRequest400(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/jobs", http.StatusBadRequest, `{"error":"Invalid schedule"}`)
	c := newTestClient(s.srv.URL)
	_, err := c.CreateJob(context.Background(), Job{Name: "n", Schedule: "bogus", Prompt: "x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Errorf("want *APIError 400, got %v", err)
	}
}

func TestClient_GetJob(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/jobs/a1b2c3d4e5f6", http.StatusOK, `{"job": `+jobJSON+`}`)
	c := newTestClient(s.srv.URL)
	j, err := c.GetJob(context.Background(), "a1b2c3d4e5f6")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if j.Name != "nightly report" {
		t.Errorf("job = %+v", j)
	}
}

func TestClient_GetJob_NotFound404(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/jobs/aaaaaaaaaaaa", http.StatusNotFound, `{"error":"Job not found"}`)
	c := newTestClient(s.srv.URL)
	_, err := c.GetJob(context.Background(), "aaaaaaaaaaaa")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		t.Errorf("want *APIError 404, got %v", err)
	}
}

func TestClient_PauseResumeRunJob(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/jobs/a1b2c3d4e5f6/pause", http.StatusOK, `{"job": `+jobJSON+`}`)
	s.handle("/api/jobs/a1b2c3d4e5f6/resume", http.StatusOK, `{"job": `+jobJSON+`}`)
	s.handle("/api/jobs/a1b2c3d4e5f6/run", http.StatusOK, `{"job": `+jobJSON+`}`)
	c := newTestClient(s.srv.URL)
	ctx := context.Background()
	if err := c.PauseJob(ctx, "a1b2c3d4e5f6"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if s.lastReq.Method != http.MethodPost {
		t.Errorf("pause method = %s", s.lastReq.Method)
	}
	if err := c.ResumeJob(ctx, "a1b2c3d4e5f6"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := c.RunJob(ctx, "a1b2c3d4e5f6"); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestClient_Jobs_RequireID(t *testing.T) {
	c := newTestClient("http://127.0.0.1:1")
	ctx := context.Background()
	if err := c.PauseJob(ctx, ""); err == nil {
		t.Error("pause: want error for empty id")
	}
	if err := c.ResumeJob(ctx, ""); err == nil {
		t.Error("resume: want error for empty id")
	}
	if err := c.RunJob(ctx, ""); err == nil {
		t.Error("run: want error for empty id")
	}
	if _, err := c.GetJob(ctx, ""); err == nil {
		t.Error("get: want error for empty id")
	}
}

func TestClient_Jobs_Auth401(t *testing.T) {
	srv := newAuthFailServer(t)
	c := NewClient(srv.URL, "bad", clientTestTimeout)
	if _, err := c.ListJobs(context.Background()); !errors.Is(err, ErrHermesAuth) {
		t.Errorf("401 should classify ErrHermesAuth, got %v", err)
	}
}

// --- job schedule decoding (upstream stores a dict; legacy records a string) --

func TestJob_UnmarshalScheduleKinds(t *testing.T) {
	// cron dict with expr
	var j Job
	raw := `{"id":"x","schedule":{"kind":"cron","expr":"0 9 * * *","display":"every day at 9am"}}`
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatalf("cron dict: %v", err)
	}
	if j.Schedule != "every day at 9am" {
		t.Errorf("cron display = %q", j.Schedule)
	}
	// once dict with run_at
	raw = `{"id":"x","schedule":{"kind":"once","run_at":"2026-10-20T09:00:00Z","display":"at 2026-10-20 09:00"}}`
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatalf("once dict: %v", err)
	}
	if j.Schedule == "" {
		t.Error("once schedule empty")
	}
	// interval dict without display
	raw = `{"id":"x","schedule":{"kind":"interval","minutes":30}}`
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatalf("interval dict: %v", err)
	}
	if j.Schedule != "every 30m" {
		t.Errorf("interval display = %q, want every 30m", j.Schedule)
	}
	// legacy bare string
	raw = `{"id":"x","schedule":"30m"}`
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatalf("string schedule: %v", err)
	}
	if j.Schedule != "30m" {
		t.Errorf("string schedule = %q", j.Schedule)
	}
	// absent schedule
	raw = `{"id":"x"}`
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatalf("absent schedule: %v", err)
	}
	if j.Schedule != "" {
		t.Errorf("absent schedule = %q, want empty", j.Schedule)
	}
}
