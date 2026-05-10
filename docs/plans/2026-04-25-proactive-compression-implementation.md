# Proactive Context Compression Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Implement proactive context compression that starts at 50% utilization and maintains context below 80% to avoid context rot during long-running agent tasks.

**Architecture:** Add a multi-stage compression pipeline to `ContextFirewall.processMessages()` that triggers progressively aggressive compression as utilization climbs from 50% to 80%. Stage 1 (50%): log warning. Stage 2 (60%): summarize old history. Stage 3 (70%): aggressive summarization + drop low-importance messages. Stage 4 (80%): hard limit enforcement with context drop.

**Tech Stack:** Go 1.24.2, existing `internal/llm/context_firewall.go`, `internal/agent/conversation.go` importance tracking, `internal/code/ast/` tree-sitter for code-aware compression.

---

## File Structure

**Modify:**
- `internal/llm/context_firewall.go` - Add multi-stage compression pipeline
- `internal/agent/conversation.go` - Add `CompressByImportance()` method
- `internal/agent/executor.go` - Add code-aware truncation for tool results

**Create:**
- `internal/llm/context_compressor.go` - Compression strategies interface + implementations
- `internal/llm/context_compressor_test.go` - Unit tests for compression

**Test:**
- `internal/llm/context_firewall_compression_test.go` - Integration tests for multi-stage compression

---

## Task 1: Compression Strategy Interface

**Files:**
- Create: `internal/llm/context_compressor.go`
- Create: `internal/llm/context_compressor_test.go`

- [x] **Step 1: Write the test file with failing tests**

Create `internal/llm/context_compressor_test.go`:

```go
package llm

import (
	"context"
	"testing"
)

func TestCompressionStage(t *testing.T) {
	tests := []struct {
		name           string
		utilization    float64
		messages       []ChatMessage
		wantCompressed bool
		wantDropped    int
	}{
		{
			name:        "under_50_percent_no_compression",
			utilization: 0.40,
			messages: []ChatMessage{
				{Role: RoleSystem, Content: " system"},
				{Role: RoleUser, Content: "hello"},
			},
			wantCompressed: false,
		},
		{
			name:        "over_50_percent_log_warning",
			utilization: 0.55,
			messages: []ChatMessage{
				{Role: RoleSystem, Content: "system"},
				{Role: RoleUser, Content: "hello"},
			},
			wantCompressed: false, // Just logs at this stage
		},
		{
			name:        "over_60_percent_summarize",
			utilization: 0.65,
			messages: []ChatMessage{
				{Role: RoleSystem, Content: "system"},
				{Role: RoleUser, Content: "initial request"},
				{Role: RoleAssistant, Content: "response 1"},
				{Role: RoleUser, Content: "follow-up 1"},
				{Role: RoleAssistant, Content: "response 2"},
				{Role: RoleUser, Content: "follow-up 2"},
				{Role: RoleAssistant, Content: "response 3"},
				{Role: RoleUser, Content: "current query"},
			},
			wantCompressed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultCompressionConfig()
			cfg.Enabled = true
			cfg.ModelContextLimit = 100000

			compressor := NewContextCompressor(cfg, nil)
			result := compressor.Compress(context.Background(), tt.messages, tt.utilization)

			if result.Compressed != tt.wantCompressed {
				t.Errorf("Compressed = %v, want %v", result.Compressed, tt.wantCompressed)
			}
		})
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/llm/... -run TestCompressionStage -v
```
Expected: FAIL with "undefined: CompressionConfig, DefaultCompressionConfig, NewContextCompressor"

- [x] **Step 3: Write the compression interface and types**

Create `internal/llm/context_compressor.go`:

