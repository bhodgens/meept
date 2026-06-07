package vector

import (
	"testing"
)

func TestGetModelInfo(t *testing.T) {
	tests := []struct {
		name       string
		modelID    string
		wantOK     bool
		wantDim    int
		wantMaxSeq int
	}{
		{
			name:       "nomic-embed-text-v1.5",
			modelID:    "nomic-embed-text-v1.5",
			wantOK:     true,
			wantDim:    768,
			wantMaxSeq: 8192,
		},
		{
			name:       "all-MiniLM-L6-v2",
			modelID:    "all-MiniLM-L6-v2",
			wantOK:     true,
			wantDim:    384,
			wantMaxSeq: 512,
		},
		{
			name:       "all-mpnet-base-v2",
			modelID:    "all-mpnet-base-v2",
			wantOK:     true,
			wantDim:    768,
			wantMaxSeq: 512,
		},
		{
			name:       "paraphrase-multilingual-mpnet-base-v2",
			modelID:    "paraphrase-multilingual-mpnet-base-v2",
			wantOK:     true,
			wantDim:    768,
			wantMaxSeq: 512,
		},
		{
			name:   "unknown model",
			modelID: "unknown/foobar",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := GetModelInfo(tt.modelID)
			if ok != tt.wantOK {
				t.Errorf("GetModelInfo(%q) ok = %v, want %v", tt.modelID, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if info.Dimension != tt.wantDim {
				t.Errorf("GetModelInfo(%q).Dimension = %d, want %d", tt.modelID, info.Dimension, tt.wantDim)
			}
			if info.MaxSequenceLen != tt.wantMaxSeq {
				t.Errorf("GetModelInfo(%q).MaxSequenceLen = %d, want %d", tt.modelID, info.MaxSequenceLen, tt.wantMaxSeq)
			}
		})
	}
}

func TestListModels(t *testing.T) {
	models := ListModels()
	if len(models) != len(knownModels) {
		t.Errorf("ListModels() returned %d models, want %d", len(models), len(knownModels))
	}

	// Verify all known models are listed
	for id := range knownModels {
		found := false
		for _, m := range models {
			if m == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("model %q from knownModels not found in ListModels()", id)
		}
	}
}

func TestRegisterModel(t *testing.T) {
	// Save original state to restore after test
	origSize := len(knownModels)

	RegisterModel(ModelInfo{
		ID:              "test/custom-model",
		Dimension:       256,
		MaxSequenceLen:  512,
		ONNXModelPath:   "model.onnx",
		TokenizerPath:   "tokenizer.json",
		TokenizerType:   "bpe",
		PoolingMethod:   "mean",
		Normalize:       true,
		Tags:            []string{"custom", "test"},
		Description:     "Test custom model",
	})

	info, ok := GetModelInfo("test/custom-model")
	if !ok {
		t.Fatal("registered model not found")
	}
	if info.Dimension != 256 {
		t.Errorf("Dimension = %d, want 256", info.Dimension)
	}

	// Clean up
	delete(knownModels, "test/custom-model")
	if len(knownModels) != origSize {
		t.Errorf("Cleanup failed: knownModels has %d entries, want %d", len(knownModels), origSize)
	}
}

func TestHasModel(t *testing.T) {
	if !HasModel("nomic-embed-text-v1.5") {
		t.Error("HasModel(\"nomic-embed-text-v1.5\") = false, want true")
	}
	if HasModel("nonexistent/model") {
		t.Error("HasModel(\"nonexistent/model\") = true, want false")
	}
}

func TestModelInfo_SupportsMatryoshka(t *testing.T) {
	testCases := []struct {
		name      string
		modelID   string
		wantBool  bool
	}{
		{"nomic matryoshka", "nomic-embed-text-v1.5", true},
		{"minilm fast", "all-MiniLM-L6-v2", false},
		{"mpnet high quality", "all-mpnet-base-v2", false},
		{"multilingual", "paraphrase-multilingual-mpnet-base-v2", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := GetModelInfo(tc.modelID)
			if !ok {
				t.Fatalf("model %q not found", tc.modelID)
			}
			got := info.SupportsMatryoshka()
			if got != tc.wantBool {
				t.Errorf("SupportsMatryoshka() = %v, want %v", got, tc.wantBool)
			}
		})
	}
}

func TestModelInfo_ValidDimension(t *testing.T) {
	info, _ := GetModelInfo("nomic-embed-text-v1.5")

	tests := []struct {
		dim     int
		wantDim int
	}{
		// Valid dimensions pass through
		{768, 768},
		{512, 512},
		{256, 256},
		{128, 128},
		// Invalid dimensions get clamped to closest
		{0, 128},
		{100, 128},
		{384, 512},
		{640, 512},
		{1000, 768},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := info.ValidDimension(tt.dim)
			if got != tt.wantDim {
				t.Errorf("ValidDimension(%d) = %d, want %d", tt.dim, got, tt.wantDim)
			}
		})
	}

	// Non-matryoshka models always return native dimension
	info2, _ := GetModelInfo("all-MiniLM-L6-v2")
	if got := info2.ValidDimension(256); got != 384 {
		t.Errorf("non-matryoshka ValidDimension(256) = %d, want 384", got)
	}
}

func TestModelInfo_Description(t *testing.T) {
	info, ok := GetModelInfo("nomic-embed-text-v1.5")
	if !ok {
		t.Fatal("model not found")
	}
	if info.Description == "" {
		t.Error("Description is empty")
	}
	if info.ONNXModelPath == "" {
		t.Error("ONNXModelPath is empty")
	}
	if info.TokenizerPath == "" {
		t.Error("TokenizerPath is empty")
	}
}
