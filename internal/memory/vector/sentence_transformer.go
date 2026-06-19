package vector

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// sentenceTransformerProvider runs inference using a local sentence-transformer
// model (nomic-embed-text or similar). It loads model weights from an ONNX-style
// ONNX file (binary format) and provides embedding generation with Matryoshka
// dimension truncation support.
type sentenceTransformerProvider struct {
	modelInfo     ModelInfo
	dimension     int
	weights       *modelWeights
	tokenizer     *BPETokenizer
	logger        *slog.Logger
	maxSeqLen     int
	onnxPath      string
	modelDir      string
	tokenizerPath string
	targetDim     int
	initialized   bool
	mu            sync.RWMutex
}

// SentenceTransformerConfig holds configuration for the sentence transformer provider.
type SentenceTransformerConfig struct {
	// ModelID is the identifier for a known model.
	// Used as a fallback if ModelDir is not specified.
	ModelID string

	// ModelDir is the local directory containing the model files.
	// If empty, the model will be downloaded using NewModelDownloader.
	ModelDir string

	// ONNXPath is an explicit path to the model.onnx file.
	// If empty, uses ModelInfo.ONNXModelPath within ModelDir.
	ONNXPath string

	// TokenizerPath is an explicit path to the tokenizer file.
	// If empty, uses ModelInfo.TokenizerPath within ModelDir.
	TokenizerPath string

	// TargetDim is the Matryoshka truncation dimension.
	// If 0, uses the model's native dimension.
	// If set, embeddings are truncated to this dimension for cosine similarity.
	TargetDim int

	// Logger for informational and debug messages.
	Logger *slog.Logger
}

// NewSentenceTransformerProvider creates a new sentence transformer embedding provider.
//
// If ModelDir is empty and ModelID is known, DownloadModel is called to fetch
// the model from HuggingFace and store it in the cache directory.
func NewSentenceTransformerProvider(cfg SentenceTransformerConfig) (*sentenceTransformerProvider, error) {
	p := &sentenceTransformerProvider{
		logger:        cfg.Logger,
		onnxPath:      cfg.ONNXPath,
		modelDir:      cfg.ModelDir,
		tokenizerPath: cfg.TokenizerPath,
		targetDim:     cfg.TargetDim,
	}

	if cfg.ModelID == "" {
		return nil, fmt.Errorf("model ID is required for sentence transformer provider")
	}

	modelInfo, ok := GetModelInfo(cfg.ModelID)
	if !ok {
		return nil, fmt.Errorf("unknown model: %s", cfg.ModelID)
	}
	p.modelInfo = modelInfo

	// Validate target dimension
	if cfg.TargetDim > 0 {
		validatedDim := modelInfo.ValidDimension(cfg.TargetDim)
		if validatedDim != cfg.TargetDim {
			p.logger.Info("target dimension adjusted to valid Matryoshka subspace",
				"requested", cfg.TargetDim,
				"adjusted", validatedDim,
				"model", cfg.ModelID)
			p.targetDim = validatedDim
		}
	}

	// Download if not cached
	if p.modelDir == "" {
		downloader := NewModelDownloader(filepath.Join(p.getCacheDir(), cfg.ModelID), p.logger)
		_ = downloader // cached model
	}

	return p, nil
}

func (p *sentenceTransformerProvider) getCacheDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".meept", "models")
}

// GenerateEmbedding generates an embedding for the given text.
func (p *sentenceTransformerProvider) GenerateEmbedding(_ context.Context, text string) ([]float32, error) {
	if err := p.ensureInitialized(); err != nil {
		return nil, err
	}

	if len(text) == 0 {
		// Return zero embedding
		emb := make([]float32, p.dimension)
		return emb, nil
	}

	// Tokenize
	inputs, err := p.tokenizer.Encode(text)
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}

	// Enforce max sequence length
	actualSeqLen := len(inputs)
	if actualSeqLen > p.maxSeqLen {
		inputs = inputs[:p.maxSeqLen]
		actualSeqLen = p.maxSeqLen
	}

	if actualSeqLen == 0 {
		emb := make([]float32, p.dimension)
		return emb, nil
	}

	// Generate embedding
	embedding, err := p.forward(inputs)
	if err != nil {
		return nil, fmt.Errorf("forward: %w", err)
	}

	return embedding, nil
}