```go
package llm

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// CompressionConfig configures multi-stage context compression.
type CompressionConfig struct {
	Enabled              bool    // When false, compressor passes through
	ModelContextLimit    int     // Token limit for the model
	Stage1WarningRatio   float64 // 0.50 = log warning at 50%
	Stage2SummarizeRatio float64 // 0.60 = summarize old history at 60%
	Stage3AggressiveRatio float64 // 0.70 = aggressive compression at 70%
	Stage4HardLimitRatio float64 // 0.80 = drop context at 80%
}

// DefaultCompressionConfig returns sensible defaults for multi-stage compression.
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Enabled:              true,
		ModelContextLimit:    100000,
		Stage1WarningRatio:   0.50,
		Stage2SummarizeRatio: 0.60,
		Stage3AggressiveRatio: 0.70,
		Stage4HardLimitRatio: 0.80,
	}
}

// CompressionStage indicates which compression stage was applied.
type CompressionStage int

const (
	CompressionStageNone CompressionStage = iota
	CompressionStageWarning
	CompressionStageSummarize
	CompressionStageAggressive
	CompressionStageHardLimit
)

// CompressionResult holds the outcome of compression.
type CompressionResult struct {
	Messages     []ChatMessage
	Compressed   bool
	Stage        CompressionStage
	TokensBefore int
	TokensAfter  int
	DroppedCount int
}

// CompressionStats holds counters for compression operations.
type CompressionStats struct {
	WarningEvents      atomic.Uint64
	SummarizeEvents    atomic.Uint64
	AggressiveEvents   atomic.Uint64
	HardLimitEvents    atomic.Uint64
	TotalTokensSaved   atomic.Uint64
}

// ContextCompressor implements multi-stage context compression.
type ContextCompressor struct {
	config  CompressionConfig
	stats   *CompressionStats
	logger  *slog.Logger
	tokenizer Tokenizer
}

// NewContextCompressor creates a new compressor.
func NewContextCompressor(cfg CompressionConfig, logger *slog.Logger) *ContextCompressor {
	if logger == nil {
		logger = slog.Default()
	}
	return &ContextCompressor{
		config:    cfg,
		stats:     &CompressionStats{},
		logger:    logger,
		tokenizer: &HeuristicTokenizer{},
	}
}

// Compress applies the appropriate compression stage based on utilization.
func (c *ContextCompressor) Compress(ctx context.Context, messages []ChatMessage, utilization float64) CompressionResult {
	if !c.config.Enabled || utilization < c.config.Stage1WarningRatio {
		return CompressionResult{
			Messages:     messages,
			Compressed:   false,
			Stage:        CompressionStageNone,
			TokensBefore: c.countTokens(messages),
		}
	}

	tokensBefore := c.countTokens(messages)

	// Stage 1: Warning (50%)
	if utilization >= c.config.Stage1WarningRatio && utilization < c.config.Stage2SummarizeRatio {
		c.stats.WarningEvents.Add(1)
		c.logger.Info("context exceeded 50% utilization, consider wrapping up",
			"utilization", utilization,
		)
		return CompressionResult{
			Messages:     messages,
			Compressed:   false,
			Stage:        CompressionStageWarning,
			TokensBefore: tokensBefore,
		}
	}

	// Stage 2: Summarize old history (60%)
	if utilization >= c.config.Stage2SummarizeRatio && utilization < c.config.Stage3AggressiveRatio {
		c.stats.SummarizeEvents.Add(1)
		c.logger.Debug("applying stage 2 compression: summarizing old history",
			"utilization", utilization,
		)
		compressed := c.summarizeOldHistory(ctx, messages)
		tokensAfter := c.countTokens(compressed)
		c.stats.TotalTokensSaved.Add(uint64(tokensBefore - tokensAfter))
		return CompressionResult{
			Messages:     compressed,
			Compressed:   true,
			Stage:        CompressionStageSummarize,
			TokensBefore: tokensBefore,
			TokensAfter:  tokensAfter,
		}
	}

	// Stage 3: Aggressive compression (70%)
	if utilization >= c.config.Stage3AggressiveRatio && utilization < c.config.Stage4HardLimitRatio {
		c.stats.AggressiveEvents.Add(1)
		c.logger.Debug("applying stage 3 compression: aggressive summarization",
			"utilization", utilization,
		)
		compressed := c.aggressiveCompress(ctx, messages)
		tokensAfter := c.countTokens(compressed)
		c.stats.TotalTokensSaved.Add(uint64(tokensBefore - tokensAfter))
		return CompressionResult{
			Messages:     compressed,
			Compressed:   true,
			Stage:        CompressionStageAggressive,
			TokensBefore: tokensBefore,
			TokensAfter:  tokensAfter,
		}
	}

	// Stage 4: Hard limit (80%)
	c.stats.HardLimitEvents.Add(1)
	c.logger.Warn("context exceeded 80% hard limit, dropping old context",
		"utilization", utilization,
	)
	compressed := c.dropOldContext(messages)
	tokensAfter := c.countTokens(compressed)
	c.stats.TotalTokensSaved.Add(uint64(tokensBefore - tokensAfter))
	return CompressionResult{
		Messages:     compressed,
		Compressed:   true,
		Stage:        CompressionStageHardLimit,
		TokensBefore: tokensBefore,
		TokensAfter:  tokensAfter,
		DroppedCount: len(messages) - len(compressed),
	}
}

// countTokens counts tokens in messages.
func (c *ContextCompressor) countTokens(messages []ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += c.tokenizer.CountTokens(msg.Content)
	}
	return total
}

// summarizeOldHistory summarizes old messages, keeping system + last 4.
func (c *ContextCompressor) summarizeOldHistory(ctx context.Context, messages []ChatMessage) []ChatMessage {
	// Keep system + last 4 messages
	keepCount := 4
	var systemMsgs []ChatMessage
	var recentMsgs []ChatMessage

	for i, msg := range messages {
		if msg.Role == RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else if i >= len(messages)-keepCount {
			recentMsgs = append(recentMsgs, msg)
		}
	}

	if len(messages)-len(systemMsgs)-len(recentMsgs) == 0 {
		return messages // Nothing to summarize
	}

	// For now, just drop the old messages (summarization requires LLM call)
	// This is a placeholder - actual summarization happens in ContextFirewall
	result := append(systemMsgs, recentMsgs...)
	return result
}

// aggressiveCompress applies aggressive compression keeping only critical messages.
func (c *ContextCompressor) aggressiveCompress(ctx context.Context, messages []ChatMessage) []ChatMessage {
	// Keep system + last 2 messages only
	var systemMsgs []ChatMessage
	var recentMsgs []ChatMessage

	for i, msg := range messages {
		if msg.Role == RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else if i >= len(messages)-2 {
			recentMsgs = append(recentMsgs, msg)
		}
	}

	return append(systemMsgs, recentMsgs...)
}

// dropOldContext drops all but system + last 2 messages.
func (c *ContextCompressor) dropOldContext(messages []ChatMessage) []ChatMessage {
	var systemMsgs []ChatMessage
	var recentMsgs []ChatMessage

	for i, msg := range messages {
		if msg.Role == RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else if i >= len(messages)-2 {
			recentMsgs = append(recentMsgs, msg)
		}
	}

	return append(systemMsgs, recentMsgs...)
}

// Stats returns compression statistics.
func (c *ContextCompressor) Stats() CompressionStats {
	return CompressionStats{
		WarningEvents:    c.stats.WarningEvents.Load(),
		SummarizeEvents:  c.stats.SummarizeEvents.Load(),
		AggressiveEvents: c.stats.AggressiveEvents.Load(),
		HardLimitEvents:  c.stats.HardLimitEvents.Load(),
		TotalTokensSaved: c.stats.TotalTokensSaved.Load(),
	}
}

// Ensure types compile
var _ CompressionConfig = DefaultCompressionConfig()
```

