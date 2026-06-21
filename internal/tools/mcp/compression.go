package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/caimlas/meept/internal/compress"
	"github.com/caimlas/meept/internal/llm"
)

// CompressionConfig holds the settings needed by the compression tools.
type CompressionConfig struct {
	// Enabled turns compression on/off.
	Enabled bool
	// MinTokensToCompress is the minimum token count for compression to be attempted.
	MinTokensToCompress int
	// TTL is the CCR store TTL for entries.
	TTL int64 // seconds, 0 = use pipeline default
}

// DefaultCompressionConfig returns the default configuration.
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Enabled:             false,
		MinTokensToCompress: 500,
		TTL:                 3600,
	}
}

// UpdateConfig replaces the handler's config with the provided value.
func (h *CompressionHandler) UpdateConfig(cfg CompressionConfig) {
	h.config = cfg
}

// CompressionHandler provides local tools for explicit compression/retrieval
// via mcc_compress, mcc_retrieve, and mcc_stats. It wraps a compression
// Pipeline for the actual compression work and a CCRStore for hash-based
// retrieval of originals.
type CompressionHandler struct {
	pipeline       *compress.Pipeline
	ccrStore       compress.CCRStore
	config         CompressionConfig
	totalSaved     atomic.Int64
	retrievalCount atomic.Int64
}

// NewCompressionHandler creates a CompressionHandler with the given pipeline,
// CCR store, and config. Returns nil if compression is not enabled or neither
// pipeline nor CCR store is available.
func NewCompressionHandler(pipeline *compress.Pipeline, ccrStore compress.CCRStore, config CompressionConfig) *CompressionHandler {
	if !config.Enabled {
		return nil
	}
	if pipeline == nil && ccrStore == nil {
		return nil
	}
	return &CompressionHandler{
		pipeline: pipeline,
		ccrStore: ccrStore,
		config:   config,
	}
}

// Tools returns the list of llm.ToolDefinition for the compression tools.
func (h *CompressionHandler) Tools() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		h.mccCompressDef(),
		h.mccRetrieveDef(),
		h.mccStatsDef(),
	}
}

// mccCompressDef returns the tool definition for mcc_compress.
func (h *CompressionHandler) mccCompressDef() llm.ToolDefinition {
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "mcc_compress",
			Description: "Compress content (JSON, code, logs, or text) using the configured compression pipeline. Returns a hash that can be used later with mcc_retrieve to get the original content. Only meaningful for large outputs (default threshold is 500 tokens).",
			Parameters: llm.FunctionParameters{
				Type: "object",
				Properties: map[string]llm.ParameterProperty{
					"content": {
						Type:        "string",
						Description: "The content string to compress.",
					},
					"tool_name": {
						Type:        "string",
						Description: "Optional: name of the tool that produced this content (used for analytics).",
					},
				},
				Required: []string{"content"},
			},
		},
	}
}

// mccRetrieveDef returns the tool definition for mcc_retrieve.
func (h *CompressionHandler) mccRetrieveDef() llm.ToolDefinition {
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "mcc_retrieve",
			Description: "Retrieve the original (uncompressed) content by its compression hash. Returns the full original string with found set to false if the hash is not found or has expired.",
			Parameters: llm.FunctionParameters{
				Type: "object",
				Properties: map[string]llm.ParameterProperty{
					"hash": {
						Type:        "string",
						Description: "The compression hash returned by mcc_compress.",
					},
				},
				Required: []string{"hash"},
			},
		},
	}
}

// mccStatsDef returns the tool definition for mcc_stats.
func (h *CompressionHandler) mccStatsDef() llm.ToolDefinition {
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "mcc_stats",
			Description: "Return compression statistics: entry count, total tokens saved, and retrieval count.",
			Parameters: llm.FunctionParameters{
				Type: "object",
				Properties: map[string]llm.ParameterProperty{},
			},
		},
	}
}

// Execute dispatches a tool call by name to the appropriate handler.
func (h *CompressionHandler) Execute(ctx context.Context, toolName string, args map[string]any) (any, error) {
	switch toolName {
	case "mcc_compress":
		return h.execCompress(ctx, args)
	case "mcc_retrieve":
		return h.execRetrieve(ctx, args)
	case "mcc_stats":
		return h.execStats(args)
	default:
		return nil, fmt.Errorf("unknown compression tool: %s", toolName) //nolint:goerr113 // invalid tool name
	}
}