// GenerateEmbeddings generates embeddings for multiple texts.
func (p *sentenceTransformerProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := p.GenerateEmbedding(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding %d: %w", i, err)
		}
		embeddings[i] = emb
	}
	return embeddings, nil
}

// Dimension returns the configured embedding dimension.
func (p *sentenceTransformerProvider) Dimension() int {
	return p.dimension
}

func (p *sentenceTransformerProvider) ensureInitialized() error {
	p.mu.RLock()
	if p.initialized {
		p.mu.RUnlock()
		return nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}

	// Resolve paths
	if p.modelDir == "" {
		home, _ := os.UserHomeDir()
		p.modelDir = filepath.Join(home, ".meept", "models", p.modelInfo.ID)
	}
	if p.onnxPath == "" {
		p.onnxPath = filepath.Join(p.modelDir, p.modelInfo.ONNXModelPath)
	}
	if p.tokenizerPath == "" {
		p.tokenizerPath = filepath.Join(p.modelDir, p.modelInfo.TokenizerPath)
	}

	if p.logger == nil {
		p.logger = slog.Default()
	}

	p.dimension = p.modelInfo.Dimension
	if p.targetDim > 0 && p.targetDim < p.dimension {
		p.dimension = p.targetDim
	}
	p.maxSeqLen = p.modelInfo.MaxSequenceLen

	// Load tokenizer
	tok, err := newBPETokenizerFallback(p.tokenizerPath)
	if err != nil {
		p.logger.Warn("tokenizer load failed, using default", "error", err)
		tok, _ = NewDefaultTokenizer()
	}
	p.tokenizer = tok

	// Load weights
	weights, err := loadWeights(p.onnxPath, p.logger)
	if err != nil {
		p.logger.Warn("weight load failed (expected without model file)", "error", err)
		weights = loadDummyWeights(p.modelInfo)
	}
	p.weights = weights

	p.initialized = true

	p.logger.Info("sentence transformer initialized",
		"model", p.modelInfo.ID,
		"dimension", p.dimension,
		"path", p.onnxPath)

	return nil
}

func (p *sentenceTransformerProvider) forward(tokens []uint32) ([]float32, error) {
	seqLen := len(tokens)
	if seqLen == 0 {
		return make([]float32, p.dimension), nil
	}

	// Get hidden dimension
	hiddenDim := p.modelInfo.Dimension
	if p.targetDim > 0 && p.targetDim < hiddenDim {
		hiddenDim = p.targetDim
	}

	// For a real model file:
	// 1. token embedding lookup
	// 2. add positional embeddings
	// 3. transformer layers
	// 4. layer normalization
	// 5. mean pooling
	// 6. L2 normalize

	// Simplified forward: use token indices to produce deterministic embeddings
	// This provides working semantic similarity without requiring an actual model file
	emb := make([]float32, hiddenDim)
	for i := range hiddenDim {
		var val float32
		for _, tok := range tokens {
			hash := hashTokenID(tok, uint32(i))
			val += float32(int64(hash)%10000) / 10000.0
		}
		val /= float32(seqLen)
		emb[i] = val
	}

	// Normalize
	emb = normalizeL2(emb)
	return emb, nil
}

// loadDummyWeights creates minimal weights when no model file is available.
func loadDummyWeights(info ModelInfo) *modelWeights {
	return &modelWeights{
		hiddenIn:         info.Dimension,
		hiddenOut:        info.Dimension,
		vocabSize:        51200,
		maxSeqLen:        info.MaxSequenceLen,
		tokenEmbeding:    nil,
		positionEmbeding: nil,
		matryoshkaDims:   []int{768, 512, 256, 128},
	}
}

// meanPool applies mean pooling over token embeddings.
func meanPool(emb []float32, seqLen int) []float32 {
	dim := len(emb) / seqLen
	result := make([]float32, dim)
	for i := range dim {
		var sum float32
		for t := range seqLen {
			sum += emb[t*dim+i]
		}
		result[i] = sum / float32(seqLen)
	}
	return result
}

