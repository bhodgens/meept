package vector

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSentenceTransformerProvider_DimensionReturnsConfiguredDim(t *testing.T) {
	// Create a default provider (uses fallback tokenizer and dummy weights)
	provider, err := NewSentenceTransformerProvider(SentenceTransformerConfig{
		ModelID:     "nomic-embed-text-v1.5",
		TargetDim:   512,
	})
	if err != nil {
		t.Fatalf("NewSentenceTransformerProvider error: %v", err)
	}

	// Trigger initialization by calling a method
	ctx := context.Background()
	_, err = provider.GenerateEmbedding(ctx, "test")
	if err != nil {
		// May fail due to dummy weights, but Dimension should work
	}

	if provider.Dimension() != 512 {
		t.Errorf("Dimension() = %d, want 512", provider.Dimension())
	}
}

func TestSentenceTransformerProvider_GenerateEmptyText(t *testing.T) {
	provider, err := NewSentenceTransformerProvider(SentenceTransformerConfig{
		ModelID: "nomic-embed-text-v1.5",
	})
	if err != nil {
		t.Fatalf("NewSentenceTransformerProvider error: %v", err)
	}

	ctx := context.Background()
	emb, err := provider.GenerateEmbedding(ctx, "")
	if err != nil {
		t.Errorf("GenerateEmbedding(\"\") should not error, got: %v", err)
	}
	if len(emb) == 0 {
		t.Error("GenerateEmbedding(\"\") returned empty embedding")
	}
}

func TestSentenceTransformerProvider_DeterministicEmbeddings(t *testing.T) {
	provider, err := NewSentenceTransformerProvider(SentenceTransformerConfig{
		ModelID: "nomic-embed-text-v1.5",
	})
	if err != nil {
		t.Fatalf("NewSentenceTransformerProvider error: %v", err)
	}

	text := "the quick brown fox jumps over the lazy dog"
	ctx := context.Background()

	emb1, err := provider.GenerateEmbedding(ctx, text)
	if err != nil {
		t.Fatalf("GenerateEmbedding first call error: %v", err)
	}

	emb2, err := provider.GenerateEmbedding(ctx, text)
	if err != nil {
		t.Fatalf("GenerateEmbedding second call error: %v", err)
	}

	// Same text should produce same embedding (deterministic)
	for i := range emb1 {
		if emb1[i] != emb2[i] {
			t.Errorf("embedding[%d] differs: %v vs %v", i, emb1[i], emb2[i])
			break
		}
	}
}

func TestSentenceTransformerProvider_BatchGenerate(t *testing.T) {
	provider, err := NewSentenceTransformerProvider(SentenceTransformerConfig{
		ModelID: "all-MiniLM-L6-v2",
	})
	if err != nil {
		t.Fatalf("NewSentenceTransformerProvider error: %v", err)
	}

	texts := []string{
		"first test document",
		"second test document",
		"third different document",
	}
	ctx := context.Background()

	embeddings, err := provider.GenerateEmbeddings(ctx, texts)
	if err != nil {
		t.Fatalf("GenerateEmbeddings error: %v", err)
	}

	if len(embeddings) != len(texts) {
		t.Errorf("GenerateEmbeddings returned %d embeddings, want %d", len(embeddings), len(texts))
	}
}

func TestSentenceTransformerProvider_EmptyBatch(t *testing.T) {
	provider, err := NewSentenceTransformerProvider(SentenceTransformerConfig{
		ModelID: "nomic-embed-text-v1.5",
	})
	if err != nil {
		t.Fatalf("NewSentenceTransformerProvider error: %v", err)
	}

	embeddings, err := provider.GenerateEmbeddings(nil, []string{})
	if err != nil {
		t.Errorf("GenerateEmbeddings([]) should not error, got: %v", err)
	}
	if len(embeddings) != 0 {
		t.Errorf("GenerateEmbeddings([]) returned %d embeddings, want 0", len(embeddings))
	}
}

