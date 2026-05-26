package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// mockHandler implements Handler for testing.
type mockHandler struct {
	chatResponse string
	chatError    error
	statusResult map[string]any
	statusError  error
}

func (h *mockHandler) Chat(ctx context.Context, message string) (string, error) {
	return h.chatResponse, h.chatError
}

func (h *mockHandler) Status(ctx context.Context) (map[string]any, error) {
	return h.statusResult, h.statusError
}

// mockMemorySearcher implements MemorySearcher.
type mockMemorySearcher struct {
	results []MemorySearchResult
	err     error
}

func (m *mockMemorySearcher) Search(ctx context.Context, query string, limit int) ([]MemorySearchResult, error) {
	return m.results, m.err
}

// mockSkillsLister implements SkillsLister.
type mockSkillsLister struct {
	skills []SkillInfo
}

func (m *mockSkillsLister) List() []SkillInfo {
	return m.skills
}

// mockJobsLister implements JobsLister.
type mockJobsLister struct {
	jobs []JobInfo
	err  error
}

func (m *mockJobsLister) ListJobs() ([]JobInfo, error) {
	return m.jobs, m.err
}

// mockSessionManager implements SessionManager.
type mockSessionManager struct {
	sessions []SessionInfo
	created  SessionInfo
	getErr   error
	delErr   error
}

func (m *mockSessionManager) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	return m.sessions, nil
}

func (m *mockSessionManager) CreateSession(ctx context.Context, name string) (SessionInfo, error) {
	m.created = SessionInfo{
		ID:           "sess-123",
		Name:         name,
		CreatedAt:    time.Now().Format(time.RFC3339),
		LastActivity: time.Now().Format(time.RFC3339),
	}
	return m.created, nil
}

func (m *mockSessionManager) GetSession(ctx context.Context, id string) (SessionInfo, error) {
	if m.getErr != nil {
		return SessionInfo{}, m.getErr
	}
	return SessionInfo{ID: id, Name: "test-session"}, nil
}

func (m *mockSessionManager) DeleteSession(ctx context.Context, id string) error {
	return m.delErr
}

// mockAgentLister implements AgentLister.
type mockAgentLister struct {
	agents []AgentEntry
	delRes DelegateResult
	delErr error
}

func (m *mockAgentLister) ListAgents(ctx context.Context) ([]AgentEntry, error) {
	return m.agents, nil
}

func (m *mockAgentLister) DelegateTask(ctx context.Context, agentID, message string) (DelegateResult, error) {
	return m.delRes, m.delErr
}

// mockToolLister implements ToolLister.
type mockToolLister struct {
	tools []ToolEntry
	err   error
}

func (m *mockToolLister) ListTools(ctx context.Context) ([]ToolEntry, error) {
	return m.tools, m.err
}

// mockMemoryStore implements MemoryStore.
type mockMemoryStore struct {
	result MemoryStoreResult
	err    error
}

func (m *mockMemoryStore) StoreMemory(ctx context.Context, req MemoryStoreRequest) (MemoryStoreResult, error) {
	return m.result, m.err
}

// mockSkillExecutor implements SkillExecutor.
type mockSkillExecutor struct {
	result SkillExecuteResult
	err    error
}

func (m *mockSkillExecutor) ExecuteSkill(ctx context.Context, name, input string) (SkillExecuteResult, error) {
	return m.result, m.err
}

// mockJobScheduler implements JobScheduler.
type mockJobScheduler struct {
	jobID     string
	createErr error
	job       map[string]any
	getErr    error
	cancelErr error
}

func (m *mockJobScheduler) CreateJob(ctx context.Context, cfg map[string]any) (string, error) {
	return m.jobID, m.createErr
}

func (m *mockJobScheduler) GetJob(ctx context.Context, id string) (map[string]any, error) {
	return m.job, m.getErr
}

func (m *mockJobScheduler) CancelJob(ctx context.Context, id string) error {
	return m.cancelErr
}

// newTestServer creates a server with mock dependencies for testing.
func newTestServer(opts ...ServerOption) *Server {
	cfg := DefaultServerConfig()
	handler := &mockHandler{
		chatResponse: "hello world",
		statusResult: map[string]any{"status": "running"},
	}
	auth := NoAuth{}
	return NewServer(cfg, handler, auth, nil, opts...)
}

func doRequest(s *Server, method, path string, body any) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()

	// Use a mux matching the real setupRoutes pattern
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	mux.ServeHTTP(w, req)
	return w
}

func parseBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	return result
}

// ---------------------------------------------------------------------------
// Health & Status tests
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/health", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", body["status"])
	}
}

func TestHandleHealthAPI(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/api/v1/health", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleStatus(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/api/v1/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["status"] != "running" {
		t.Fatalf("expected running, got %v", body["status"])
	}
}