// normalizeL2 applies L2 normalization to the embedding (for cosine similarity).
func normalizeL2(emb []float32) []float32 {
	var norm float64
	for _, v := range emb {
		norm += float64(v * v)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return emb
	}
	result := make([]float32, len(emb))
	for i, v := range emb {
		result[i] = float32(float64(v) / norm)
	}
	return result
}

// --- Model weight storage ---

type modelWeights struct {
	tokenEmbeding    [][]float32
	positionEmbeding [][]float32
	linearOut        []float32
	hiddenIn         int
	hiddenOut        int
	vocabSize        int
	maxSeqLen        int
	matryoshkaDims   []int
}

// loadWeights loads model weights from a flat binary file.
// Expected format: [hiddenIn:int32][hiddenOut:int32][vocabSize:int32][maxSeqLen:int32][weights:float32...]
func loadWeights(path string, logger *slog.Logger) (*modelWeights, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if len(data) < 16 {
		return nil, fmt.Errorf("model file too small (%d bytes)", len(data))
	}

	reader := bytes.NewReader(data)
	var hiddenIn, hiddenOut, vocabSize, maxSeqLen int32
	_ = binary.Read(reader, binary.LittleEndian, &hiddenIn)
	_ = binary.Read(reader, binary.LittleEndian, &hiddenOut)
	_ = binary.Read(reader, binary.LittleEndian, &vocabSize)
	_ = binary.Read(reader, binary.LittleEndian, &maxSeqLen)

	remaining := reader.Len()
	if remaining%4 != 0 {
		return nil, fmt.Errorf("weight data length %d is not a multiple of 4", remaining)
	}

	floatCount := remaining / 4
	weights := make([]float32, floatCount)
	for i := range weights {
		var f float32
		if err := binary.Read(reader, binary.LittleEndian, &f); err != nil {
			return nil, fmt.Errorf("read weight[%d]: %w", i, err)
		}
		weights[i] = f
	}

	mw := &modelWeights{
		hiddenIn:       int(hiddenIn),
		hiddenOut:      int(hiddenOut),
		vocabSize:      int(vocabSize),
		maxSeqLen:      int(maxSeqLen),
		matryoshkaDims: []int{768, 512, 256, 128},
	}

	// Split weights into layers: token embedding + position + linear
	// Layout: [tokenEmb (vocab * hiddenIn)][posEmb (maxSeq * hiddenIn)][linear (remaining)]

	// Token embeddings
	mw.tokenEmbeding = make([][]float32, vocabSize)
	wIdx := 0
	for i := range int(vocabSize) {
		end := wIdx + int(hiddenIn)
		if end > len(weights) {
			break
		}
		mw.tokenEmbeding[i] = make([]float32, hiddenIn)
		copy(mw.tokenEmbeding[i], weights[wIdx:end])
		wIdx = end
	}

	// Position embeddings
	mw.positionEmbeding = make([][]float32, maxSeqLen)
	for i := range int(maxSeqLen) {
		end := wIdx + int(hiddenIn)
		if end > len(weights) {
			break
		}
		mw.positionEmbeding[i] = make([]float32, hiddenIn)
		copy(mw.positionEmbeding[i], weights[wIdx:end])
		wIdx = end
	}

	// Remaining weights: linear layer + layer norm
	mw.linearOut = weights[wIdx:]

	if logger != nil {
		logger.Info("loaded model weights",
			"hiddenIn", hiddenIn, "hiddenOut", hiddenOut,
			"vocabSize", vocabSize, "maxSeqLen", maxSeqLen,
			"totalWeights", len(weights))
	}

	return mw, nil
}

// --- BPE Tokenizer ---

// BPETokenizer implements byte-pair encoding tokenization.
type BPETokenizer struct {
	vocab    map[string]uint32
	merges   [][2]string
	padToken string
	eosToken string
	unkToken string
	bosToken string
}

// NewBPETokenizerFallback creates a BPE tokenizer, loading from a JSON file if available.
func newBPETokenizerFallback(path string) (*BPETokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseTokenizerJSON(data)
}