// execCompress implements mcc_compress: compress content, store in CCR, return hash + stats.
func (h *CompressionHandler) execCompress(ctx context.Context, args map[string]any) (any, error) {
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return nil, fmt.Errorf("content is required: %w", errors.New("missing content"))
	}

	toolName, _ := args["tool_name"].(string)

	pipeline := h.pipeline
	if pipeline == nil {
		return nil, fmt.Errorf("compression pipeline not available: %w", errors.New("pipeline unavailable"))
	}

	minTokens := h.config.MinTokensToCompress
	if minTokens <= 0 {
		minTokens = 500
	}

	// Compress via CompressToolResult — handles CCR storage + marker injection.
	compressed, err := pipeline.CompressToolResult(ctx, toolName, content, 1_000_000)
	if err != nil {
		return nil, fmt.Errorf("compression failed: %w", err)
	}

	originalTokens := estimateTokens(content)
	compressedTokens := estimateTokens(compressed)

	// Extract hash from the CCR marker appended to compressed content.
	// Markers look like: <<ccr:HASH>> or [N items compressed to X tokens, hash=HASH]
	hash := extractHashFromContent(compressed)

	saved := max(0, originalTokens-compressedTokens)
	if saved > 0 {
		h.totalSaved.Add(int64(saved))
	}

	return map[string]any{
		"hash":              hash,
		"original_tokens":   originalTokens,
		"compressed_tokens": compressedTokens,
		"saved":             saved,
	}, nil
}

// execRetrieve implements mcc_retrieve: look up original content by hash.
func (h *CompressionHandler) execRetrieve(ctx context.Context, args map[string]any) (any, error) {
	hash, ok := args["hash"].(string)
	if !ok || hash == "" {
		return nil, fmt.Errorf("hash is required: %w", errors.New("missing hash"))
	}

	store := h.ccrStore
	if store == nil {
		return nil, fmt.Errorf("compression store not available: %w", errors.New("store unavailable"))
	}

	entry, err := store.Retrieve(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("retrieval failed: %w", err)
	}
	if entry == nil {
		return map[string]any{
			"original": "",
			"found":    false,
		}, nil
	}

	h.retrievalCount.Add(1)

	return map[string]any{
		"original":   entry.OriginalContent,
		"found":      true,
		"strategy":   string(entry.Strategy),
		"hash":       entry.Hash,
		"tool_name":  entry.ToolName,
		"created_at": entry.CreatedAt,
	}, nil
}

// execStats implements mcc_stats: return compression/retrieval stats.
func (h *CompressionHandler) execStats(_ map[string]any) (any, error) {
	var ccrEntries int64
	var ccrTotalRetrievals int64

	if h.ccrStore != nil {
		st := h.ccrStore.Stats()
		ccrEntries = st.EntryCount
		ccrTotalRetrievals = st.TotalRetrievals
	}

	return map[string]any{
		"entry_count":      ccrEntries,
		"total_saved":      h.totalSaved.Load(),
		"retrieval_count":  h.retrievalCount.Load(),
		"store_entries":    ccrEntries,
		"store_retrievals": ccrTotalRetrievals,
	}, nil
}

// estimateTokens approximates the token count of a string (1 token ≈ 4 chars).
func estimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return len(s) / 4
}

// extractHashFromContent searches for a CCR hash marker within the content.
// Returns the hash if found, or "" if no marker is present.
func extractHashFromContent(content string) string {
	// Trim trailing whitespace that may have been appended.
	content = strings.TrimSpace(content)

	// Try standard marker: <<ccr:HASH>>
	if idx := strings.Index(content, "<<ccr:"); idx >= 0 {
		marker := content[idx:]
		if hash := compress.ParseMarker(marker); hash != "" {
			return hash
		}
	}

	// Try verbose marker: [...hash=HASH]
	if idx := strings.Index(content, "hash="); idx >= 0 {
		// Extract everything after "hash=" until "]"
		rest := content[idx+5:]
		end := strings.Index(rest, "]")
		if end > 0 {
			hash := strings.TrimSpace(rest[:end])
			if len(hash) == compress.HashLength {
				return hash
			}
		}
	}

	return ""
}