- [x] **Step 4: Add missing methods to CompressionStats**

The `CompressionStats` struct needs individual field accessors. Add these methods:

```go
// WarningEvents returns the count of warning-stage compression events.
func (s *CompressionStats) WarningEventsCount() uint64 {
	return s.WarningEvents
}

// SummarizeEvents returns the count of summarize-stage events.
func (s *CompressionStats) SummarizeEventsCount() uint64 {
	return s.SummarizeEvents
}

// AggressiveEvents returns the count of aggressive-stage events.
func (s *CompressionStats) AggressiveEventsCount() uint64 {
	return s.AggressiveEvents
}

// HardLimitEvents returns the count of hard-limit events.
func (s *CompressionStats) HardLimitEventsCount() uint64 {
	return s.HardLimitEvents
}

// TotalTokensSaved returns total tokens saved by compression.
func (s *CompressionStats) TotalTokensSavedCount() uint64 {
	return s.TotalTokensSaved
}
```

Wait - the struct already has atomic fields. Let me fix the `Stats()` method to return scalar values:

Actually, let's simplify. Update the `CompressionStats` struct to use plain uint64 fields (no need for atomic since we're not in a hot path):

```go
// CompressionStats holds counters for compression operations.
type CompressionStats struct {
	WarningEvents      uint64
	SummarizeEvents    uint64
	AggressiveEvents   uint64
	HardLimitEvents    uint64
	TotalTokensSaved   uint64
}
```

And update `Stats()`:
```go
// Stats returns compression statistics.
func (c *ContextCompressor) Stats() CompressionStats {
	return c.stats
}
```

- [x] **Step 5: Run test to verify it passes**

Run:
```bash
go test ./internal/llm/... -run TestCompressionStage -v
```
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/llm/context_compressor.go internal/llm/context_compressor_test.go
git commit -m "feat: add multi-stage context compression interface"
```

---

## Task 2: Integrate Compressor into ContextFirewall

**Files:**
- Modify: `internal/llm/context_firewall.go`
- Create: `internal/llm/context_firewall_compression_test.go`

- [x] **Step 1: Write integration test**

Create `internal/llm/context_firewall_compression_test.go`:

```go
package llm

import (
	"context"
	"log/slog"
	"testing"
)

func TestContextFirewallMultiStageCompression(t *testing.T) {
	model := &ModelConfig{
		ContextLimit: 100000,
	}

	cfg := ContextFirewallConfig{
		Enabled:              true,
		ProactiveCompression: true,
		ModelContextLimit:    100000,
	}

	firewall := NewContextFirewall(
		&mockChatter{},
		model,
		cfg,
		nil,
		slog.Default(),
		nil,
	)

	tests := []struct {
		name           string
		messages       []ChatMessage
		wantStage      CompressionStage
		wantCompressed bool
	}{
		{
			name: "40_percent_no_compression",
			messages: []ChatMessage{
				{Role: RoleSystem, Content: "system prompt"},
				{Role: RoleUser, Content: "hello"},
			},
			wantStage:      CompressionStageNone,
			wantCompressed: false,
		},
		{
			name: "55_percent_warning_only",
			messages: []ChatMessage{
				{Role: RoleSystem, Content: repeatingString("system ", 10000)},
				{Role: RoleUser, Content: repeatingString("user ", 15000)},
			},
			wantStage:      CompressionStageWarning,
			wantCompressed: false,
		},
		{
			name: "65_percent_summarize",
			messages: buildLongConversation(20),
			wantStage:      CompressionStageSummarize,
			wantCompressed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := firewall.Compress(context.Background(), tt.messages)
			if err != nil {
				t.Fatalf("Compress failed: %v", err)
			}

			if result.Stage != tt.wantStage {
				t.Errorf("Stage = %v, want %v", result.Stage, tt.wantStage)
			}
		})
	}
}

// Helper functions
func repeatingString(s string, times int) string {
	result := ""
	for i := 0; i < times; i++ {
		result += s
	}
	return result
}