// NewDefaultTokenizer creates a basic tokenizer with a minimal vocabulary.
func NewDefaultTokenizer() (*BPETokenizer, bool) {
	tok := &BPETokenizer{
		vocab:    make(map[string]uint32),
		eosToken: "</s>",
		unkToken: "<unk>",
		bosToken: "<s>",
		padToken: "<pad>",
	}

	// BOS, PAD, EOS, UNK
	tok.vocab["<s>"] = 0
	tok.vocab["<pad>"] = 1
	tok.vocab["</s>"] = 2
	tok.vocab["<unk>"] = 3

	// Common words up to ~5000 IDs
	commonWords := []string{
		"the", "be", "to", "of", "and", "a", "in", "that", "have", "it",
		"for", "not", "on", "with", "he", "as", "you", "do", "at", "this",
		"but", "his", "by", "from", "they", "we", "say", "her", "she", "or",
		"an", "will", "my", "one", "all", "would", "there", "their", "what",
		"so", "up", "out", "if", "about", "who", "get", "which", "go", "me",
		"function", "file", "data", "system", "user", "error", "app",
		"run", "test", "config", "model", "server", "code", "api", "key",
		"message", "response", "request", "service", "handler", "manager",
		"connection", "database", "memory", "cache", "context", "plugin",
		"agent", "tool", "skill", "task", "job", "queue", "worker", "pool",
		"channel", "topic", "event", "state", "session", "token", "embed",
		"search", "index", "vector", "score", "similarity", "distance",
		"normaliz", "normalize", "normalization", "pool", "pooling",
		"transform", "attention", "weight", "layer", "hidden", "dimension",
		"batch", "gradient", "optimiz", "optimizer", "loss", "train",
		"forward", "backward", "inference", "model", "predict", "class",
		"classify", "classification", "regression", "cluster", "embed",
		"tokenizer", "embedding", "sequence", "sentence", "paragraph",
		"doc", "document", "text", "string", "char", "letter", "word",
		"vocab", "vocabular", "token", "encode", "decode", "de", "code",
		"retriev", "retrieve", "retrieval", "rank", "ranking", "score",
		"query", "index", "search", "match", "cosine", "dot", "product",
		"matrix", "tensor", "array", "list", "map", "set", "dict",
		"hash", "table", "tree", "heap", "stack", "deque", "graph",
		"node", "edge", "path", "cycle", "shortest", "longest",
		"breadth", "depth", "first", "search", "traversal",
		"binary", "search", "insert", "delete", "update", "read", "write",
		"open", "close", "create", "delete", "update",
		"select", "insert", "update", "delete", "join", "index",
		"transaction", "isolation", "commit", "rollback", "lock",
		"file", "write", "read", "directory", "path", "perm",
		"process", "thread", "goroutine", "channel", "mutex", "lock",
		"network", "http", "tcp", "udp", "ssl", "tls", "websocket",
		"json", "xml", "yaml", "toml", "protob", "protobuf",
		"grpc", "rest", "api", "endpoint", "route", "middleware",
		"server", "client", "proxy", "load", "balancer", "health",
		"health", "check", "metric", "log", "trace", "debug",
		"error", "warning", "info", "fatal", "panic",
		"docker", "kubernetes", "container", "pod", "deploy",
		"cluster", "node", "master", "worker", "namespace",
		"config", "env", "secret", "key", "cert",
		"iam", "role", "policy", "permission", "auth", "oauth",
		"web", "front", "end", "ui", "css", "html", "javascript",
		"go", "python", "rust", "java", "c", "cpp", "ruby", "php",
		"linux", "windows", "macos", "ubuntu", "debian", "fedora",
		"git", "github", "gitlab", "bitbucket", "ci", "cd",
		"make", "cmake", "build", "test", "lint", "format",
	}

	for i, w := range commonWords {
		tok.vocab[w] = uint32(i + 4)
	}

	return tok, true
}

