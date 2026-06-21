package compress

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Pipeline is the main compression pipeline orchestrating all components.
//
// Flow:
// 1. ContentRouter detects content type
// 2. Appropriate compressor is applied
// 3. CCR store saves originals for retrieval
// 4. Compressed content returned with markers
type Pipeline struct {
	router     *ContentRouter
	ccrStore   CCRStore
	config     PipelineConfig
	mu         sync.RWMutex
	closed     bool
}

// PipelineConfig configures the Pipeline.
type PipelineConfig struct {
	// MinTokensToCompress is the minimum tokens for compression
	MinTokensToCompress int
	// TTL is the CCR store TTL for entries
	TTL time.Duration
	// EnableCCR enables CCR storage (retrieval markers)
	EnableCCR bool
	// CompressUserMessages enables compression of user messages
	CompressUserMessages bool
	// TargetRatio is the target compression ratio (for lossy compressors)
	TargetRatio float64
}

// DefaultPipelineConfig returns default configuration.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		MinTokensToCompress:  500,
		TTL:                 time.Hour,
		EnableCCR:           true,
		CompressUserMessages: false,
		TargetRatio:         0.0,
	}
}

// NewPipeline creates a new compression pipeline.
func NewPipeline(ccrStore CCRStore) *Pipeline {
	return NewPipelineWithConfig(ccrStore, DefaultPipelineConfig())
}

// NewPipelineWithConfig creates a pipeline with custom configuration.
func NewPipelineWithConfig(ccrStore CCRStore, config PipelineConfig) *Pipeline {
	router := NewContentRouter(DefaultContentRouterConfig())
	return &Pipeline{
		router:   router,
		ccrStore: ccrStore,
		config:   config,
	}
}

// CompressRequest represents a compression request.
type CompressRequest struct {
	// Messages to compress
	Messages []Message
	// Model name (for token counting)
	Model string
	// Query for relevance-based compression (optional)
	Query string
}

// Message represents an LLM message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompressionResult (pipeline-level) is the result of compressing messages.
type PipelineResult struct {
	// Messages is the compressed messages
	Messages []Message
	// TokensBefore is the total tokens before compression
	TokensBefore int
	// TokensAfter is the total tokens after compression
	TokensAfter int
	// TokensSaved is the difference
	TokensSaved int
	// TransformsApplied lists all transforms used
	TransformsApplied []string
}

// Compress compresses messages using the pipeline.
func (p *Pipeline) Compress(ctx context.Context, messages []Message, cfg CompressConfig) (*PipelineResult, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("pipeline is closed: %w", errors.New("closed"))
	}

	result := &PipelineResult{
		Messages: make([]Message, len(messages)),
	}

	for i, msg := range messages {
		// Skip user messages if configured
		if msg.Role == "user" && !cfg.CompressUserMessages {
			result.Messages[i] = msg
			result.TokensBefore += countTokens(msg.Content)
			result.TokensAfter += countTokens(msg.Content)
			continue
		}

		// Skip short content
		tokens := countTokens(msg.Content)
		if tokens < p.config.MinTokensToCompress {
			result.Messages[i] = msg
			result.TokensBefore += tokens
			result.TokensAfter += tokens
			continue
		}

		// Detect content type
		contentType := p.router.DetectType(msg.Content)

		// Compress
		compressed, crushResult := p.router.Compress(msg.Content, contentType, "")

		// Store in CCR if enabled and compression was effective
		hash := ""
		if p.config.EnableCCR && crushResult.TokensSaved > 0 {
			entry := CCREntry{
				OriginalContent:   msg.Content,
				CompressedContent: compressed,
				OriginalTokens:    crushResult.OriginalTokens,
				CompressedTokens:  crushResult.CompressedTokens,
				Strategy:          crushResult.Strategy,
				TTL:               p.config.TTL,
				ExpiresAt:         time.Now().Add(p.config.TTL),
			}
			h, err := p.ccrStore.Store(ctx, entry)
			if err == nil {
				hash = h
				// Add retrieval marker to compressed content
				compressed = fmt.Sprintf("%s\n\n%s", compressed, MarkerFormat(h))
			}
		}

		result.Messages[i] = Message{
			Role:    msg.Role,
			Content: compressed,
		}
		result.TokensBefore += crushResult.OriginalTokens
		result.TokensAfter += crushResult.CompressedTokens
		result.TransformsApplied = appendUnique(result.TransformsApplied, crushResult.TransformsApplied...)

		_ = hash // nolint:ineffassign // Could log for metrics
	}

	result.TokensSaved = max(0, result.TokensBefore-result.TokensAfter)
	return result, nil
}

