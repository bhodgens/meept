package compress

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestContentHash_Deterministic(t *testing.T) {
	content := "test content for hashing"
	h1 := ContentHash(content)
	h2 := ContentHash(content)

	if h1 != h2 {
		t.Errorf("ContentHash not deterministic: %s != %s", h1, h2)
	}

	if len(h1) != HashLength {
		t.Errorf("Hash length wrong: %d != %d", len(h1), HashLength)
	}
}

func TestContentHash_DifferentContent(t *testing.T) {
	h1 := ContentHash("content1")
	h2 := ContentHash("content2")

	if h1 == h2 {
		t.Errorf("Different content produced same hash")
	}
}

func TestMarkerFormat(t *testing.T) {
	hash := "abc123def456789012345678"
	marker := MarkerFormat(hash)
	expected := "<<ccr:abc123def456789012345678>>"

	if marker != expected {
		t.Errorf("MarkerFormat wrong: %s != %s", marker, expected)
	}
}

func TestParseMarker(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<<ccr:abc123def456789012345678>>", "abc123def456789012345678"},
		{"<<ccr:123456789012345678901234>>", "123456789012345678901234"},
		{"invalid", ""},
		{"<<ccr:short>>", ""},
		{"ccr:abc123def456789012345678", ""},
	}

	for _, tt := range tests {
		got := ParseMarker(tt.input)
		if got != tt.expected {
			t.Errorf("ParseMarker(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSmartCrusher_JSONArray(t *testing.T) {
	sc := NewSmartCrusher(DefaultSmartCrusherConfig())

	// Create a JSON array with duplicates
	items := make([]map[string]interface{}, 100)
	for i := 0; i < 100; i++ {
		items[i] = map[string]interface{}{
			"id":      i,
			"name":    "item",
			"value":   42,
			"padding": "This is some padding text to make the item larger",
		}
	}

	data, _ := json.Marshal(items)
	content := string(data)

	compressed, result := sc.Crush(content)

	if result.TokensSaved <= 0 {
		t.Errorf("Expected tokens saved, got %d", result.TokensSaved)
	}

	if result.CompressionRatio >= 1.0 {
		t.Errorf("Expected compression ratio < 1.0, got %f", result.CompressionRatio)
	}

	// Verify compressed is smaller
	if len(compressed) >= len(content) {
		t.Errorf("Compressed not smaller: %d >= %d", len(compressed), len(content))
	}
}

func TestSmartCrusher_ErrorPreservation(t *testing.T) {
	sc := NewSmartCrusher(SmartCrusherConfig{
		KeepFirstN:     5,
		KeepLastN:      5,
		PreserveErrors: true,
		MaxArrayItems:  20,
	})

	// Create array with error in the middle
	items := []interface{}{
		map[string]interface{}{"id": 1, "data": "ok"},
		map[string]interface{}{"id": 2, "data": "ok"},
		map[string]interface{}{"error": "Something went wrong", "code": 500},
		map[string]interface{}{"id": 4, "data": "ok"},
	}

	for i := 4; i < 50; i++ {
		items = append(items, map[string]interface{}{"id": i, "data": "ok"})
	}

	data, _ := json.Marshal(items)
	content := string(data)

	compressed, result := sc.Crush(content)

	// Error should be preserved
	if !containsString(result.TransformsApplied, "error_preserved") {
		t.Errorf("Expected error to be preserved")
	}

	_ = compressed
}

func TestCodeCompressor(t *testing.T) {
	cc := NewCodeCompressor(DefaultCodeCompressorConfig())

	content := `package main

import "fmt"

func main() {
    fmt.Println("Hello")
}

func someFunction() {
    // lots of code here
    // line 1
    // line 2
    // line 3
    // line 4
    // line 5
    // line 6
    // line 7
    // line 8
    // line 9
    // line 10
    // line 11
    // line 12
    // line 13
    // line 14
    // line 15
}
`

	compressed, result := cc.Crush(content, "go")

	if result.Strategy != StrategyCode {
		t.Errorf("Expected StrategyCode, got %v", result.Strategy)
	}

	_ = compressed
}

func TestContentRouter_Detection(t *testing.T) {
	router := NewContentRouter(DefaultContentRouterConfig())

	tests := []struct {
		content  string
		expected ContentType
	}{
		{`{"key": "value"}`, ContentJSON},
		{`[1, 2, 3]`, ContentJSON},
		{"2024-01-01 ERROR: something failed", ContentLogs},
		{"file.go:42: match1\nfile.go:100: match2", ContentSearch},
		{"diff --git a/file.go b/file.go", ContentDiff},
		{"func foo() {}", ContentCode},
		{"just plain text", ContentText},
	}

	for _, tt := range tests {
		got := router.DetectType(tt.content)
		if got != tt.expected {
			t.Errorf("DetectType(%q) = %v, want %v", tt.content, got, tt.expected)
		}
	}
}

func TestPipeline_Compress(t *testing.T) {
	// Create in-memory test store
	store := newMemStore()
	pipeline := NewPipeline(store)
	defer pipeline.Close()

	messages := []Message{
		{Role: "user", Content: "What is Go?"},
		{Role: "assistant", Content: "Go is a programming language."},
		{Role: "tool", Content: createLargeJSONOutput()},
	}

	cfg := CompressConfig{
		MinTokensToCompress: 100,
	}

	result, err := pipeline.Compress(context.Background(), messages, cfg)
	if err != nil {
		t.Fatalf("Pipeline.Compress failed: %v", err)
	}

	if result.TokensBefore == 0 {
		t.Error("Expected TokensBefore > 0")
	}

	if len(result.Messages) != len(messages) {
		t.Errorf("Expected %d messages, got %d", len(messages), len(result.Messages))
	}
}

func createLargeJSONOutput() string {
	items := make([]map[string]interface{}, 50)
	for i := 0; i < 50; i++ {
		items[i] = map[string]interface{}{
			"id":      i,
			"path":    "/some/long/path/that/makes/it/bigger",
			"matches": []string{"result1", "result2", "result3"},
		}
	}
	data, _ := json.Marshal(items)
	return string(data)
}

// memStore is an in-memory CCR store for testing.
type memStore struct {
	data map[string]*CCREntry
	mu   sync.RWMutex
}

func newMemStore() *memStore {
	return &memStore{
		data: make(map[string]*CCREntry),
	}
}

func (s *memStore) Store(ctx context.Context, entry CCREntry) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hash := entry.Hash
	if hash == "" {
		hash = ContentHash(entry.OriginalContent)
	}
	s.data[hash] = &entry
	return hash, nil
}

func (s *memStore) Retrieve(ctx context.Context, hash string) (*CCREntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.data[hash]
	if !ok {
		return nil, nil
	}
	return entry, nil
}

func (s *memStore) Search(ctx context.Context, hash, query string) ([]CCRSearchResult, error) {
	entry, err := s.Retrieve(ctx, hash)
	if err != nil || entry == nil {
		return nil, nil
	}

	idx := findSubstring(entry.OriginalContent, query)
	if idx < 0 {
		return nil, nil
	}

	return []CCRSearchResult{
		{Hash: hash, MatchedContent: query, Score: 1.0},
	}, nil
}

func (s *memStore) Exists(ctx context.Context, hash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.data[hash]
	return ok
}

func (s *memStore) Delete(ctx context.Context, hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data[hash]; !ok {
		return false, nil
	}
	delete(s.data, hash)
	return true, nil
}

func (s *memStore) Stats() CCRStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var totalOriginal, totalCompressed int64
	for _, e := range s.data {
		totalOriginal += int64(e.OriginalTokens)
		totalCompressed += int64(e.CompressedTokens)
	}

	return CCRStats{
		EntryCount:              int64(len(s.data)),
		TotalOriginalTokens:     totalOriginal,
		TotalCompressedTokens:   totalCompressed,
		TotalRetrievals:         0,
		ExpiredCount:            0,
	}
}

func (s *memStore) Close() error {
	return nil
}