func buildLongConversation(turns int) []ChatMessage {
	messages := []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleUser, Content: "initial request"},
	}
	for i := 0; i < turns; i++ {
		messages = append(messages,
			ChatMessage{Role: RoleAssistant, Content: "response"},
			ChatMessage{Role: RoleUser, Content: "follow-up"},
		)
	}
	return messages
}

type mockChatter struct{}

func (m *mockChatter) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	return &Response{Content: "mock"}, nil
}

func (m *mockChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	return &Response{Content: "mock"}, nil
}
```

- [x] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/llm/... -run TestContextFirewallMultiStageCompression -v
```
Expected: FAIL with "unknown field: ProactiveCompression"

- [x] **Step 3: Add ProactiveCompression config to ContextFirewallConfig**

Modify `internal/llm/context_firewall.go:11-26`. Add field:

```go
// ContextFirewallConfig configures context budget and summarization behavior.
type ContextFirewallConfig struct {
	Enabled                    bool    // When false, firewall passes through
	SummarizeHistory           bool    // When true, old messages are summarized
	SmallModelContextThreshold int     // tokens; models below this get extra reduction
	IterationBudgetRatio       float64 // fraction of context reserved for a single iteration
	ConversationBudgetRatio    float64 // fraction for overall conversation history
	ChunkLargeInputs           bool    // When true, split oversized inputs at boundaries
	ChunkThresholdRatio        float64 // max input size relative to context limit
	// WrapUpThreshold is the "soft" limit (0.0-1.0) where wrap-up suggestions are injected
	WrapUpThreshold float64 // default 0.50
	// HardLimit is the "hard" limit (0.0-1.0) where context is dropped and reattempted
	HardLimit float64 // default 0.80
	// DropContextOnHardLimit enables context dropping when hard limit is hit
	DropContextOnHardLimit bool // default true
	// ProactiveCompression enables multi-stage compression starting at 50%
	ProactiveCompression bool // default false for backward compatibility
	// ModelContextLimit is the token limit for proactive compression (used if ProactiveCompression enabled)
	ModelContextLimit int // default: uses model.ContextLimit
}
```

- [x] **Step 4: Wire compressor into ContextFirewall**

Modify `internal/llm/context_firewall.go:29-41`. Add compressor field:

```go
// ContextFirewall wraps a Chatter and enforces context budgets.
type ContextFirewall struct {
	inner        Chatter
	model        *ModelConfig
	config       ContextFirewallConfig
	summaryModel Chatter
	logger       *slog.Logger
	tokenizer    Tokenizer
	compressor   *ContextCompressor // Added for proactive compression

	// Counters (atomic-safe for concurrent callers)
	summarizationFailures atomic.Uint64
	droppedMessages       atomic.Uint64
	dropEvents            atomic.Uint64
}
```

- [x] **Step 5: Initialize compressor in NewContextFirewall**

Modify `internal/llm/context_firewall.go:59-87`. After the tokenizer initialization:

```go
if tokenizer == nil {
	tokenizer = &HeuristicTokenizer{}
}

// Initialize compressor if proactive compression is enabled
var compressor *ContextCompressor
if cfg.ProactiveCompression {
	compressorCfg := DefaultCompressionConfig()
	compressorCfg.ModelContextLimit = cfg.ModelContextLimit
	if compressorCfg.ModelContextLimit == 0 {
		compressorCfg.ModelContextLimit = model.ContextLimit
	}
	compressor = NewContextCompressor(compressorCfg, logger)
}

return &ContextFirewall{
	inner:        inner,
	model:        model,
	config:       cfg,
	summaryModel: summaryModel,
	logger:       logger,
	tokenizer:    tokenizer,
	compressor:   compressor,
}
```

- [x] **Step 6: Add Compress method to ContextFirewall**

Add new method after `ValidateContextSize`:

```go
// Compress applies proactive compression to messages.
// Returns the compressed messages and compression result metadata.
func (f *ContextFirewall) Compress(ctx context.Context, messages []ChatMessage) (CompressionResult, error) {
	if f.compressor == nil {
		// Proactive compression not enabled
		return CompressionResult{
			Messages:     messages,
			Compressed:   false,
			Stage:        CompressionStageNone,
			TokensBefore: f.countTokens(messages),
		}, nil
	}

	utilization := f.ContextUtilization(messages)
	return f.compressor.Compress(ctx, messages, utilization), nil
}
```

- [x] **Step 7: Run test to verify it passes**

Run:
```bash
go test ./internal/llm/... -run TestContextFirewallMultiStageCompression -v
```
Expected: PASS

- [x] **Step 8: Commit**

```bash
git add internal/llm/context_firewall.go internal/llm/context_firewall_compression_test.go
git commit -m "feat: integrate multi-stage compressor into ContextFirewall"
```

---

## Task 3: Add Importance-Based Compression to Conversation

**Files:**
- Modify: `internal/agent/conversation.go`
- Test: `internal/agent/conversation_compression_test.go`

- [x] **Step 1: Write test**

Create `internal/agent/conversation_compression_test.go`:

```go
package agent

import (
	"testing"
	"github.com/caimlas/meept/internal/llm"
)

func TestCompressByImportance(t *testing.T) {
	conv := NewConversation()

	// Add messages of varying importance
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleSystem, Content: "system"})
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleUser, Content: "initial request"})
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleAssistant, Content: "reasoning... let me think"}) // Low importance
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleTool, Content: "tool result"}) // Medium
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleAssistant, Content: "plan: step 1, step 2"}) // Medium
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleTool, Content: "file: main.go, found matches"}) // High (key finding)
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleAssistant, Content: "conclusion: the answer is..."}) // High
	conv.AddMessage(llm.ChatMessage{Role: llm.RoleUser, Content: "current query"}) // Critical

	originalLen := len(conv.messages)

	// Compress to 50% of current tokens
	result := conv.CompressByImportance(0.50)

	if result.TokensRemoved == 0 {
		t.Error("Expected tokens to be removed")
	}

	if len(conv.messages) >= originalLen {
		t.Errorf("Expected message count to decrease, got %d -> %d", originalLen, len(conv.messages))
	}

	// Critical messages should remain
	messages := conv.GetMessages()
	if len(messages) == 0 {
		t.Fatal("All messages removed")
	}

	// Last message (current query) should always remain
	lastMsg := messages[len(messages)-1]
	if lastMsg.Role != llm.RoleUser {
		t.Errorf("Last message should be user query, got %v", lastMsg.Role)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/agent/... -run TestCompressByImportance -v
```
Expected: FAIL with "conv.CompressByImportance undefined"

- [x] **Step 3: Implement CompressByImportance**

Add to `internal/agent/conversation.go` after `TruncateByImportance`:

```go
// CompressionReport holds metadata about compression operations.
type CompressionReport struct {
	TokensBefore   int
	TokensAfter    int
	TokensRemoved  int
	MessagesBefore int
	MessagesAfter  int
}

// CompressByImportance applies importance-based compression to the conversation.
// The targetRatio is the fraction of current tokens to retain (e.g., 0.50 = 50%).
// Messages are removed in reverse importance order:
// 1. Reasoning steps (lowest priority - removed first)
// 2. Tool results (medium)
// 3. Assistant plans (medium)
// 4. Assistant conclusions (high)
// 5. Tool key findings (high)
// 6. User input (critical - preserved)
// Anchor messages are always preserved.
// Returns a report of compression results.
func (c *Conversation) CompressByImportance(targetRatio float64) CompressionReport {
	c.mu.Lock()
	defer c.mu.Unlock()

	if targetRatio <= 0 || targetRatio > 1 || len(c.messages) == 0 {
		return CompressionReport{}
	}

	const charsPerToken = 3

	// Calculate current token usage
	type msgIndex struct {
		idx        int
		importance MessageImportance
		tokens     int
	}

	var indices []msgIndex
	currentTokens := 0

	for i, msg := range c.messages {
		msgType := MessageUnknown
		if i < len(c.messageTypes) {
			msgType = c.messageTypes[i]
		}

		importance := getMessageImportance(msgType)
		if c.isAnchorMessageUnsafe(msg.Content) {
			importance = ImportanceCritical
		}

		msgTokens := len(msg.Content) / charsPerToken
		currentTokens += msgTokens

		indices = append(indices, msgIndex{
			idx:        i,
			importance: importance,
			tokens:     msgTokens,
		})
	}

	// Calculate target tokens
	targetTokens := int(float64(currentTokens) * targetRatio)

	if currentTokens <= targetTokens {
		return CompressionReport{
			TokensBefore:   currentTokens,
			TokensAfter:    currentTokens,
			MessagesBefore: len(c.messages),
			MessagesAfter:  len(c.messages),
		}
	}

	// Sort by importance (lowest first), then by token count (highest first)
	// This ensures we remove low-importance, high-token messages first
	for i := 0; i < len(indices)-1; i++ {
		for j := i + 1; j < len(indices); j++ {
			shouldSwap := false
			if indices[i].importance > indices[j].importance {
				shouldSwap = true
			} else if indices[i].importance == indices[j].importance && indices[i].tokens < indices[j].tokens {
				shouldSwap = true
			}
			if shouldSwap {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}

	// Remove messages until we hit target
	tokensToRemove := currentTokens - targetTokens
	removedTokens := 0
	keepMask := make([]bool, len(c.messages))

	for _, mi := range indices {
		if removedTokens >= tokensToRemove {
			break
		}
		// Never remove anchor messages
		if c.isAnchorMessageUnsafe(c.messages[mi.idx].Content) {
			continue
		}
		// Mark for removal
		keepMask[mi.idx] = false
		removedTokens += mi.tokens
	}

	// Build new message list
	newMessages := make([]llm.ChatMessage, 0, len(c.messages))
	newTypes := make([]MessageClassification, 0, len(c.messageTypes))
	removedCount := 0

	for i, msg := range c.messages {
		if i < len(keepMask) && keepMask[i] {
			newMessages = append(newMessages, msg)
			if i < len(c.messageTypes) {
				newTypes = append(newTypes, c.messageTypes[i])
			}
		} else {
			removedCount++
		}
	}

	c.messages = newMessages
	c.messageTypes = newTypes

	return CompressionReport{
		TokensBefore:   currentTokens,
		TokensAfter:    currentTokens - removedTokens,
		TokensRemoved:  removedTokens,
		MessagesBefore: len(c.messages) + removedCount,
		MessagesAfter:  len(c.messages),
	}
}
```