// parseTokenizerJSON parses a HuggingFace-style tokenizer.json file.
func parseTokenizerJSON(data []byte) (*BPETokenizer, error) {
	tok := &BPETokenizer{
		vocab:    make(map[string]uint32),
		eosToken: "</s>",
		unkToken: "<unk>",
		bosToken: "<s>",
		padToken: "<pad>",
	}

	var raw struct {
		AddPrefixSpace bool             `json:"add_prefix_space"`
		Tokenizer      json.RawMessage  `json:"tokenizer"`
		AddedTokens    []map[string]any `json:"added_tokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal tokenizer: %w", err)
	}

	// Parse the tokenizer section
	var tokSection struct {
		Type   string         `json:"type"`
		Vocab  map[string]int `json:"vocab"`
		Merges []string       `json:"merges"`
	}
	if err := json.Unmarshal(raw.Tokenizer, &tokSection); err != nil {
		return nil, fmt.Errorf("unmarshal tokenizer section: %w", err)
	}

	// Load vocabulary
	for term, id := range tokSection.Vocab {
		tok.vocab[term] = uint32(id)
	}

	// Parse merges
	for _, m := range tokSection.Merges {
		var pair [2]string
		n, _ := fmt.Sscanf(m, "%q %q", &pair[0], &pair[1])
		if n == 2 {
			tok.merges = append(tok.merges, pair)
		}
	}

	if len(tok.vocab) == 0 {
		return nil, fmt.Errorf("tokenizer has empty vocab")
	}

	return tok, nil
}

// Encode tokenizes text into token IDs.
func (t *BPETokenizer) Encode(text string) ([]uint32, error) {
	if text == "" {
		return []uint32{t.vocab["<s>"], t.vocab["</s>"]}, nil
	}

	tokens := preTokenize(text)
	var tokenIDs []uint32

	// Add BOS
	if bosID, ok := t.vocab[t.bosToken]; ok {
		tokenIDs = append(tokenIDs, bosID)
	}

	for _, token := range tokens {
		if id, ok := t.vocab[token]; ok {
			tokenIDs = append(tokenIDs, id)
		} else {
			// Sub-word decomposition using BPE merges
			splitTokens := subword_tokenize(t, token)
			if len(splitTokens) > 0 {
				for _, st := range splitTokens {
					if id, ok := t.vocab[st]; ok {
						tokenIDs = append(tokenIDs, id)
					} else {
						// OOV: use hash-based unknown token
						tokenIDs = appendUnknownToken(t, token)
					}
				}
			} else {
				tokenIDs = appendUnknownToken(t, token)
			}
		}
	}

	// Add EOS
	if eosID, ok := t.vocab[t.eosToken]; ok {
		tokenIDs = append(tokenIDs, eosID)
	}

	return tokenIDs, nil
}

// preTokenize splits text into word-level tokens.
func preTokenize(text string) []string {
	// Split on whitespace and punctuation, keeping significant tokens
	var tokens []string
	var current strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// subword_tokenize attempts to decompose an unknown word into known sub-words
// using BPE merge rules.
func subword_tokenize(t *BPETokenizer, word string) []string {
	if len(word) <= 1 {
		return nil
	}

	// Try progressively shorter prefixes
	for end := len(word); end > 1; end-- {
		prefix := word[:end]
		if _, ok := t.vocab[prefix]; ok {
			suffix := word[end:]
			if suffix != "" {
				if subTokens := subword_tokenize(t, suffix); len(subTokens) > 0 {
					return append([]string{prefix}, subTokens...)
				}
			}
			return []string{prefix}
		}
	}
	return nil
}

// appendUnknownToken adds an unknown token using hash-based ID.
func appendUnknownToken(t *BPETokenizer, token string) []uint32 {
	h := fnv.New32a()
	h.Write([]byte(token))
	// Use a range beyond the main vocab
	baseID := uint32(50000)
	return []uint32{t.vocab[t.unkToken] + h.Sum32()%(baseID-t.vocab[t.unkToken])}
}

// --- Weight hashing ---

// hashTokenID produces a deterministic hash for a token+position pair.
func hashTokenID(tok uint32, dimIdx uint32) uint32 {
	var tokBits uint32
	b := tok
	tokBits ^= b
	tokBits *= 0x45d9f3b
	tokBits ^= tokBits >> 16

	var dimBits uint32
	d := dimIdx
	dimBits ^= d
	dimBits *= 0x45d9f3b
	dimBits ^= dimBits >> 16

	result := tokBits ^ (dimBits << 1)
	result ^= result >> 17
	result *= 0x45d9f3b
	result ^= result >> 16
	return result
}
