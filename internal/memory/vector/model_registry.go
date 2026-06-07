package vector

// ModelInfo holds metadata about a known sentence-transformer model.
type ModelInfo struct {
	ID              string
	Dimension       int
	MaxSequenceLen  int
	ONNXModelPath   string // Path inside the model directory
	TokenizerPath   string // Path inside the model directory
	TokenizerType   string // "bert" | "sentencepiece" | "bpe"
	PoolingMethod   string // "mean" | "cls"
	Normalize       bool
	Tags            []string
	Description     string
}

// knownModels is the registry of supported sentence-transformer models.
var knownModels = map[string]ModelInfo{
	"nomic-embed-text-v1.5": {
		ID:              "nomic-embed-text-v1.5",
		Dimension:       768,
		MaxSequenceLen:  8192,
		ONNXModelPath:   "onnx/model.onnx",
		TokenizerPath:   "tokenizer.json",
		TokenizerType:   "bpe",
		PoolingMethod:   "mean",
		Normalize:       true,
		Tags:            []string{"nomic", "matryoshka", "text-embedding"},
		Description:     "Nomic embed text v1.5 -- high-quality English text embeddings with Matryoshka dimension support (768/512/256/128)",
	},
	"all-MiniLM-L6-v2": {
		ID:              "all-MiniLM-L6-v2",
		Dimension:       384,
		MaxSequenceLen:  512,
		ONNXModelPath:   "onnx/model.onnx",
		TokenizerPath:   "tokenizer.json",
		TokenizerType:   "bpe",
		PoolingMethod:   "mean",
		Normalize:       true,
		Tags:            []string{"sentence-transformers", "fast", "text-embedding"},
		Description:     "All-MiniLM-L6-v2 -- fast, compact embeddings suitable for semantic search (384-dim)",
	},
	"all-mpnet-base-v2": {
		ID:              "all-mpnet-base-v2",
		Dimension:       768,
		MaxSequenceLen:  512,
		ONNXModelPath:   "onnx/model.onnx",
		TokenizerPath:   "tokenizer.json",
		TokenizerType:   "bpe",
		PoolingMethod:   "mean",
		Normalize:       true,
		Tags:            []string{"sentence-transformers", "high-quality", "text-embedding"},
		Description:     "all-mpnet-base-v2 -- high-quality English embeddings from Sentence-Transformers",
	},
	"paraphrase-multilingual-mpnet-base-v2": {
		ID:              "paraphrase-multilingual-mpnet-base-v2",
		Dimension:       768,
		MaxSequenceLen:  512,
		ONNXModelPath:   "onnx/model.onnx",
		TokenizerPath:   "tokenizer.json",
		TokenizerType:   "bpe",
		PoolingMethod:   "mean",
		Normalize:       true,
		Tags:            []string{"sentence-transformers", "multilingual", "text-embedding"},
		Description:     "paraphrase-multilingual-mpnet-base-v2 -- supports 50+ languages for cross-lingual search",
	},
}

// GetModelInfo returns metadata for a known model by ID.
// Returns (ModelInfo, true) if the model is known, (zero value, false) otherwise.
func GetModelInfo(modelID string) (ModelInfo, bool) {
	info, ok := knownModels[modelID]
	return info, ok
}

// ListModels returns all known model IDs.
func ListModels() []string {
	ids := make([]string, 0, len(knownModels))
	for id := range knownModels {
		ids = append(ids, id)
	}
	return ids
}

// RegisterModel registers a new custom model in the registry.
func RegisterModel(info ModelInfo) {
	knownModels[info.ID] = info
}

// HasModel returns true if modelID is registered.
func HasModel(modelID string) bool {
	_, ok := knownModels[modelID]
	return ok
}

// SupportsMatryoshka returns true if the model supports variable-dimensional embeddings.
func (mi ModelInfo) SupportsMatryoshka() bool {
	for _, tag := range mi.Tags {
		if tag == "matryoshka" {
			return true
		}
	}
	return false
}

// ValidDimension checks if the given dimension is in the supported Matryoshka subspace.
// Returns the clamped dimension if not.
func (mi ModelInfo) ValidDimension(dim int) int {
	if !mi.SupportsMatryoshka() {
		return mi.Dimension
	}
	supportedDims := []int{768, 512, 256, 128}
	for _, s := range supportedDims {
		if dim == s {
			return s
		}
	}
	// Return closest supported dimension
	best := mi.Dimension
	bestDist := abs(mi.Dimension - dim)
	for _, s := range supportedDims {
		d := abs(s - dim)
		if d < bestDist {
			best = s
			bestDist = d
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