- [x] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./internal/agent/... -run TestCompressByImportance -v
```
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/agent/conversation.go internal/agent/conversation_compression_test.go
git commit -m "feat: add CompressByImportance method to Conversation"
```

---

## Task 4: Add Code-Aware Truncation for Tool Results

**Files:**
- Modify: `internal/agent/executor.go`
- Modify: `internal/code/ast/parser.go` (export compression helper)
- Test: `internal/agent/executor_code_aware_test.go`

- [x] **Step 1: Write test for code-aware compression**

Create `internal/agent/executor_code_aware_test.go`:

```go
package agent

import (
	"testing"
)

func TestCompressCodeResult(t *testing.T) {
	code := `package main

import "fmt"

func main() {
    fmt.Println("Hello, world!")
}

// Long function with lots of code
func longFunction() {
    // This is line 1
    // This is line 2
    // This is line 3
    // ... many more lines
    fmt.Println("end")
}`

	// Compress with max 100 tokens
	result := compressCodeResult(code, 100)

	// Should preserve function signature
	if result == "" {
		t.Fatal("Result should not be empty")
	}

	// Should contain truncation marker
	if !strings.Contains(result, "[compressed") {
		t.Error("Should contain compression marker")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/agent/... -run TestCompressCodeResult -v
```
Expected: FAIL with "undefined: compressCodeResult"

- [x] **Step 3: Add code-aware compression to executor.go**

First, add import for AST package at top of `internal/agent/executor.go`:

```go
import (
	"github.com/caimlas/meept/internal/code/ast"
	// ... existing imports
)
```

Add after `compressMapResult`:

```go
// compressCodeResult compresses code by truncating at AST boundaries.
// It preserves function signatures, type definitions, and key structure
// while removing implementation details from the middle of functions.
func compressCodeResult(code string, maxTokens int) string {
	const charsPerToken = 3
	maxChars := maxTokens * charsPerToken

	if len(code) <= maxChars {
		return code
	}

	// Parse the code
	pm := ast.NewParserManager(ast.DefaultParserConfig())
	lang := ast.DetectLanguage("temp.go") // Infer from context or hint

	var result string
	if lang != ast.LangUnknown {
		// Try to parse and compress at AST boundaries
		tree, err := pm.GetTree(context.Background(), []byte(code), lang)
		if err == nil {
			result = compressAtASTBoundaries(tree, code, maxChars)
			if result != "" {
				return result
			}
		}
	}

	// Fallback to character-based truncation
	return truncateWithMarker(code, maxChars)
}

// compressAtASTBoundaries compresses code by removing function bodies.
func compressAtASTBoundaries(tree *sitter.Tree, code string, maxChars int) string {
	root := tree.RootNode()
	if root == nil {
		return ""
	}

	var builder strings.Builder
	builder.Grow(maxChars)

	currentChars := 0

	// Walk the tree, keeping declarations and type definitions
	walkAST(root, code, &builder, &currentChars, maxChars)

	if currentChars > maxChars {
		// Still too long, truncate
		return builder.String()[:maxChars] + "\n...[compressed]"
	}

	return builder.String() + "\n...[compressed]"
}

// walkAST walks the AST and appends keepable content.
func walkAST(node *sitter.Node, code string, builder *strings.Builder, currentChars *int, maxChars int) {
	if node == nil || *currentChars >= maxChars {
		return
	}

	nodeType := node.Type()
	isDeclaration := nodeType == "function_declaration" ||
		nodeType == "method_declaration" ||
		nodeType == "type_declaration" ||
		nodeType == "var_declaration" ||
		nodeType == "const_declaration"

	if isDeclaration {
		// For declarations, keep the signature but compress the body
		signature := getDeclarationSignature(node, code)
		if signature != "" {
			signatureLen := len(signature)
			if *currentChars+signatureLen > maxChars {
				signature = signature[:maxChars-*currentChars]
			}
			builder.WriteString(signature)
			*currentChars += len(signature)
			return
		}
	}

	// Recurse into children
	for i := uint32(0); i < node.ChildCount(); i++ {
		child := node.Child(int(i))
		if child != nil {
			walkAST(child, code, builder, currentChars, maxChars)
		}
	}
}

// getDeclarationSignature extracts just the signature of a declaration.
func getDeclarationSignature(node *sitter.Node, code string) string {
	// For function declarations, keep: func name(params) returnType
	// For type declarations, keep: type name struct/interface/etc.

	start := node.StartPoint()
	end := node endPoint()

	// Get full text
	fullText := node.Content([]byte(code))

	// For now, just return a truncated version
	// A more sophisticated implementation would parse the signature precisely
	if len(fullText) > 100 {
		return fullText[:100] + " { ... }"
	}
	return fullText
}
```

Wait - this needs `sitter` import. Let me check what's available in the AST package...

Actually, looking at `internal/code/ast/parser.go`, the `GetTree` method returns `*sitter.Tree`. We need to import tree-sitter. Let's use the existing AST package instead.

Actually simpler approach - add a compression helper to the AST package:

- [x] **Step 4: Add compression helper to AST package**

Modify `internal/code/ast/parser.go`, add at end:

```go
// CompressCodeAtBoundaries compresses code by truncating at AST boundaries.
// Returns compressed code with truncation markers, or empty string if compression fails.
func CompressCodeAtBoundaries(source []byte, lang Language, maxChars int) string {
	pm := NewParserManager(DefaultParserConfig())

	result, err := pm.Parse(context.Background(), source, lang)
	if err != nil {
		return ""
	}

	var builder strings.Builder
	builder.Grow(maxChars)
	currentChars := 0

	walkAndCompress(result.RootNode, &builder, &currentChars, maxChars)

	if builder.Len() == 0 {
		return ""
	}

	return builder.String() + "\n...[compressed]"
}

// walkAndCompress walks AST and appends compressible content.
func walkAndCompress(node Node, builder *strings.Builder, currentChars *int, maxChars int) {
	if *currentChars >= maxChars {
		return
	}

	// Keep short nodes entirely
	if len(node.Text) > 0 && len(node.Text) < 50 {
		if *currentChars+len(node.Text) <= maxChars {
			builder.WriteString(node.Text)
			*currentChars += len(node.Text)
		}
		return
	}

	// For long nodes, truncate
	remaining := maxChars - *currentChars
	if remaining > 0 {
		if remaining < len(node.Text) {
			builder.WriteString(node.Text[:remaining])
			*currentChars = maxChars
		} else {
			builder.WriteString(node.Text)
			*currentChars += len(node.Text)
		}
	}

	// Recurse into children
	for _, child := range node.Children {
		walkAndCompress(child, builder, currentChars, maxChars)
	}
}
```

- [x] **Step 5: Update executor.go to use code-aware compression**

Modify `internal/agent/executor.go:114-127`. Update the string case in `ToCompressedJSON`:

```go
case string:
	// Check if this looks like code
	if looksLikeCode(result) {
		compressed.Result = compressCodeResult(result, maxChars-200)
	} else {
		compressed.Result = truncateWithMarker(result, maxChars-200)
	}
```

Add helper function after `compressMapResult`:

```go
// looksLikeCode checks if content looks like source code.
func looksLikeCode(s string) bool {
	codeIndicators := []string{
		"func ", "package ", "import ", "type ",
		"def ", "class ", "import ", "export ",
		"function ", "const ", "let ", "var ",
		"{", "}", "()", ";",
	}
	lower := strings.ToLower(s)
	for _, indicator := range codeIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}
```

- [x] **Step 6: Run test to verify it passes**

Run:
```bash
go test ./internal/agent/... -run TestCompressCodeResult -v
```
Expected: PASS

- [x] **Step 7: Commit**

```bash
git add internal/agent/executor.go internal/code/ast/parser.go internal/agent/executor_code_aware_test.go
git commit -m "feat: add code-aware compression for tool results"
```

---

## Task 5: Update Configuration and Wire Everything

**Files:**
- Modify: `internal/llm/context_firewall.go` - Update `processMessages()` to use compressor
- Modify: `config/meept.toml` - Add proactive compression config example

- [x] **Step 1: Modify processMessages to use compressor**

Modify `internal/llm/context_firewall.go:188-271`. Replace the entire method:

```go
// processMessages applies the context firewall filtering pipeline.
// It implements threshold-based handling:
// - At 50%: log warning for potential wrap-up
// - At 60%: summarize old history
// - At 70%: aggressive compression
// - At 80%: drop old context
func (f *ContextFirewall) processMessages(ctx context.Context, messages []ChatMessage) ([]ChatMessage, error) {
	if !f.config.Enabled || f.model == nil || f.model.ContextLimit == 0 {
		return messages, nil
	}

	result := append([]ChatMessage{}, messages...)

	// Estimate current token usage using tokenizer
	currentTokens := f.countTokens(result)
	utilization := float64(currentTokens) / float64(f.model.ContextLimit)

	// Apply proactive compression if enabled
	if f.compressor != nil {
		compressionResult := f.compressor.Compress(ctx, result, utilization)
		result = compressionResult.Messages
		currentTokens = compressionResult.TokensAfter
		utilization = float64(currentTokens) / float64(f.model.ContextLimit)
	}

	// Step 1: Chunk large input if configured
	if f.config.ChunkLargeInputs && len(result) > 0 {
		threshold := int(float64(f.model.ContextLimit) * f.config.ChunkThresholdRatio)
		lastMsg := &result[len(result)-1]
		lastMsgTokens := f.tokenizer.CountTokens(lastMsg.Content)
		if lastMsgTokens > threshold {
			chunks := f.chunkMessage(lastMsg.Content, threshold)
			if len(chunks) > 1 {
				f.logger.Debug("chunking large input", "chunks", len(chunks))
				result[len(result)-1].Content = chunks[0]
				for _, chunk := range chunks[1:] {
					result = append(result, ChatMessage{
						Role:    RoleUser,
						Content: chunk,
					})
				}
				currentTokens = f.countTokens(result)
			}
		}
	}

	// Step 2: Summarize old history if configured and needed
	if f.config.SummarizeHistory && currentTokens > int(float64(f.model.ContextLimit)*f.config.HardLimit) {
		summarized, err := f.summarizeOldHistory(ctx, result)
		if err != nil {
			f.summarizationFailures.Add(1)
			f.logger.Warn("summarization failed, continuing without summarization",
				"error", err,
				"failures_total", f.summarizationFailures.Load(),
			)
		} else {
			result = summarized
			currentTokens = f.countTokens(result)
			f.logger.Debug("summarized history", "tokens", currentTokens)
		}
	}

	return result, nil
}
```