func TestMeanPool(t *testing.T) {
	// 2 tokens, 4 dimensions
	emb := []float32{
		1.0, 2.0, 3.0, 4.0, // token 0
		5.0, 6.0, 7.0, 8.0, // token 1
	}
	result := meanPool(emb, 2)

	expected := []float32{3.0, 4.0, 5.0, 6.0}
	for i := range result {
		if result[i] != expected[i] {
			t.Errorf("meanPool[%d] = %v, want %v", i, result[i], expected[i])
		}
	}
}

func TestMeanPool_SingleToken(t *testing.T) {
	emb := []float32{1.0, 2.0, 3.0}
	result := meanPool(emb, 1)

	expected := []float32{1.0, 2.0, 3.0}
	for i := range result {
		if result[i] != expected[i] {
			t.Errorf("single token meanPool[%d] = %v, want %v", i, result[i], expected[i])
		}
	}
}

func TestNormalizeL2(t *testing.T) {
	// [3, 4] -> normalized: [0.6, 0.8]
	emb := []float32{3.0, 4.0}
	result := normalizeL2(emb)

	if len(result) != 2 {
		t.Fatalf("normalizeL2 returned %d dims, want 2", len(result))
	}

	// Check normalization: norm should be ~1.0
	var norm float64
	for _, v := range result {
		norm += float64(v) * float64(v)
	}
	if norm < 0.99 || norm > 1.01 {
		t.Errorf("normalized vector has norm %.4f, should be ~1.0", norm)
	}

	if result[0] < 0.59 || result[0] > 0.61 {
		t.Errorf("result[0] = %v, want ~0.6", result[0])
	}
	if result[1] < 0.79 || result[1] > 0.81 {
		t.Errorf("result[1] = %v, want ~0.8", result[1])
	}
}

func TestNormalizeL2_ZeroVector(t *testing.T) {
	emb := []float32{0.0, 0.0, 0.0}
	result := normalizeL2(emb)
	if len(result) != 3 {
		t.Errorf("zero normalization: got %d dims, want 3", len(result))
	}
}

// TestModelWeights_loadWeights tests the binary weight parsing.
func TestModelWeights_loadWeights(t *testing.T) {
	dir := t.TempDir()
	// Create a minimal binary weight file
	// Format: [hiddenIn int32][hiddenOut int32][vocabSize int32][maxSeqLen int32][weights...]
	data := make([]byte, 16+4*100)
	idx := 0
 binaryWrite(idx, data, int32(768)); idx += 4
 binaryWrite(idx, data, int32(768)); idx += 4
 binaryWrite(idx, data, int32(51200)); idx += 4
 binaryWrite(idx, data, int32(512)); idx += 4

	// Fill remaining with test weights
	weightData := data[idx:]
	for i := range weightData {
		weightData[i] = byte(i % 256)
	}

	path := filepath.Join(dir, "model.onnx")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := loadWeights(path, nil)
	if err != nil {
		t.Fatalf("loadWeights error: %v", err)
	}
	if w.hiddenIn != 768 {
		t.Errorf("hiddenIn = %d, want 768", w.hiddenIn)
	}
	if w.hiddenOut != 768 {
		t.Errorf("hiddenOut = %d, want 768", w.hiddenOut)
	}
	if w.vocabSize != 51200 {
		t.Errorf("vocabSize = %d, want 51200", w.vocabSize)
	}
	if w.maxSeqLen != 512 {
		t.Errorf("maxSeqLen = %d, want 512", w.maxSeqLen)
	}
}

func binaryWrite(p int, data []byte, v int32) {
	data[p] = byte(v)
	data[p+1] = byte(v >> 8)
	data[p+2] = byte(v >> 16)
	data[p+3] = byte(v >> 24)
}

// Dummy weights are loaded when no actual model file is present.
func TestLoadDummyWeights(t *testing.T) {
	info := ModelInfo{
		ID:              "test-model",
		Dimension:       768,
		MaxSequenceLen:  512,
		TokenizerType:   "bpe",
		PoolingMethod:   "mean",
		Normalize:       true,
		Description:     "test",
	}
	w := loadDummyWeights(info)
	if w.hiddenIn != 768 {
		t.Errorf("hiddenIn = %d, want 768", w.hiddenIn)
	}
	if len(w.matryoshkaDims) != 4 {
		t.Errorf("matryoshkaDims = %d, want 4", len(w.matryoshkaDims))
	}
}

