package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/services"
)

func newTestPromptServer(t *testing.T) (*Server, string) {
	t.Helper()
	tmp := t.TempDir()
	projectDir := filepath.Join(tmp, "project")
	userDir := filepath.Join(tmp, "user")
	systemDir := filepath.Join(tmp, "system")
	bundledDir := filepath.Join(tmp, "bundled")

	// Seed a bundled template
	mustWritePromptFile(t, filepath.Join(bundledDir, "planner", "decompose.md"), "---\nname: x\n---\nHELLO {{.Input}}")
	mustWritePromptFile(t, filepath.Join(bundledDir, "planner", "interview.md"), "INTERVIEW {{.Request}}")

	svc := services.NewPromptService(projectDir, userDir, systemDir, bundledDir)
	reg := &services.ServiceRegistry{Prompt: svc}
	server := NewServer(ServerConfig{}, nil, nil, nil, reg, nil)
	return server, tmp
}

func TestHandlePromptsList(t *testing.T) {
	server, _ := newTestPromptServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts", http.NoBody)
	w := httptest.NewRecorder()

	server.handlePromptsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result struct {
		Prompts []services.PromptEntry `json:"prompts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Prompts) != 2 {
		t.Errorf("expected 2 prompts, got %d", len(result.Prompts))
	}
}

func TestHandlePromptsList_ServiceUnavailable(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts", http.NoBody)
	w := httptest.NewRecorder()

	server.handlePromptsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandlePromptsGet(t *testing.T) {
	server, _ := newTestPromptServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/planner%2Fdecompose.md", http.NoBody)
	req.SetPathValue("path", "planner/decompose.md")
	w := httptest.NewRecorder()

	server.handlePromptsGet(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var detail services.PromptDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Name != "planner/decompose.md" {
		t.Errorf("name = %s", detail.Name)
	}
	if detail.Content == "" {
		t.Error("content is empty")
	}
}

func TestHandlePromptsGet_NotFound(t *testing.T) {
	server, _ := newTestPromptServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/nonexistent.md", http.NoBody)
	req.SetPathValue("path", "planner/nonexistent.md")
	w := httptest.NewRecorder()

	server.handlePromptsGet(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandlePromptsPut(t *testing.T) {
	server, _ := newTestPromptServer(t)

	body := `{"content": "---\nname: override\n---\nOVERRIDE {{.Input}}"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/prompts/planner%2Fdecompose.md", strReader(body))
	req.SetPathValue("path", "planner/decompose.md")
	w := httptest.NewRecorder()

	server.handlePromptsPut(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %v", resp.StatusCode, http.StatusOK, resp.Body)
	}
}

func TestHandlePromptsPut_ValidationFails(t *testing.T) {
	server, _ := newTestPromptServer(t)

	body := `{"content": "{{ .Broken"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/prompts/planner%2Fbroken.md", strReader(body))
	req.SetPathValue("path", "planner/broken.md")
	w := httptest.NewRecorder()

	server.handlePromptsPut(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandlePromptsDelete(t *testing.T) {
	server, _ := newTestPromptServer(t)

	// First PUT to create an override
	putBody := `{"content": "OVERRIDE {{.X}}"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/prompts/planner%2Fdecompose.md", strReader(putBody))
	req.SetPathValue("path", "planner/decompose.md")
	w := httptest.NewRecorder()
	server.handlePromptsPut(w, req)

	// Now DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/prompts/planner%2Fdecompose.md", http.NoBody)
	req.SetPathValue("path", "planner/decompose.md")
	w = httptest.NewRecorder()

	server.handlePromptsDelete(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandlePromptsDelete_NoOverride(t *testing.T) {
	server, _ := newTestPromptServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/prompts/planner%2Fdecompose.md", http.NoBody)
	req.SetPathValue("path", "planner/decompose.md")
	w := httptest.NewRecorder()

	server.handlePromptsDelete(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandlePromptsValidate_All(t *testing.T) {
	server, _ := newTestPromptServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts/validate", strReader(`{}`))
	w := httptest.NewRecorder()

	server.handlePromptsValidate(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandlePromptsValidate_Single(t *testing.T) {
	server, _ := newTestPromptServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts/validate", strReader(`{"name": "planner/decompose.md"}`))
	w := httptest.NewRecorder()

	server.handlePromptsValidate(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// --- helpers ---

func mustWritePromptFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func strReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