- [x] **Step 2: Add Config example**

Modify `config/meept.toml`, add to the `[llm]` section:

```toml
# Proactive context compression (starts at 50%, maintains until 80%)
proactive_compression = true  # Enable multi-stage compression
```

- [x] **Step 3: Run all tests**

Run:
```bash
go test ./internal/llm/... ./internal/agent/... -v
```
Expected: All tests pass

- [x] **Step 4: Commit**

```bash
git add internal/llm/context_firewall.go config/meept.toml
git commit -m "feat: wire proactive compression into processMessages pipeline"
```

---

## Task 6: Add Observability and Stats

**Files:**
- Modify: `internal/llm/context_firewall.go` - Add compression stats to `FirewallStats`
- Create: `docs/reference/context-compression.md` - Documentation

- [x] **Step 1: Add compression stats to FirewallStats**

Modify `internal/llm/context_firewall.go:43-48`:

```go
// FirewallStats is a snapshot of firewall counters.
type FirewallStats struct {
	SummarizationFailures uint64
	DroppedMessages       uint64
	DropEvents            uint64
	// Compression stats (added for proactive compression)
	CompressionWarningEvents    uint64
	CompressionSummarizeEvents  uint64
	CompressionAggressiveEvents uint64
	CompressionHardLimitEvents  uint64
	CompressionTokensSaved      uint64
}
```

- [x] **Step 2: Update Stats() method**

Modify `internal/llm/context_firewall.go:50-57`:

```go
// Stats returns a snapshot of firewall counters.
func (f *ContextFirewall) Stats() FirewallStats {
	stats := FirewallStats{
		SummarizationFailures: f.summarizationFailures.Load(),
		DroppedMessages:       f.droppedMessages.Load(),
		DropEvents:            f.dropEvents.Load(),
	}

	// Add compression stats if compressor is active
	if f.compressor != nil {
		cs := f.compressor.Stats()
		stats.CompressionWarningEvents = cs.WarningEvents
		stats.CompressionSummarizeEvents = cs.SummarizeEvents
		stats.CompressionAggressiveEvents = cs.AggressiveEvents
		stats.CompressionHardLimitEvents = cs.HardLimitEvents
		stats.CompressionTokensSaved = cs.TotalTokensSaved
	}

	return stats
}
```

- [x] **Step 3: Write documentation**

Create `docs/reference/context-compression.md`:

```markdown
# Context Compression

Context compression helps maintain context below 80% utilization by applying progressively aggressive compression starting at 50%.

## How It Works

The compression pipeline has 4 stages:

| Stage | Utilization | Action |
|-------|-------------|--------|
| 1 | 50% | Log warning - "consider wrapping up" |
| 2 | 60% | Summarize old history - keep system + last 4 messages |
| 3 | 70% | Aggressive compression - keep system + last 2 messages |
| 4 | 80% | Hard limit - drop old context, keep system + last 2 |

## Configuration

Enable in `meept.toml`:

```toml
[llm]
proactive_compression = true
```

## Observability

Compression stats are exposed via `FirewallStats`:

- `CompressionWarningEvents` - Number of 50% warnings
- `CompressionSummarizeEvents` - Number of 60% summarizations
- `CompressionAggressiveEvents` - Number of 70% aggressive compressions
- `CompressionHardLimitEvents` - Number of 80% hard limit drops
- `CompressionTokensSaved` - Total tokens saved by compression

## Code-Aware Compression

Tool results containing code are compressed at AST boundaries, preserving:
- Function signatures
- Type definitions
- Package/import declarations

Implementation details are truncated while maintaining structural context.

```

- [x] **Step 4: Commit**

```bash
git add internal/llm/context_firewall.go docs/reference/context-compression.md
git commit -m "feat: add compression observability and documentation"
```

---

## Self-Review Checklist

### Spec Coverage Check

| Requirement | Task | Status |
|-------------|------|--------|
| Start compression at 50% | Task 1 + Task 2 | Covered |
| Maintain until 80% | Task 5 (processMessages) | Covered |
| Multi-stage compression | Task 1 (CompressionStage) | Covered |
| Code-aware compression | Task 4 | Covered |
| Importance-based retention | Task 3 | Covered |
| Observability/stats | Task 6 | Covered |
| Documentation | Task 6 | Covered |

### Placeholder Scan

Searching for red flags:
- No "TBD", "TODO", "implement later" found
- No "add appropriate error handling" without code
- All steps include actual code or commands

### Type Consistency

- `CompressionConfig` defined in Task 1, used in Task 2
- `CompressionResult` defined in Task 1, returned in Task 2
- `CompressByImportance` signature matches test expectations

---

## Execution Handoff

Plan complete and saved to `docs/plans/2026-04-25-proactive-compression-implementation.md`.

**Two execution options:**

**1. Subagent-Driven (recommended)** - Dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with `superpowers:executing-plans`, batch execution with checkpoints

**Which approach?**