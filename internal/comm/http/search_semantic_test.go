package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/internal/session"
)

func TestHandleSearchSemantic_OK(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(nil)
	searchSvc := services.NewSearchService(store, nil, nil, nil)
	svcReg := &services.ServiceRegistry{Search: searchSvc}
	server := NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)

	// Seed store so keyword fallback returns hits.
	sess, _ := store.Create("test session")
	_ = store.SaveMessages(sess.ID, []session.Message{
		{Role: "user", Content: "golang test message"},
		{Role: "assistant", Content: "response about golang"},
	})

	body := `{"query":"golang"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search/semantic", strings.NewReader(body))
	w := httptest.NewRecorder()

	server.handleSearchSemantic(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var sr services.SemanticSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if sr.Mode == "" {
		t.Error("expected non-empty Mode")
	}
	// Mode should be "keyword" since no embedder is configured.
	if sr.Mode != "keyword" {
		t.Errorf("Mode = %q, want %q", sr.Mode, "keyword")
	}
}

func TestHandleSearchSemantic_EmptyQuery(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(nil)
	searchSvc := services.NewSearchService(store, nil, nil, nil)
	svcReg := &services.ServiceRegistry{Search: searchSvc}
	server := NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)

	body := `{"query":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search/semantic", strings.NewReader(body))
	w := httptest.NewRecorder()

	server.handleSearchSemantic(w, req)

	resp := w.Result()
	if resp.StatusCode < 400 {
		t.Errorf("status = %d, want 4xx", resp.StatusCode)
	}
}

func TestHandleSearchSemantic_InvalidBody(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(nil)
	searchSvc := services.NewSearchService(store, nil, nil, nil)
	svcReg := &services.ServiceRegistry{Search: searchSvc}
	server := NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/search/semantic", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	server.handleSearchSemantic(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleSearchSemantic_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	// No services wired.
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	body := `{"query":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/search/semantic", strings.NewReader(body))
	w := httptest.NewRecorder()

	server.handleSearchSemantic(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if respBody["error"] != "search service not available" {
		t.Errorf("error = %s, want 'search service not available'", respBody["error"])
	}
}