// ---------------------------------------------------------------------------
// Chat tests
// ---------------------------------------------------------------------------

func TestHandleChat(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/chat", map[string]string{"message": "hi"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["response"] != "hello world" {
		t.Fatalf("expected 'hello world', got %v", body["response"])
	}
}

func TestHandleChat_EmptyMessage(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/chat", map[string]string{"message": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleChat_InvalidBody(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleChat_QueryAlias(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/query", map[string]string{"message": "hi"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Sessions tests
// ---------------------------------------------------------------------------

func TestSessionsList_NotConfigured(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/api/v1/sessions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["message"] != "session management not configured" {
		t.Fatalf("expected not configured message, got %v", body["message"])
	}
}

func TestSessionsList_WithManager(t *testing.T) {
	sm := &mockSessionManager{
		sessions: []SessionInfo{
			{ID: "s1", Name: "session one"},
			{ID: "s2", Name: "session two"},
		},
	}
	s := newTestServer(WithSessionManager(sm))
	w := doRequest(s, http.MethodGet, "/api/v1/sessions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["count"].(float64) != 2 {
		t.Fatalf("expected 2 sessions, got %v", body["count"])
	}
}

func TestSessionsCreate(t *testing.T) {
	sm := &mockSessionManager{}
	s := newTestServer(WithSessionManager(sm))
	w := doRequest(s, http.MethodPost, "/api/v1/sessions", map[string]string{"name": "test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["id"] != "sess-123" {
		t.Fatalf("expected sess-123, got %v", body["id"])
	}
}

func TestSessionsCreate_DefaultName(t *testing.T) {
	sm := &mockSessionManager{}
	s := newTestServer(WithSessionManager(sm))
	w := doRequest(s, http.MethodPost, "/api/v1/sessions", map[string]string{"name": ""})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

func TestSessionsGet(t *testing.T) {
	sm := &mockSessionManager{}
	s := newTestServer(WithSessionManager(sm))
	w := doRequest(s, http.MethodGet, "/api/v1/sessions/abc", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestSessionsDelete(t *testing.T) {
	sm := &mockSessionManager{}
	s := newTestServer(WithSessionManager(sm))
	w := doRequest(s, http.MethodDelete, "/api/v1/sessions/abc", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["ok"] != true {
		t.Fatalf("expected ok true, got %v", body["ok"])
	}
}

// ---------------------------------------------------------------------------
// Agents tests
// ---------------------------------------------------------------------------

func TestAgentsList_Default(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/api/v1/agents", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["count"].(float64) != 8 {
		t.Fatalf("expected 8 agents, got %v", body["count"])
	}
}

func TestAgentsList_WithLister(t *testing.T) {
	al := &mockAgentLister{
		agents: []AgentEntry{
			{ID: "coder", Name: "coder", Role: "Executor"},
		},
	}
	s := newTestServer(WithAgentLister(al))
	w := doRequest(s, http.MethodGet, "/api/v1/agents", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["count"].(float64) != 1 {
		t.Fatalf("expected 1 agent, got %v", body["count"])
	}
}

func TestAgentsDelegate(t *testing.T) {
	al := &mockAgentLister{
		delRes: DelegateResult{AgentID: "coder", Status: "delegated"},
	}
	s := newTestServer(WithAgentLister(al))
	w := doRequest(s, http.MethodPost, "/api/v1/agents/coder/delegate", map[string]string{"message": "fix bug"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["agent_id"] != "coder" {
		t.Fatalf("expected coder, got %v", body["agent_id"])
	}
}

func TestAgentsDelegate_NoMessage(t *testing.T) {
	al := &mockAgentLister{}
	s := newTestServer(WithAgentLister(al))
	w := doRequest(s, http.MethodPost, "/api/v1/agents/coder/delegate", map[string]string{"message": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAgentsDelegate_NotConfigured(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/agents/coder/delegate", map[string]string{"message": "test"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Tools tests
// ---------------------------------------------------------------------------

func TestToolsList_NotConfigured(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/api/v1/tools", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["message"] != "tool listing not configured" {
		t.Fatalf("expected not configured message, got %v", body["message"])
	}
}

func TestToolsList_WithLister(t *testing.T) {
	tl := &mockToolLister{
		tools: []ToolEntry{
			{Name: "file_read", Description: "Read a file"},
			{Name: "shell", Description: "Run shell command"},
		},
	}
	s := newTestServer(WithToolLister(tl))
	w := doRequest(s, http.MethodGet, "/api/v1/tools", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["count"].(float64) != 2 {
		t.Fatalf("expected 2 tools, got %v", body["count"])
	}
}

// ---------------------------------------------------------------------------
// Memory tests
// ---------------------------------------------------------------------------

func TestMemorySearch_NoQuery(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/api/v1/memory/search", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMemorySearch_NotConfigured(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/memory/search?q=test", http.NoBody)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMemorySearch_WithSearcher(t *testing.T) {
	ms := &mockMemorySearcher{
		results: []MemorySearchResult{
			{ID: "m1", Content: "test memory", Score: 0.95},
		},
	}
	s := newTestServer(WithMemorySearcher(ms))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/memory/search?q=test&limit=5", http.NoBody)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["count"].(float64) != 1 {
		t.Fatalf("expected 1 result, got %v", body["count"])
	}
}

func TestMemoryStore_NotConfigured(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/memory", map[string]string{"content": "test"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestMemoryStore_WithStore(t *testing.T) {
	ms := &mockMemoryStore{result: MemoryStoreResult{ID: "mem-abc"}}
	s := newTestServer(WithMemoryStore(ms))
	w := doRequest(s, http.MethodPost, "/api/v1/memory", map[string]string{
		"content": "test content",
		"type":    "episodic",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["id"] != "mem-abc" {
		t.Fatalf("expected mem-abc, got %v", body["id"])
	}
}

func TestMemoryStore_EmptyContent(t *testing.T) {
	ms := &mockMemoryStore{}
	s := newTestServer(WithMemoryStore(ms))
	w := doRequest(s, http.MethodPost, "/api/v1/memory", map[string]string{"content": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMemoryStore_DefaultType(t *testing.T) {
	ms := &mockMemoryStore{result: MemoryStoreResult{ID: "mem-1"}}
	s := newTestServer(WithMemoryStore(ms))
	w := doRequest(s, http.MethodPost, "/api/v1/memory", map[string]string{"content": "some content"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Skills tests
// ---------------------------------------------------------------------------

func TestSkillsList_NotConfigured(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/api/v1/skills", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["message"] != "skills listing not configured" {
		t.Fatalf("expected not configured message, got %v", body["message"])
	}
}

func TestSkillsList_WithLister(t *testing.T) {
	sl := &mockSkillsLister{
		skills: []SkillInfo{
			{Name: "code-review", Description: "Review code", Tags: []string{"code"}},
		},
	}
	s := newTestServer(WithSkillsLister(sl))
	w := doRequest(s, http.MethodGet, "/api/v1/skills", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["count"].(float64) != 1 {
		t.Fatalf("expected 1 skill, got %v", body["count"])
	}
}

func TestSkillsList_TagFilter(t *testing.T) {
	sl := &mockSkillsLister{
		skills: []SkillInfo{
			{Name: "code-review", Description: "Review code", Tags: []string{"code"}},
			{Name: "planning", Description: "Plan tasks", Tags: []string{"reasoning"}},
		},
	}
	s := newTestServer(WithSkillsLister(sl))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills?tags=code", http.NoBody)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["count"].(float64) != 1 {
		t.Fatalf("expected 1 filtered skill, got %v", body["count"])
	}
}

func TestSkillsExecute_NotConfigured(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/skills/review/execute", map[string]string{"input": "test"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestSkillsExecute_WithExecutor(t *testing.T) {
	se := &mockSkillExecutor{
		result: SkillExecuteResult{Content: "review done", Model: "gpt-4"},
	}
	s := newTestServer(WithSkillExecutor(se))
	w := doRequest(s, http.MethodPost, "/api/v1/skills/review/execute", map[string]string{"input": "review this code"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["content"] != "review done" {
		t.Fatalf("expected 'review done', got %v", body["content"])
	}
}

func TestSkillsExecute_EmptyInput(t *testing.T) {
	se := &mockSkillExecutor{}
	s := newTestServer(WithSkillExecutor(se))
	w := doRequest(s, http.MethodPost, "/api/v1/skills/review/execute", map[string]string{"input": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Jobs tests
// ---------------------------------------------------------------------------

func TestJobsList_NotConfigured(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodGet, "/api/v1/jobs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["message"] != "jobs listing not configured" {
		t.Fatalf("expected not configured message, got %v", body["message"])
	}
}

func TestJobsList_WithLister(t *testing.T) {
	jl := &mockJobsLister{
		jobs: []JobInfo{
			{ID: "j1", Name: "cleanup", Schedule: "0 * * * *"},
		},
	}
	s := newTestServer(WithJobsLister(jl))
	w := doRequest(s, http.MethodGet, "/api/v1/jobs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["count"].(float64) != 1 {
		t.Fatalf("expected 1 job, got %v", body["count"])
	}
}

func TestJobsCreate(t *testing.T) {
	js := &mockJobScheduler{jobID: "job-new"}
	s := newTestServer(WithJobScheduler(js))
	w := doRequest(s, http.MethodPost, "/api/v1/jobs", map[string]string{"name": "test", "schedule": "0 0 * * *"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["id"] != "job-new" {
		t.Fatalf("expected job-new, got %v", body["id"])
	}
}

func TestJobsCreate_NotConfigured(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/jobs", map[string]string{"name": "test"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestJobsGet(t *testing.T) {
	js := &mockJobScheduler{job: map[string]any{"id": "j1", "name": "cleanup"}}
	s := newTestServer(WithJobScheduler(js))
	w := doRequest(s, http.MethodGet, "/api/v1/jobs/j1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestJobsGet_NotFound(t *testing.T) {
	js := &mockJobScheduler{getErr: context.DeadlineExceeded}
	s := newTestServer(WithJobScheduler(js))
	w := doRequest(s, http.MethodGet, "/api/v1/jobs/nonexistent", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestJobsCancel(t *testing.T) {
	js := &mockJobScheduler{}
	s := newTestServer(WithJobScheduler(js))
	w := doRequest(s, http.MethodDelete, "/api/v1/jobs/j1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseBody(t, w)
	if body["ok"] != true {
		t.Fatalf("expected ok true, got %v", body["ok"])
	}
}

func TestJobsCancel_NotFound(t *testing.T) {
	js := &mockJobScheduler{cancelErr: context.DeadlineExceeded}
	s := newTestServer(WithJobScheduler(js))
	w := doRequest(s, http.MethodDelete, "/api/v1/jobs/nonexistent", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Chat Stream (SSE) tests
// ---------------------------------------------------------------------------

func TestChatStream_NotConfigured_Fallback(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/chat/stream", map[string]string{"message": "hi"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Should be SSE response
	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "data:") {
		t.Fatalf("expected SSE data in response, got %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("expected done event in response, got %s", body)
	}
}

func TestChatStream_EmptyMessage(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, http.MethodPost, "/api/v1/chat/stream", map[string]string{"message": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Auth tests
// ---------------------------------------------------------------------------

func TestAuth_Unauthorized(t *testing.T) {
	cfg := DefaultServerConfig()
	handler := &mockHandler{chatResponse: "hi"}
	s := NewServer(cfg, handler, NewBearerAuth("valid-token"), nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	wrapped := s.middleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", http.NoBody)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuth_Authorized(t *testing.T) {
	cfg := DefaultServerConfig()
	handler := &mockHandler{chatResponse: "hi"}
	s := NewServer(cfg, handler, NewBearerAuth("valid-token"), nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	wrapped := s.middleware(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// WebSocket Hub tests
// ---------------------------------------------------------------------------

func TestWebSocketHub_RegisterUnregister(t *testing.T) {
	hub := NewWebSocketHub(nil)
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestWebSocketHub_Broadcast_NoClients(t *testing.T) {
	hub := NewWebSocketHub(nil)
	// Should not panic with no clients
	hub.Broadcast("test", map[string]string{"msg": "hello"})
}

// ---------------------------------------------------------------------------
// Server lifecycle tests
// ---------------------------------------------------------------------------

func TestNewServer_DefaultAddr(t *testing.T) {
	cfg := ServerConfig{} // empty addr
	handler := &mockHandler{}
	s := NewServer(cfg, handler, NoAuth{}, nil)
	if s.config.Addr != ":8080" {
		t.Fatalf("expected :8080, got %s", s.config.Addr)
	}
}

func TestServer_WSHub(t *testing.T) {
	s := newTestServer()
	hub := s.WSHub()
	if hub == nil {
		t.Fatalf("expected non-nil hub")
	}
}

// ---------------------------------------------------------------------------
// parseLimit tests
// ---------------------------------------------------------------------------

func TestParseLimit(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  int
	}{
		{"10", 100, 10},
		{"200", 100, 100},
		{"0", 100, 1},
		{"-5", 100, 1},
	}
	for _, tt := range tests {
		got, err := parseLimit(tt.input, tt.max)
		if err != nil {
			t.Fatalf("parseLimit(%q, %d) error: %v", tt.input, tt.max, err)
		}
		if got != tt.want {
			t.Fatalf("parseLimit(%q, %d) = %d, want %d", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestParseLimit_Invalid(t *testing.T) {
	_, err := parseLimit("abc", 100)
	if err == nil {
		t.Fatalf("expected error for invalid input")
	}
}

// ---------------------------------------------------------------------------
// hasAnyTag tests
// ---------------------------------------------------------------------------

func TestHasAnyTag(t *testing.T) {
	if !hasAnyTag([]string{"code", "review"}, []string{"code"}) {
		t.Fatalf("expected true for matching tag")
	}
	if hasAnyTag([]string{"code"}, []string{"security"}) {
		t.Fatalf("expected false for no matching tags")
	}
	if hasAnyTag([]string{}, []string{"code"}) {
		t.Fatalf("expected false for empty skill tags")
	}
}
