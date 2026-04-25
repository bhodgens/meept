package clawskills

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.baseURL != defaultBaseURL {
		t.Errorf("expected default baseURL %q, got %q", defaultBaseURL, client.baseURL)
	}
	defer client.Close()
}

func TestNewClientWithOptions(t *testing.T) {
	customURL := "https://custom.clawhub.test"
	client := NewClient(WithBaseURL(customURL), WithTimeout(10*time.Second))
	defer client.Close()

	if client.baseURL != customURL {
		t.Errorf("expected baseURL %q, got %q", customURL, client.baseURL)
	}
	if client.httpClient.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", client.httpClient.Timeout)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(3, 1.0) // 3 requests per second

	// Should allow 3 requests
	for i := 0; i < 3; i++ {
		if !rl.acquire() {
			t.Fatalf("request %d should have been allowed", i+1)
		}
	}

	// 4th should be rejected
	if rl.acquire() {
		t.Fatal("4th request should have been rate limited")
	}

	// Wait time should be positive
	wait := rl.waitTime()
	if wait <= 0 {
		t.Error("expected positive wait time after rate limit")
	}
}

func TestRateLimiterRecovery(t *testing.T) {
	rl := newRateLimiter(1, 1.0) // 1 request per 1 second

	if !rl.acquire() {
		t.Fatal("first request should be allowed")
	}

	// Immediately try again -- should be rate limited since the window hasn't expired
	if rl.acquire() {
		t.Fatal("second request should be rate limited")
	}

	// Verify waitTime is positive
	wait := rl.waitTime()
	if wait <= 0 {
		t.Error("expected positive wait time when rate limited")
	}
}

func TestResponseCache(t *testing.T) {
	cache := newResponseCache(100 * time.Millisecond)

	// Cache miss
	if _, ok := cache.get("key1"); ok {
		t.Fatal("expected cache miss for key1")
	}

	// Cache put and hit
	cache.put("key1", "value1")
	if val, ok := cache.get("key1"); !ok || val.(string) != "value1" {
		t.Fatal("expected cache hit for key1")
	}

	// Cache expiry
	time.Sleep(150 * time.Millisecond)
	if _, ok := cache.get("key1"); ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestCacheInvalidate(t *testing.T) {
	cache := newResponseCache(1 * time.Minute)
	cache.put("key1", "value1")
	cache.invalidate("key1")

	if _, ok := cache.get("key1"); ok {
		t.Fatal("expected cache miss after invalidation")
	}
}

func TestCacheClear(t *testing.T) {
	cache := newResponseCache(1 * time.Minute)
	cache.put("key1", "value1")
	cache.put("key2", "value2")
	cache.clear()

	if _, ok := cache.get("key1"); ok {
		t.Fatal("expected cache miss after clear")
	}
	if _, ok := cache.get("key2"); ok {
		t.Fatal("expected cache miss after clear")
	}
}

func TestSearch(t *testing.T) {
	results := []SearchResult{
		{Slug: "test-skill", Name: "Test Skill", Version: "1.0.0", Score: 0.95},
		{Slug: "other-skill", Name: "Other", Version: "2.0.0", Score: 0.5},
	}
	body, _ := json.Marshal(results)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Errorf("expected /api/v1/search, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	defer client.Close()

	searchResults, err := client.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(searchResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(searchResults))
	}
	if searchResults[0].Slug != "test-skill" {
		t.Errorf("expected slug 'test-skill', got %q", searchResults[0].Slug)
	}
}

func TestSearchWrapped(t *testing.T) {
	wrapper := struct {
		Results []SearchResult `json:"results"`
	}{
		Results: []SearchResult{
			{Slug: "wrapped-skill", Name: "Wrapped", Version: "1.0.0"},
		},
	}
	body, _ := json.Marshal(wrapper)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	defer client.Close()

	results, err := client.Search(context.Background(), "wrapped", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 || results[0].Slug != "wrapped-skill" {
		t.Errorf("expected wrapped-skill, got %v", results)
	}
}

func TestSkillDetail(t *testing.T) {
	skill := RemoteSkill{
		Slug: "my-skill", Name: "My Skill", Version: "1.0.0",
		Description: "A test skill", Author: "test",
	}
	body, _ := json.Marshal(skill)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/my-skill" {
			t.Errorf("expected /api/v1/skills/my-skill, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	defer client.Close()

	result, err := client.SkillDetail(context.Background(), "my-skill")
	if err != nil {
		t.Fatalf("SkillDetail failed: %v", err)
	}
	if result.Slug != "my-skill" {
		t.Errorf("expected slug 'my-skill', got %q", result.Slug)
	}
}

func TestAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	defer client.Close()

	_, err := client.SkillDetail(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", apiErr.StatusCode)
	}
}

func TestDownloadSizeLimit(t *testing.T) {
	// Create a server that returns a response larger than maxDownloadBytes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write more than 10MB
		large := make([]byte, maxDownloadBytes+1)
		w.Write(large)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	defer client.Close()

	_, err := client.Download(context.Background(), "big-skill", "")
	if err == nil {
		t.Fatal("expected error for oversized download")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 413 {
		t.Errorf("expected status 413, got %d", apiErr.StatusCode)
	}
}

func TestCaching(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		results := []SearchResult{{Slug: "cached"}}
		body, _ := json.Marshal(results)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	defer client.Close()

	// First call
	_, err := client.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("first search failed: %v", err)
	}

	// Second call should be cached
	_, err = client.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("second search failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 API call (second cached), got %d", callCount)
	}
}

func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithTimeout(10*time.Second))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Search(ctx, "test", 10)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