// Stats returns pipeline statistics.
func (p *Pipeline) Stats() PipelineStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PipelineStats{}
	if p.ccrStore != nil {
		storeStats := p.ccrStore.Stats()
		stats.CCREntries = storeStats.EntryCount
		stats.CCRTotalOriginalTokens = storeStats.TotalOriginalTokens
		stats.CCRTotalCompressedTokens = storeStats.TotalCompressedTokens
		stats.CCRTotalRetrievals = storeStats.TotalRetrievals
	}
	return stats
}

// PipelineStats provides pipeline statistics.
type PipelineStats struct {
	CCREntries              int64
	CCRTotalOriginalTokens  int64
	CCRTotalCompressedTokens int64
	CCRTotalRetrievals      int64
}

// Close releases pipeline resources.
func (p *Pipeline) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	if p.ccrStore != nil {
		return p.ccrStore.Close()
	}
	return nil
}

// CompressConfig is the user-facing compression configuration.
// This mirrors AgentCompressionConfig from internal/config/schema.go
// but is tailored for pipeline use.
type CompressConfig struct {
	// CompressUserMessages enables compression of user messages
	CompressUserMessages bool
	// MinTokensToCompress is the minimum token count
	MinTokensToCompress int
	// TargetRatio is for lossy compression (0.0 = auto)
	TargetRatio float64
}

// CompressToolResult compresses a tool execution result.
// It returns the compressed content as a JSON string with markers.
// This is a convenience method for agent loop integration.
func (p *Pipeline) CompressToolResult(ctx context.Context, toolName string, output string, maxTokens int) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return "", fmt.Errorf("pipeline closed: %w", errors.New("closed"))
	}

	// Skip if too short
	tokens := countTokens(output)
	if tokens < p.config.MinTokensToCompress {
		return output, nil
	}

	// Detect content type
	contentType := p.router.DetectType(output)

	// Compress
	compressed, crushResult := p.router.Compress(output, contentType, "")

	// Store in CCR if enabled and compression was effective
	if p.config.EnableCCR && crushResult.TokensSaved > 0 {
		entry := CCREntry{
			OriginalContent:   output,
			CompressedContent: compressed,
			OriginalTokens:    crushResult.OriginalTokens,
			CompressedTokens:  crushResult.CompressedTokens,
			Strategy:          crushResult.Strategy,
			ToolName:          toolName,
			TTL:               p.config.TTL,
			ExpiresAt:         time.Now().Add(p.config.TTL),
		}
		hash, err := p.ccrStore.Store(ctx, entry)
		if err == nil {
			// Add retrieval marker to compressed content
			compressed = fmt.Sprintf("%s\n\n%s", compressed, VerboseMarkerFormat(0, crushResult.CompressedTokens, hash))
		}
	}

	// Check against maxTokens and truncate if needed
	if countTokens(compressed) > maxTokens {
		compressed = truncateString(compressed, maxTokens)
	}

	return compressed, nil
}

// truncateString truncates a string to approximately maxTokens.
func truncateString(s string, maxTokens int) string {
	const charsPerToken = 3
	maxChars := maxTokens * charsPerToken
	if len(s) <= maxChars {
		return s
	}
	if maxChars <= 20 {
		return "...[truncated]"
	}
	return s[:maxChars-20] + "...[truncated]"
}
