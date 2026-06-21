// Package compress provides context compression for LLM messages.
//
// This package implements Headroom-style compression adapted for Meept:
// - CCR (Compress-Cache-Retrieve): Reversible compression with SQLite storage
// - SmartCrusher: JSON/tool output compression (70-90% reduction)
// - CodeCompressor: AST-aware code compression via tree-sitter
// - Content Router: Automatic content-type detection and routing
//
// Usage:
//
//	pipeline := compress.NewPipeline(ccrStore)
//	result := pipeline.Compress(ctx, messages, config)
//
// Configuration is controlled by AgentCompressionConfig in internal/config/schema.go.
package compress

import (
	"time"
)

// CompressionStrategy identifies which compressor was used.
type CompressionStrategy string

const (
	StrategySmartCrusher CompressionStrategy = "smart_crusher"
	StrategyCode         CompressionStrategy = "code"
	StrategyLog          CompressionStrategy = "log"
	StrategySearch       CompressionStrategy = "search"
	StrategyPassthrough  CompressionStrategy = "passthrough"
	StrategyUnknown      CompressionStrategy = "unknown"
)

// CCREntry represents a compressed content entry stored in the CCR store.
type CCREntry struct {
	// Hash is the content-addressed identifier (24 hex chars)
	Hash string
	// OriginalContent is the full uncompressed content
	OriginalContent string
	// CompressedContent is the compressed version (may include markers)
	CompressedContent string
	// OriginalTokens is the token count before compression
	OriginalTokens int
	// CompressedTokens is the token count after compression
	CompressedTokens int
	// Strategy is which compressor was used
	Strategy CompressionStrategy
	// ToolName is which tool produced this (optional)
	ToolName string
	// CreatedAt is when the entry was created
	CreatedAt time.Time
	// TTL is how long the entry should be retained
	TTL time.Duration
	// ExpiresAt is when the entry should be deleted
	ExpiresAt time.Time
	// RetrievalCount tracks how many times this was retrieved
	RetrievalCount int64
}

// CompressionResult is returned after compressing content.
type CompressionResult struct {
	// OriginalContent is the input before compression
	OriginalContent string
	// CompressedContent is the output after compression
	CompressedContent string
	// OriginalTokens is the token count before compression
	OriginalTokens int
	// CompressedTokens is the token count after compression
	CompressedTokens int
	// TokensSaved is the difference (OriginalTokens - CompressedTokens)
	TokensSaved int
	// CompressionRatio is CompressedTokens / OriginalTokens (lower = better)
	CompressionRatio float64
	// Strategy is which compressor was used
	Strategy CompressionStrategy
	// Hash is the CCR store key for retrieval (empty if not stored)
	Hash string
	// TransformsApplied lists the specific transforms used
	TransformsApplied []string
}

// CCRStats provides metrics about the CCR store.
type CCRStats struct {
	// EntryCount is the number of entries in the store
	EntryCount int64
	// TotalOriginalTokens is the sum of all original tokens
	TotalOriginalTokens int64
	// TotalCompressedTokens is the sum of all compressed tokens
	TotalCompressedTokens int64
	// TotalRetrievals is the sum of all retrieval counts
	TotalRetrievals int64
	// ExpiredCount is the number of entries expired (pending cleanup)
	ExpiredCount int64
}

// CCRSearchResult is returned when searching within compressed content.
type CCRSearchResult struct {
	// Hash is the entry hash
	Hash string
	// MatchedContent is the portion that matched the query
	MatchedContent string
	// Context is surrounding content for relevance
	Context string
	// Score is the relevance score (0-1)
	Score float64
}
