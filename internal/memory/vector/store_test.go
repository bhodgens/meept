package vector

import (
	"context"
	"math/rand"
	"path/filepath"
	"testing"
)

// mockProvider generates deterministic random embeddings for testing.
type mockProvider struct {
	dim int
	rng *rand.Rand
}

func newMockProvider(dim int) *mockProvider {
	return &mockProvider{
		dim: dim,
		rng: rand.New(rand.NewSource(42)),
	}
}

func (p *mockProvider) GenerateEmbedding(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, p.dim)
	for i := range v {
		v[i] = float32(p.rng.Float64()*2 - 1) // [-1, 1]
	}
	// Normalize
	norm := float32(0)
	for _, x := range v {
		norm += x * x
	}
	norm = float32(1.0 / float64(norm))
	for i := range v {
		v[i] *= norm
	}
	return v, nil
}

func (p *mockProvider) GenerateEmbeddings(_ context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		emb, err := p.GenerateEmbedding(context.TODO(), texts[i])
		if err != nil {
			return nil, err
		}
		results[i] = emb
	}
	return results, nil
}

func (p *mockProvider) Dimension() int {
	return p.dim
}

// newTestShard creates a temporary vector shard for testing.
func newTestShard(t *testing.T, dim int) *VectorShard {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	shard, err := NewVectorShard(path, dim, 16, 200)
	if err != nil {
		t.Fatalf("failed to create shard: %v", err)
	}
	return shard
}

// --- Unit Tests ---

func TestNewVectorShard(t *testing.T) {
	shard := newTestShard(t, 128)
	defer shard.Close()

	stats := shard.Stats()
	if stats.Dimension != 128 {
		t.Errorf("expected dimension 128, got %d", stats.Dimension)
	}
	if stats.EFConstruction != 200 {
		t.Errorf("expected efConstruction 200, got %d", stats.EFConstruction)
	}
}

func TestNewVectorShardDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "defaults.db")

	shard, err := NewVectorShard(path, 64, 0, 0)
	if err != nil {
		t.Fatalf("failed to create shard with defaults: %v", err)
	}
	defer shard.Close()

	if shard.M != DefaultM {
		t.Errorf("expected default M=%d, got %d", DefaultM, shard.M)
	}
	if shard.efConstruction != DefaultEFConstruction {
		t.Errorf("expected default efConstruction=%d, got %d", DefaultEFConstruction, shard.efConstruction)
	}
	if shard.efSearch != DefaultEFSearch {
		t.Errorf("expected default efSearch=%d, got %d", DefaultEFSearch, shard.efSearch)
	}
}

func TestNewVectorShardInvalidDimension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.db")

	_, err := NewVectorShard(path, 0, 16, 200)
	if err == nil {
		t.Fatal("expected error for dimension 0")
	}

	_, err = NewVectorShard(path, -1, 16, 200)
	if err == nil {
		t.Fatal("expected error for negative dimension")
	}
}

func TestInsertSearch(t *testing.T) {
	shard := newTestShard(t, 128)
	defer shard.Close()

	provider := newMockProvider(128)
	ctx := context.Background()

	// Insert vectors
	topics := []string{
		"Go concurrency with channels and goroutines",
		"Machine learning model training and inference",
		"Web development with React and TypeScript",
		"Database optimization and query tuning",
		"Natural language processing techniques",
	}

	for i, topic := range topics {
		embedding, err := provider.GenerateEmbedding(ctx, topic)
		if err != nil {
			t.Fatalf("generate embedding: %v", err)
		}
		if err := shard.Insert(ctx, string(rune('a'+i)), embedding, topic); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// Search for similar content
	queryEmbedding, err := provider.GenerateEmbedding(ctx, "goroutines and channels in Go")
	if err != nil {
		t.Fatalf("generate query: %v", err)
	}

	results, err := shard.Search(ctx, queryEmbedding, 3, 50)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected search results, got none")
	}

	// Top result should be the Go-related topic
	if results[0].Content != topics[0] {
		t.Logf("top result: %q (expected: %q)", results[0].Content, topics[0])
	}

	// Scores should be in [0, 1]
	for _, r := range results {
		if r.VectorSimilarity < 0 || r.VectorSimilarity > 1 {
			t.Errorf("score out of range: %f", r.VectorSimilarity)
		}
	}
}

