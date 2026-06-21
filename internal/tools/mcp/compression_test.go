package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/caimlas/meept/internal/compress"
)

// testCCRStore is a minimal in-memory CCR store for compression handler tests.
type testCCRStore struct {
	data   map[string]*compress.CCREntry
	stats  compress.CCRStats
}

func newTestCCRStore() *testCCRStore {
	return &testCCRStore{data: make(map[string]*compress.CCREntry)}
}

func (s *testCCRStore) Store(_ context.Context, entry compress.CCREntry) (string, error) {
	hash := compress.ContentHash(entry.OriginalContent)
	entry.Hash = hash
	s.data[hash] = &entry
	return hash, nil
}

func (s *testCCRStore) Retrieve(_ context.Context, hash string) (*compress.CCREntry, error) {
	s.stats.TotalRetrievals++
	return s.data[hash], nil
}

func (s *testCCRStore) Search(_ context.Context, _, _ string) ([]compress.CCRSearchResult, error) {
	return nil, nil
}

func (s *testCCRStore) Exists(_ context.Context, hash string) bool {
	_, ok := s.data[hash]
	return ok
}

func (s *testCCRStore) Delete(_ context.Context, hash string) (bool, error) {
	if _, ok := s.data[hash]; !ok {
		return false, nil
	}
	delete(s.data, hash)
	return true, nil
}

func (s *testCCRStore) Stats() compress.CCRStats {
	var orig, comp int64
	for _, e := range s.data {
		orig += int64(e.OriginalTokens)
		comp += int64(e.CompressedTokens)
	}
	return compress.CCRStats{
		EntryCount:          int64(len(s.data)),
		TotalOriginalTokens: orig,
		TotalCompressedTokens: comp,
		TotalRetrievals:     s.stats.TotalRetrievals,
	}
}

func (s *testCCRStore) Close() error { return nil }

// nilStore wraps a store but returns nil for Retrieve.
type nilStore struct {
	wrap *testCCRStore
}

func (s *nilStore) Store(ctx context.Context, e compress.CCREntry) (string, error) {
	return s.wrap.Store(ctx, e)
}
func (s *nilStore) Retrieve(ctx context.Context, hash string) (*compress.CCREntry, error) {
	s.wrap.stats.TotalRetrievals++
	return nil, nil
}
func (s *nilStore) Search(ctx context.Context, h, q string) ([]compress.CCRSearchResult, error) {
	return nil, nil
}
func (s *nilStore) Exists(ctx context.Context, h string) bool              { return false }
func (s *nilStore) Delete(ctx context.Context, h string) (bool, error)    { return false, nil }
func (s *nilStore) Stats() compress.CCRStats                              { return compress.CCRStats{} }
func (s *nilStore) Close() error                                          { return nil }

func TestNewCompressionHandler_Disabled(t *testing.T) {
	cfg := DefaultCompressionConfig()
	cfg.Enabled = false
	h := NewCompressionHandler(nil, nil, cfg)
	if h != nil {
		t.Error("expected nil handler when config.Enabled is false")
	}
}

func TestNewCompressionHandler_NoTools(t *testing.T) {
	cfg := DefaultCompressionConfig()
	cfg.Enabled = true
	h := NewCompressionHandler(nil, nil, cfg)
	if h != nil {
		t.Error("expected nil when neither pipeline nor store available")
	}
}

func TestCompressionHandler_Tools(t *testing.T) {
	store := newTestCCRStore()
	cfg := DefaultCompressionConfig()
	cfg.Enabled = true
	h := NewCompressionHandler(nil, store, cfg)
	if h == nil {
		t.Fatal("expected non-nil handler with store available")
	}

	tools := h.Tools()
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}

	wantNames := map[string]bool{
		"mcc_compress":  false,
		"mcc_retrieve":  false,
		"mcc_stats":     false,
	}
	for _, tool := range tools {
		wantNames[tool.Function.Name] = true
	}
	if !wantNames["mcc_compress"] {
		t.Error("missing mcc_compress tool")
	}
	if !wantNames["mcc_retrieve"] {
		t.Error("missing mcc_retrieve tool")
	}
	if !wantNames["mcc_stats"] {
		t.Error("missing mcc_stats tool")
	}
}