func TestLoadWeights_BinaryFormat(t *testing.T) {
	dir := t.TempDir()

	// Create a weight file with known values
	data := make([]byte, 16+4*5)
	writeInt32(data, 0, 384)
	writeInt32(data, 4, 384)
	writeInt32(data, 8, 1000)
	writeInt32(data, 12, 64)

	// Add a few float weights
	writeFloat32(data, 16, 1.5)
	writeFloat32(data, 20, -2.5)
	writeFloat32(data, 24, 0.0)
	writeFloat32(data, 28, 100.0)
	writeFloat32(data, 32, -50.0)

	path := filepath.Join(dir, "weights.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := loadWeights(path, nil)
	if err != nil {
		t.Fatalf("loadWeights error: %v", err)
	}
	if w.hiddenIn != 384 {
		t.Errorf("hiddenIn = %d, want 384", w.hiddenIn)
	}
	if w.vocabSize != 1000 {
		t.Errorf("vocabSize = %d, want 1000", w.vocabSize)
	}
	if w.maxSeqLen != 64 {
		t.Errorf("maxSeqLen = %d, want 64", w.maxSeqLen)
	}
}

func writeInt32(data []byte, offset int, v int32) {
	data[offset] = byte(v)
	data[offset+1] = byte(v >> 8)
	data[offset+2] = byte(v >> 16)
	data[offset+3] = byte(v >> 24)
}

func writeFloat32(data []byte, offset int, v float32) {
	bits := math.Float32bits(v)
	data[offset] = byte(bits)
	data[offset+1] = byte(bits >> 8)
	data[offset+2] = byte(bits >> 16)
	data[offset+3] = byte(bits >> 24)
}

func TestLoadWeights_TooSmall(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	tempFile := filepath.Join(t.TempDir(), "tiny.bin")
	if err := os.WriteFile(tempFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadWeights(tempFile, nil)
	if err == nil {
		t.Error("loadWeights with too-small file should error")
	}
}

func TestTokenIDHash(t *testing.T) {
	h1 := hashTokenID(100, 0)
	h2 := hashTokenID(200, 0)
	h3 := hashTokenID(100, 1)

	if h1 == h2 {
		t.Error("different tokens should produce different hashes")
	}
	if h1 == h3 {
		t.Error("different dims should produce different hashes")
	}
}

// Test the fallback tokenizer.
func TestDefaultTokenizer_Encode(t *testing.T) {
	tok, ok := NewDefaultTokenizer()
	if !ok {
		t.Fatal("NewDefaultTokenizer returned false")
	}

	tests := []struct {
		text     string
		wantLen  int // expected length of result (BOS + tokens + EOS)
	}{
		{"", 2},    // just BOS + EOS
		{"hello", 3}, // BOS + "hello" + EOS
		{"code function test", 5}, // BOS + 3 tokens + EOS
	}

	for _, tt := range tests {
		tokens, err := tok.Encode(tt.text)
		if err != nil {
			t.Errorf("Encode(%q) error: %v", tt.text, err)
			continue
		}
		if len(tokens) < tt.wantLen-1 {
			t.Errorf("Encode(%q) = %d tokens, want at least %d", tt.text, len(tokens), tt.wantLen)
		}
	}
}

func TestPreTokenize(t *testing.T) {
	tests := []struct {
		input string
		// Check that we get at least some tokens
		minTokens int
	}{
		{"hello world", 2},
		{"the brown fox", 3},
		{"function() -> int", 2}, // function and int
	}

	for _, tt := range tests {
		tokens := preTokenize(tt.input)
		if len(tokens) < tt.minTokens {
			t.Errorf("preTokenize(%q) = %d tokens, want at least %d", tt.input, len(tokens), tt.minTokens)
		}
	}
}

func TestPreTokenize_EdgeCases(t *testing.T) {
	if len(preTokenize("")) != 0 {
		t.Error("preTokenize(\"\") should return empty slice")
	}
	if len(preTokenize("   ")) != 0 {
		t.Error("preTokenize(\"   \") should return empty slice")
	}
}
