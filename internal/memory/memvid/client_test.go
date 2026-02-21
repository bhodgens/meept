package memvid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Store(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/store" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req StoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Content != "test content" {
			t.Errorf("unexpected content: %s", req.Content)
		}
		if req.Zone != "test-zone" {
			t.Errorf("unexpected zone: %s", req.Zone)
		}

		resp := StoreResponse{
			ID:      "abc123",
			Success: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: server.URL,
		Zone:     "test-zone",
	})

	id, err := client.Store(context.Background(), "test content", map[string]any{
		"key": "value",
	})
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}
	if id != "abc123" {
		t.Errorf("unexpected ID: %s", id)
	}
}

func TestClient_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Query != "test query" {
			t.Errorf("unexpected query: %s", req.Query)
		}
		if req.Limit != 5 {
			t.Errorf("unexpected limit: %d", req.Limit)
		}

		resp := SearchResponse{
			Results: []MemoryResult{
				{
					Memory: Memory{
						ID:        "mem1",
						Content:   "matched content",
						Zone:      "test-zone",
						CreatedAt: time.Now(),
					},
					RelevanceScore: 0.95,
				},
			},
			Total: 1,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: server.URL,
		Zone:     "test-zone",
	})

	results, err := client.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Memory.ID != "mem1" {
		t.Errorf("unexpected memory ID: %s", results[0].Memory.ID)
	}
	if results[0].RelevanceScore != 0.95 {
		t.Errorf("unexpected relevance score: %f", results[0].RelevanceScore)
	}
}

func TestClient_GetByIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/get" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req GetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if len(req.IDs) != 2 {
			t.Errorf("unexpected IDs length: %d", len(req.IDs))
		}

		resp := GetResponse{
			Memories: []Memory{
				{
					ID:        "id1",
					Content:   "content 1",
					Zone:      "test-zone",
					CreatedAt: time.Now(),
				},
				{
					ID:        "id2",
					Content:   "content 2",
					Zone:      "test-zone",
					CreatedAt: time.Now(),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: server.URL,
		Zone:     "test-zone",
	})

	memories, err := client.GetByIDs(context.Background(), []string{"id1", "id2"})
	if err != nil {
		t.Fatalf("GetByIDs failed: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(memories))
	}
}

func TestClient_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/delete" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req DeleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.ID != "to-delete" {
			t.Errorf("unexpected ID: %s", req.ID)
		}

		resp := DeleteResponse{
			Success: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: server.URL,
		Zone:     "test-zone",
	})

	err := client.Delete(context.Background(), "to-delete")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestClient_Health(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}

		resp := HealthResponse{
			Status:    "ok",
			Zones:     3,
			Memories:  100,
			DiskUsage: 1024000,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: server.URL,
		Zone:     "test-zone",
	})

	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("unexpected status: %s", health.Status)
	}
	if health.Zones != 3 {
		t.Errorf("unexpected zones: %d", health.Zones)
	}
}

func TestClient_IsAvailable(t *testing.T) {
	// Test when service is available
	availableServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := HealthResponse{Status: "ok"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer availableServer.Close()

	client := NewClient(ClientConfig{Endpoint: availableServer.URL})
	if !client.IsAvailable(context.Background()) {
		t.Error("expected service to be available")
	}

	// Test when service is unavailable
	unavailableClient := NewClient(ClientConfig{Endpoint: "http://localhost:99999"})
	if unavailableClient.IsAvailable(context.Background()) {
		t.Error("expected service to be unavailable")
	}
}

func TestClient_WithZone(t *testing.T) {
	client := NewClient(ClientConfig{
		Endpoint: "http://localhost:8765",
		Zone:     "zone1",
	})

	if client.Zone() != "zone1" {
		t.Errorf("unexpected zone: %s", client.Zone())
	}

	client2 := client.WithZone("zone2")
	if client2.Zone() != "zone2" {
		t.Errorf("unexpected zone: %s", client2.Zone())
	}

	// Original client should be unchanged
	if client.Zone() != "zone1" {
		t.Errorf("original client zone changed: %s", client.Zone())
	}
}

func TestClient_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: server.URL,
		Zone:     "test-zone",
	})

	_, err := client.Store(context.Background(), "content", nil)
	if err == nil {
		t.Error("expected error for server error")
	}
}

func TestClient_StoreError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := StoreResponse{
			ID:      "",
			Success: false,
			Error:   "storage full",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Endpoint: server.URL,
		Zone:     "test-zone",
	})

	_, err := client.Store(context.Background(), "content", nil)
	if err == nil {
		t.Error("expected error for store error response")
	}
}