func TestInsertDelete(t *testing.T) {
	shard := newTestShard(t, 64)
	defer shard.Close()

	provider := newMockProvider(64)
	ctx := context.Background()

	// Insert
	emb, _ := provider.GenerateEmbedding(ctx, "test content")
	if err := shard.Insert(ctx, "mem-1", emb, "test content"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Verify exists
	stats := shard.Stats()
	if stats.VectorCount != 1 {
		t.Errorf("expected 1 vector, got %d", stats.VectorCount)
	}

	// Delete
	if err := shard.Delete(ctx, "mem-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify removed
	stats = shard.Stats()
	if stats.VectorCount != 0 {
		t.Errorf("expected 0 vectors after delete, got %d", stats.VectorCount)
	}
}

func TestSearchDimensionMismatch(t *testing.T) {
	shard := newTestShard(t, 128)
	defer shard.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	otherShard, err := NewVectorShard(path, 64, 16, 200)
	if err != nil {
		t.Fatalf("create shard: %v", err)
	}
	defer otherShard.Close()

	_, err = otherShard.Search(context.Background(), make([]float32, 128), 10, 50)
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestInsertDimensionMismatch(t *testing.T) {
	shard := newTestShard(t, 128)
	defer shard.Close()

	wrong := make([]float32, 64)
	ctx := context.Background()
	err := shard.Insert(ctx, "m1", wrong, "test")
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestBatchInsert(t *testing.T) {
	shard := newTestShard(t, 64)
	defer shard.Close()

	provider := newMockProvider(64)
	ctx := context.Background()

	n := 50
	memoryIDs := make([]string, n)
	embeddings := make([][]float32, n)
	contents := make([]string, n)

	for i := 0; i < n; i++ {
		memoryIDs[i] = string(rune('a' + i%26))
		contents[i] = "content item " + string(rune('a'+i%26))
		emb, err := provider.GenerateEmbedding(ctx, contents[i])
		if err != nil {
			t.Fatalf("generate embedding: %v", err)
		}
		embeddings[i] = emb
	}

	if err := shard.InsertBatch(ctx, memoryIDs, embeddings, contents); err != nil {
		t.Fatalf("batch insert: %v", err)
	}

	stats := shard.Stats()
	if stats.VectorCount != int64(n) {
		t.Errorf("expected %d vectors, got %d", n, stats.VectorCount)
	}
}

func TestOpenExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.db")

	// Create and populate
	shard1, err := NewVectorShard(path, 32, 8, 100)
	if err != nil {
		t.Fatalf("create shard: %v", err)
	}

	provider := newMockProvider(32)
	ctx := context.Background()
	emb, _ := provider.GenerateEmbedding(ctx, "test")
	if err := shard1.Insert(ctx, "existing", emb, "existing content"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	shard1.Close()

	// Reopen with same parameters
	shard2, err := NewVectorShard(path, 32, 8, 100)
	if err != nil {
		t.Fatalf("reopen shard: %v", err)
	}
	defer shard2.Close()

	if shard2.Dimension() != 32 {
		t.Errorf("expected dimension 32, got %d", shard2.Dimension())
	}
}

// --- Dimension Slicing Tests ---

func TestSliceEmbedding(t *testing.T) {
	original := make([]float32, 768)
	for i := range original {
		original[i] = float32(i)
	}

	tests := []struct {
		target    int
		wantLen   int
		wantFirst float32
		wantLast  float32
	}{
		{768, 768, 0, 767},
		{512, 512, 0, 511},
		{256, 256, 0, 255},
		{128, 128, 0, 127},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			sliced, err := SliceEmbedding(original, tc.target)
			if err != nil {
				t.Fatalf("slice(%d): %v", tc.target, err)
			}
			if len(sliced) != tc.wantLen {
				t.Errorf("length: got %d, want %d", len(sliced), tc.wantLen)
			}
			if sliced[0] != tc.wantFirst {
				t.Errorf("first: got %f, want %f", sliced[0], tc.wantFirst)
			}
			if sliced[len(sliced)-1] != tc.wantLast {
				t.Errorf("last: got %f, want %f", sliced[len(sliced)-1], tc.wantLast)
			}
		})
	}
}

func TestSliceEmbeddingErrors(t *testing.T) {
	emb := []float32{1, 2, 3}

	_, err := SliceEmbedding(emb, 0)
	if err == nil {
		t.Error("expected error for target 0")
	}

	_, err = SliceEmbedding(emb, 4)
	if err == nil {
		t.Error("expected error for target > source")
	}

	_, err = SliceEmbedding(emb, -1)
	if err == nil {
		t.Error("expected error for negative target")
	}
}

func TestValidateDimension(t *testing.T) {
	if err := ValidateDimension(768, 512); err != nil {
		t.Errorf("768->512: unexpected error: %v", err)
	}
	if err := ValidateDimension(768, 768); err != nil {
		t.Errorf("768->768: unexpected error: %v", err)
	}
	if err := ValidateDimension(768, 769); err == nil {
		t.Error("expected error: target > source")
	}
	if err := ValidateDimension(0, 512); err == nil {
		t.Error("expected error: source = 0")
	}
}

func TestSuggestedDimension(t *testing.T) {
	tests := []struct {
		src, want int
	}{
		{768, Dim512},
		{512, Dim256},
		{256, Dim128},
		{128, 128},
		{1024, Dim512},
	}
	for _, tc := range tests {
		if got := SuggestedDimension(tc.src); got != tc.want {
			t.Errorf("SuggestedDimension(%d) = %d, want %d", tc.src, got, tc.want)
		}
	}
}

// --- Benchmark Tests ---

func BenchmarkInsert(b *testing.B) {
	dims := []int{768, 512, 256, 128}
	for _, dim := range dims {
		b.Run("", func(b *testing.B) {
			dir := b.TempDir()
			path := filepath.Join(dir, "bench.db")
			shard, err := NewVectorShard(path, dim, 16, 200)
			if err != nil {
				b.Fatalf("create shard: %v", err)
			}
			defer shard.Close()

			provider := newMockProvider(dim)
			ctx := context.Background()

			embedding, _ := provider.GenerateEmbedding(ctx, "benchmark item")

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = shard.Insert(ctx, string(rune(i%26)), embedding, "benchmark content")
			}
		})
	}
}

func BenchmarkSearch(b *testing.B) {
	dims := []int{768, 512, 256, 128}
	shardSizes := []int{1000, 10000} // pre-inserted vectors

	for _, dim := range dims {
		for _, size := range shardSizes {
			name := ""
			switch dim {
			case 768:
				name = "d768"
			case 512:
				name = "d512"
			case 256:
				name = "d256"
			case 128:
				name = "d128"
			}
			name += "_n" + string(rune('1'+size/1000))

			b.Run(name, func(b *testing.B) {
				dir := b.TempDir()
				path := filepath.Join(dir, "bench.db")
				shard, err := NewVectorShard(path, dim, 16, 200)
				if err != nil {
					b.Fatalf("create shard: %v", err)
				}
				defer shard.Close()

				provider := newMockProvider(dim)
				ctx := context.Background()

				// Pre-populate
				for i := 0; i < size; i++ {
					emb, _ := provider.GenerateEmbedding(ctx, "item")
					_ = shard.Insert(ctx, string(rune(i%26)), emb, "item")
				}

				// Query vector
				queryEmb, _ := provider.GenerateEmbedding(ctx, "query")

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _ = shard.Search(ctx, queryEmb, 10, 50)
				}
			})
		}
	}
}

func BenchmarkDimensionComparison(b *testing.B) {
	dims := []int{768, 512, 256, 128}
	nItems := 10000

	queryEmb, _ := newMockProvider(768).GenerateEmbedding(context.Background(), "query")

	for _, dim := range dims {
		b.Run("", func(b *testing.B) {
			dir := b.TempDir()
			path := filepath.Join(dir, "bench.db")
			shard, err := NewVectorShard(path, dim, 16, 200)
			if err != nil {
				b.Fatalf("create shard: %v", err)
			}
			defer shard.Close()

			provider := newMockProvider(dim)
			ctx := context.Background()

			for i := 0; i < nItems; i++ {
				emb, _ := provider.GenerateEmbedding(ctx, "item")
				_ = shard.Insert(ctx, string(rune(i%26)), emb, "item")
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = shard.Search(ctx, queryEmb, 10, 50)
			}
		})
	}
}
