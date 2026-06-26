package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/services"
)

// newTestReflectionServer constructs a Server with a stub services registry
// backed by a ReflectionService writing to a temp directory. If nilServices is
// true, the services field is set to nil to test the 503 path.
func newTestReflectionServer(t *testing.T, nilServices bool) *Server {
	t.Helper()
	if nilServices {
		return &Server{}
	}
	tmp := t.TempDir()
	svc := services.NewReflectionService(filepath.Join(tmp, "improvements.md"))
	return &Server{
		services: &services.ServiceRegistry{
			Reflection: svc,
		},
	}
}

// newTestReflectionServerWithProposal creates a server backed by a ReflectionService
// that already has one pending proposal. Returns the server and the proposal ID.
func newTestReflectionServerWithProposal(t *testing.T) (*Server, string) {
	t.Helper()
	tmp := t.TempDir()
	queuePath := filepath.Join(tmp, "improvements.md")
	queue := agent.NewExternalProposalQueue(queuePath)
	proposal := agent.ReflectionProposal{
		Type:   "skill_create",
		Target: ".meept/skills/test/SKILL.md",
		Change: "add a new skill",
	}
	if err := queue.Append(proposal); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	svc := services.NewReflectionService(queuePath)
	pending, err := svc.ListPending()
	if err != nil || len(pending) != 1 {
		t.Fatalf("setup failed: pending=%v err=%v", pending, err)
	}
	return &Server{
		services: &services.ServiceRegistry{
			Reflection: svc,
		},
	}, pending[0].ID
}

func TestHandleReflectionList_Empty(t *testing.T) {
	t.Run("nil services returns 503", func(t *testing.T) {
		s := newTestReflectionServer(t, true)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reflection/proposals", http.NoBody)
		w := httptest.NewRecorder()

		s.handleReflectionList(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("empty queue returns 200 with empty list", func(t *testing.T) {
		s := newTestReflectionServer(t, false)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/reflection/proposals", http.NoBody)
		w := httptest.NewRecorder()

		s.handleReflectionList(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		var body struct {
			Proposals []agent.ReflectionProposal `json:"proposals"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(body.Proposals) != 0 {
			t.Errorf("expected 0 proposals, got %d", len(body.Proposals))
		}
	})
}

func TestHandleReflectionList(t *testing.T) {
	s, _ := newTestReflectionServerWithProposal(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reflection/proposals", http.NoBody)
	w := httptest.NewRecorder()

	s.handleReflectionList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	bodyStr := w.Body.String()
	if !bytes.Contains([]byte(bodyStr), []byte(".meept/skills/test/SKILL.md")) {
		t.Errorf("response body does not contain target string: %s", bodyStr)
	}
}

func TestHandleReflectionApply(t *testing.T) {
	s, id := newTestReflectionServerWithProposal(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reflection/proposals/"+id+"/apply", http.NoBody)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()

	s.handleReflectionApply(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "applied" {
		t.Errorf("status = %q, want %q", body["status"], "applied")
	}
	if body["id"] != id {
		t.Errorf("id = %q, want %q", body["id"], id)
	}
}

func TestHandleReflectionApply_MissingID(t *testing.T) {
	s := newTestReflectionServer(t, false)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reflection/proposals//apply", http.NoBody)
	req.SetPathValue("id", "")
	w := httptest.NewRecorder()

	s.handleReflectionApply(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleReflectionSkip(t *testing.T) {
	s, id := newTestReflectionServerWithProposal(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reflection/proposals/"+id+"/skip", http.NoBody)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()

	s.handleReflectionSkip(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "skipped" {
		t.Errorf("status = %q, want %q", body["status"], "skipped")
	}
	if body["id"] != id {
		t.Errorf("id = %q, want %q", body["id"], id)
	}
}

func TestHandleReflectionRemember(t *testing.T) {
	s := newTestReflectionServer(t, false)

	payload := map[string]string{
		"target":        "CLAUDE.md",
		"change":        "add a new rule",
		"justification": "improves consistency",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reflection/remember", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleReflectionRemember(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "queued" {
		t.Errorf("status = %q, want %q", body["status"], "queued")
	}
	if body["target"] != "CLAUDE.md" {
		t.Errorf("target = %q, want %q", body["target"], "CLAUDE.md")
	}
}

func TestHandleReflectionRemember_MissingFields(t *testing.T) {
	s := newTestReflectionServer(t, false)

	payload := map[string]string{
		"target": "",
		"change": "some change",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reflection/remember", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleReflectionRemember(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleReflectionRemember_NoServices(t *testing.T) {
	s := newTestReflectionServer(t, true)

	payload := map[string]string{
		"target": "CLAUDE.md",
		"change": "test",
	}
	bodyBytes, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reflection/remember", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleReflectionRemember(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