func TestExecCompress_MissingContent(t *testing.T) {
	cfg := DefaultCompressionConfig()
	h := &CompressionHandler{config: cfg}

	_, err := h.execCompress(nil, map[string]any{})
	if err == nil {
		t.Error("expected error for missing content")
	}

	_, err = h.execCompress(nil, map[string]any{"content": ""})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestExecCompress_NilPipeline(t *testing.T) {
	store := newTestCCRStore()
	cfg := DefaultCompressionConfig()
	cfg.Enabled = true
	h := &CompressionHandler{config: cfg, ccrStore: store}

	_, err := h.execCompress(nil, map[string]any{"content": "test"})
	if err == nil {
		t.Error("expected error for nil pipeline")
	}
}

func TestExecRetrieve_MissingHash(t *testing.T) {
	cfg := DefaultCompressionConfig()
	h := &CompressionHandler{config: cfg}

	_, err := h.execRetrieve(nil, map[string]any{})
	if err == nil {
		t.Error("expected error for missing hash")
	}
}

func TestExecRetrieve_NilStore(t *testing.T) {
	cfg := DefaultCompressionConfig()
	h := &CompressionHandler{config: cfg}

	_, err := h.execRetrieve(nil, map[string]any{"hash": "abc123"})
	if err == nil {
		t.Error("expected error for nil store")
	}
}

func TestExecRetrieve_NotFound(t *testing.T) {
	nstore := &nilStore{wrap: newTestCCRStore()}
	cfg := DefaultCompressionConfig()
	cfg.Enabled = true
	h := &CompressionHandler{config: cfg, ccrStore: nstore}

	result, err := h.execRetrieve(nil, map[string]any{"hash": "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if found, ok := m["found"].(bool); !ok || found {
		t.Errorf("expected found=false, got %v", m["found"])
	}
	if original, ok := m["original"].(string); !ok || original != "" {
		t.Errorf("expected empty original, got %q", original)
	}
}

func TestExecStats(t *testing.T) {
	store := newTestCCRStore()
	cfg := DefaultCompressionConfig()
	cfg.Enabled = true
	h := &CompressionHandler{config: cfg, ccrStore: store}

	result, err := h.execStats(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if _, ok := m["entry_count"]; !ok {
		t.Error("missing entry_count in stats")
	}
	if _, ok := m["total_saved"]; !ok {
		t.Error("missing total_saved in stats")
	}
	if _, ok := m["retrieval_count"]; !ok {
		t.Error("missing retrieval_count in stats")
	}
}

func TestExecute_UnknownTool(t *testing.T) {
	cfg := DefaultCompressionConfig()
	h := &CompressionHandler{config: cfg}

	_, err := h.Execute(nil, "unknown_tool", map[string]any{})
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestExtractHashFromContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "standard CCR marker",
			content:  "some compressed text\n\n<<ccr:abcdef123456abcdef123456>>",
			expected: "abcdef123456abcdef123456",
		},
		{
			name:     "verbose hash marker",
			content:  "compressed [42 items compressed to 100 tokens, hash=abcdef123456abcdef123456]",
			expected: "abcdef123456abcdef123456",
		},
		{
			name:     "no marker",
			content:  "no hash here",
			expected: "",
		},
		{
			name:     "short hash in marker",
			content:  "<<ccr:short>>",
			expected: "",
		},
		{
			name:     "trailing whitespace",
			content:  "compressed\n\n<<ccr:abcdef123456abcdef123456>>\n",
			expected: "abcdef123456abcdef123456",
		},
		{
			name:     "hash too long",
			content:  "<<ccr:abcdef123456abcdef1234567890>>extra",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHashFromContent(tt.content)
			if result != tt.expected {
				t.Errorf("extractHashFromContent(%q) = %q, want %q", tt.content, result, tt.expected)
			}
		})
	}
}

func TestExtractHashFromContent_WithCompressedPrefix(t *testing.T) {
	// Verify the standard marker extraction works when marker is embedded
	// in a larger compressed string with content before it.
	content := `Array of 100 entries compressed to 3 entries.
Keys: id, name, status.
See <<ccr:abcdef123456abcdef123456>>
`
	hash := extractHashFromContent(content)
	if hash != "abcdef123456abcdef123456" {
		t.Errorf("expected extracted hash, got %q", hash)
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input  string
		min    int
		max    int
	}{
		{"", 0, 0},
		{"hello", 0, 5},
		{"this is a test string for token estimation", 5, 20},
		{"a", 0, 3},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d chars", len(tt.input)), func(t *testing.T) {
			result := estimateTokens(tt.input)
			if result < tt.min || result > tt.max {
				t.Errorf("estimateTokens(%q) = %d, expected [%d, %d]", tt.input, result, tt.min, tt.max)
			}
		})
	}
}
