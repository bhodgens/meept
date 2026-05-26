# Prefix Cache Hit Measurement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Thread provider-returned cache token counts through the entire stack — from LLM API response parsing, through the agent loop, into metrics storage and events — so operators can measure prefix cache hit rates.

**Architecture:** One new field (`CachedTokens`) added to `TokenUsage`, plus two Anthropic-specific fields (`CacheCreationTokens`, `CacheReadTokens`). The OpenAI-compatible client parses `prompt_tokens_details.cached_tokens`. The Anthropic client parses `cache_creation_input_tokens` and `cache_read_input_tokens`. The agent loop wires `StabilizeToolPrefix()` and emits real cache data in events. Metrics store gets a new column.

**Tech Stack:** Go 1.22+, SQLite (metrics), Anthropic Messages API, OpenAI Chat Completions API

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/llm/models.go:93-98` | Modify | Add cache fields to `TokenUsage` |
| `internal/llm/models.go:280-284` | Modify | Add cache fields to `ChatResponse.Usage` |
| `internal/llm/anthropic.go:431-434` | Modify | Add cache fields to `anthropicUsage` |
| `internal/llm/anthropic.go:885-933` | Modify | Populate cache fields in `buildResponseFromBlocks` |
| `internal/llm/anthropic.go:937-981` | Modify | Populate cache fields in `parseResponse` |
| `internal/llm/anthropic.go:614-657` | Modify | Populate cache fields in metrics record (non-streaming) |
| `internal/llm/anthropic.go:738-765` | Modify | Populate cache fields in metrics record (streaming) |
| `internal/llm/anthropic.go:813-816` | Modify | Capture cache fields from `message_start` event |
| `internal/llm/client.go:638-673` | Modify | Parse `prompt_tokens_details.cached_tokens` in `parseResponse` |
| `internal/llm/client.go:614-632` | Modify | Populate cache fields in metrics record |
| `internal/llm/metrics/store.go:31-43` | Modify | Add `CachedTokens` to `RequestRecord` |
| `internal/llm/metrics/store.go:131-145` | Modify | Add `cached_tokens` column to SQLite schema |
| `internal/llm/metrics/store.go:210-226` | Modify | Add `cached_tokens` to INSERT statement |
| `internal/agent/events.go:282-290` | Modify | Add `CachedTokens` to `AfterProviderResponseData` |
| `internal/agent/events.go:147-154` | Modify | Add `CachedTokens` to `TurnEndData` |
| `internal/agent/loop.go:2334-2343` | Modify | Populate `CachedTokens` in event emission |
| `internal/agent/loop.go:3891-3906` | Modify | Populate `CachedTokens` in turn end event |
| `internal/llm/models_test.go` | Modify | Tests for new TokenUsage fields |
| `internal/llm/anthropic_test.go` | Modify | Tests for Anthropic cache field parsing |
| `internal/llm/client_test.go` | Modify | Tests for OpenAI cache field parsing |
| `internal/llm/metrics/store_test.go` | Modify | Tests for cached_tokens column |

---

### Task 1: Add cache fields to TokenUsage and ChatResponse.Usage

**Files:**
- Modify: `internal/llm/models.go:93-98` (TokenUsage)
- Modify: `internal/llm/models.go:280-284` (ChatResponse.Usage)
- Test: `internal/llm/models_test.go`

- [ ] **Step 1: Write the failing test**

Add a test verifying the new fields exist and serialize correctly:

```go
func TestTokenUsage_CacheFields(t *testing.T) {
	u := TokenUsage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		TotalTokens:      1200,
		CachedTokens:     800,
	}
	if u.CachedTokens != 800 {
		t.Errorf("CachedTokens = %d, want 800", u.CachedTokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/... -run TestTokenUsage_CacheFields -v`
Expected: FAIL — `CachedTokens` field does not exist

- [ ] **Step 3: Add fields to TokenUsage**

In `internal/llm/models.go`, update the `TokenUsage` struct (line 93-98):

```go
// TokenUsage represents token usage counters returned by the API.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
}
```

- [ ] **Step 4: Add cache fields to ChatResponse.Usage**

In `internal/llm/models.go`, update the inline `Usage` struct inside `ChatResponse` (line 280-284). The OpenAI API returns cached tokens in `prompt_tokens_details`:

```go
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/llm/... -run TestTokenUsage_CacheFields -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/llm/models.go internal/llm/models_test.go
git commit -m "feat(llm): add CachedTokens field to TokenUsage and ChatResponse.Usage"
```

---

### Task 2: Parse Anthropic cache fields

**Files:**
- Modify: `internal/llm/anthropic.go:431-434` (anthropicUsage)
- Modify: `internal/llm/anthropic.go:813-816` (stream message_start)
- Modify: `internal/llm/anthropic.go:885-933` (buildResponseFromBlocks)
- Modify: `internal/llm/anthropic.go:937-981` (parseResponse)
- Test: `internal/llm/anthropic_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestAnthropicUsage_CacheFields(t *testing.T) {
	raw := `{"input_tokens": 100, "output_tokens": 50, "cache_creation_input_tokens": 200, "cache_read_input_tokens": 80}`
	var u anthropicUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.CacheCreationInputTokens != 200 {
		t.Errorf("CacheCreationInputTokens = %d, want 200", u.CacheCreationInputTokens)
	}
	if u.CacheReadInputTokens != 80 {
		t.Errorf("CacheReadInputTokens = %d, want 80", u.CacheReadInputTokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/... -run TestAnthropicUsage_CacheFields -v`
Expected: FAIL — fields do not exist

- [ ] **Step 3: Add cache fields to anthropicUsage**

In `internal/llm/anthropic.go`, update the `anthropicUsage` struct (line 431-434):

```go
type anthropicUsage struct {
	InputTokens                int `json:"input_tokens"`
	OutputTokens               int `json:"output_tokens"`
	CacheCreationInputTokens   int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens       int `json:"cache_read_input_tokens"`
}
```

- [ ] **Step 4: Populate cache fields in buildResponseFromBlocks**

In `internal/llm/anthropic.go`, update the `TokenUsage` construction in `buildResponseFromBlocks` (around line 926-930):

```go
		Usage: TokenUsage{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.InputTokens + usage.OutputTokens,
			CachedTokens:     usage.CacheReadInputTokens,
		},
```

- [ ] **Step 5: Populate cache fields in parseResponse**

In `internal/llm/anthropic.go`, update the `TokenUsage` construction in `parseResponse` (around line 973-977):

```go
		Usage: TokenUsage{
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			TotalTokens:      apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
			CachedTokens:     apiResp.Usage.CacheReadInputTokens,
		},
```

- [ ] **Step 6: Capture cache fields from message_start stream event**

In `internal/llm/anthropic.go`, update the `message_start` case in the streaming handler (around line 814-816). Currently it only captures `InputTokens`. Add cache fields:

```go
		case "message_start":
			if streamEvent.Message != nil && streamEvent.Message.Usage != nil {
				usage.InputTokens = streamEvent.Message.Usage.InputTokens
				usage.CacheCreationInputTokens = streamEvent.Message.Usage.CacheCreationInputTokens
				usage.CacheReadInputTokens = streamEvent.Message.Usage.CacheReadInputTokens
			}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/llm/... -run TestAnthropicUsage_CacheFields -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/llm/anthropic.go internal/llm/anthropic_test.go
git commit -m "feat(llm): parse Anthropic cache_creation_input_tokens and cache_read_input_tokens"
```

---

### Task 3: Parse OpenAI-compatible cache fields

**Files:**
- Modify: `internal/llm/client.go:638-673` (parseResponse)
- Test: `internal/llm/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestParseResponse_CachedTokens(t *testing.T) {
	raw := `{
		"id": "chatcmpl-1",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-4",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}],
		"usage": {
			"prompt_tokens": 1000,
			"completion_tokens": 10,
			"total_tokens": 1010,
			"prompt_tokens_details": {"cached_tokens": 800}
		}
	}`
	var chatResp ChatResponse
	if err := json.Unmarshal([]byte(raw), &chatResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if chatResp.Usage.PromptTokensDetails.CachedTokens != 800 {
		t.Errorf("CachedTokens = %d, want 800", chatResp.Usage.PromptTokensDetails.CachedTokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/... -run TestParseResponse_CachedTokens -v`
Expected: FAIL — `PromptTokensDetails` field does not exist (but it was added in Task 1 Step 4)

Note: If Task 1 was done correctly, this test should already PASS for the JSON parsing. The remaining work is in `parseResponse`.

- [ ] **Step 3: Populate CachedTokens in Client.parseResponse**

In `internal/llm/client.go`, update `parseResponse` (around line 665-669):

```go
	return &Response{
		Content:   content,
		ToolCalls: toolCalls,
		Usage: TokenUsage{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
			CachedTokens:     chatResp.Usage.PromptTokensDetails.CachedTokens,
		},
		Model:        model,
		FinishReason: choice.FinishReason,
	}, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/... -run TestParseResponse_CachedTokens -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/llm/client.go internal/llm/client_test.go
git commit -m "feat(llm): parse OpenAI prompt_tokens_details.cached_tokens into TokenUsage"
```

---

### Task 4: Wire cache metrics into metrics store

**Files:**
- Modify: `internal/llm/metrics/store.go:31-43` (RequestRecord)
- Modify: `internal/llm/metrics/store.go:131-145` (schema)
- Modify: `internal/llm/metrics/store.go:210-226` (INSERT)
- Modify: `internal/llm/client.go:614-632` (OpenAI metrics record)
- Modify: `internal/llm/anthropic.go:614-657` (Anthropic non-streaming metrics)
- Modify: `internal/llm/anthropic.go:738-765` (Anthropic streaming metrics)
- Test: `internal/llm/metrics/store_test.go`

- [ ] **Step 1: Write the failing test**

Add a test that inserts a record with CachedTokens and reads it back:

```go
func TestStore_RecordWithCachedTokens(t *testing.T) {
	// Uses existing test helper to create an in-memory store
	store, cleanup := testStore(t)
	defer cleanup()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	record := RequestRecord{
		Timestamp:        time.Now(),
		ProviderID:       "openai",
		ModelID:          "gpt-4",
		PromptTokens:     1000,
		CompletionTokens: 10,
		CachedTokens:     800,
		LatencyMs:        500,
		HTTPStatus:       200,
		Success:          true,
	}
	if err := store.Record(ctx, record); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Allow async processing
	time.Sleep(100 * time.Millisecond)

	// Verify via direct query
	stats, err := store.GetStats(ctx, "openai", "gpt-4", 1)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.RequestCount != 1 {
		t.Errorf("RequestCount = %d, want 1", stats.RequestCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/metrics/... -run TestStore_RecordWithCachedTokens -v`
Expected: FAIL — `CachedTokens` field does not exist on `RequestRecord`

- [ ] **Step 3: Add CachedTokens to RequestRecord**

In `internal/llm/metrics/store.go`, add a field to `RequestRecord` (after line 36):

```go
type RequestRecord struct {
	Timestamp        time.Time
	ProviderID       string
	ModelID          string
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	LatencyMs        int64
	TTFBMs           int64
	HTTPStatus       int
	ErrorType        ErrorType
	ErrorMessage     string
	Success          bool
}
```

- [ ] **Step 4: Add column to SQLite schema**

In `internal/llm/metrics/store.go`, add `cached_tokens` to the CREATE TABLE statement (after line 138):

```sql
CREATE TABLE IF NOT EXISTS provider_requests (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    ts                INTEGER NOT NULL,
    provider_id       TEXT NOT NULL,
    model_id          TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens     INTEGER NOT NULL DEFAULT 0,
    latency_ms        INTEGER NOT NULL DEFAULT 0,
    ttfb_ms           INTEGER NOT NULL DEFAULT 0,
    http_status       INTEGER NOT NULL DEFAULT 0,
    error_type        TEXT NOT NULL DEFAULT 'none',
    error_message     TEXT NOT NULL DEFAULT '',
    success           INTEGER NOT NULL DEFAULT 0
);
```

Also add a migration for existing databases — append after the schema DDL:

```sql
ALTER TABLE provider_requests ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0;
```

Wrap this in a `try/catch` pattern (SQLite will error if column already exists):

```go
// Migrate: add cached_tokens column if missing
db.ExecContext(ctx, "ALTER TABLE provider_requests ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0")
```

- [ ] **Step 5: Update INSERT statement**

In `internal/llm/metrics/store.go`, update `recordSync` (around line 212-222):

```go
const q = `
INSERT INTO provider_requests (ts, provider_id, model_id, prompt_tokens, completion_tokens, cached_tokens, latency_ms, ttfb_ms, http_status, error_type, error_message, success)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
ts := r.Timestamp.UnixMilli()
success := 0
if r.Success {
	success = 1
}
_, err := db.ExecContext(ctx, q, ts, r.ProviderID, r.ModelID, r.PromptTokens, r.CompletionTokens, r.CachedTokens, r.LatencyMs, r.TTFBMs, r.HTTPStatus, string(r.ErrorType), r.ErrorMessage, success)
```

- [ ] **Step 6: Populate CachedTokens in OpenAI client metrics**

In `internal/llm/client.go`, update the metrics record construction (around line 617-627):

```go
			record := metrics.RequestRecord{
				Timestamp:        time.Now(),
				ProviderID:       c.config.ProviderID,
				ModelID:          c.config.ModelID,
				PromptTokens:     chatResp.Usage.PromptTokens,
				CompletionTokens: chatResp.Usage.CompletionTokens,
				CachedTokens:     chatResp.Usage.PromptTokensDetails.CachedTokens,
				LatencyMs:        latencyMs,
				HTTPStatus:       resp.StatusCode,
				ErrorType:        metrics.ErrorTypeNone,
				Success:          true,
			}
```

- [ ] **Step 7: Populate CachedTokens in Anthropic non-streaming metrics**

In `internal/llm/anthropic.go`, update the non-streaming metrics record (around line 636-645). The Anthropic client records metrics **before** parsing the response body, so it doesn't have cache data at record time. Move the metrics recording to **after** response parsing, or defer the cache field population:

The simplest approach: record metrics after parsing (like the OpenAI client does). Restructure the non-streaming `chat()` method to parse the response first, then record metrics with actual usage:

```go
	// Parse response
	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		// ... existing error handling ...
	}

	// Record metrics with actual usage (including cache data)
	if c.metricsStore != nil {
		record := metrics.RequestRecord{
			Timestamp:        time.Now(),
			ProviderID:       c.config.ProviderID,
			ModelID:          c.config.ModelID,
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			CachedTokens:     apiResp.Usage.CacheReadInputTokens,
			LatencyMs:        latencyMs,
			HTTPStatus:       resp.StatusCode,
			ErrorType:        metrics.ErrorTypeNone,
			Success:          true,
		}
		store := c.metricsStore
		logger := c.logger
		go func() {
			if rerr := store.Record(context.Background(), record); rerr != nil {
				logger.Debug("metrics record failed", "error", rerr)
			}
		}()
	}
```

Remove the earlier pre-parse metrics recording block (lines 628-657).

- [ ] **Step 8: Populate CachedTokens in Anthropic streaming metrics**

In `internal/llm/anthropic.go`, the streaming metrics are also recorded before parsing. Move to after the stream completes, when `usage` is fully populated. Restructure similarly to step 7: move the metrics recording to after the stream parsing loop, using the final `usage` values:

```go
	// Record metrics after stream completes with actual usage
	if c.metricsStore != nil {
		record := metrics.RequestRecord{
			Timestamp:        time.Now(),
			ProviderID:       c.config.ProviderID,
			ModelID:          c.config.ModelID,
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			CachedTokens:     usage.CacheReadInputTokens,
			LatencyMs:        latencyMs,
			HTTPStatus:       resp.StatusCode,
			ErrorType:        metrics.ErrorTypeNone,
			Success:          true,
		}
		store := c.metricsStore
		logger := c.logger
		go func() {
			if rerr := store.Record(context.Background(), record); rerr != nil {
				logger.Debug("metrics record failed", "error", rerr)
			}
		}()
	}
```

Remove the earlier pre-stream metrics recording block (lines 738-765).

- [ ] **Step 9: Run test to verify it passes**

Run: `go test ./internal/llm/metrics/... -run TestStore_RecordWithCachedTokens -v`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/llm/metrics/store.go internal/llm/client.go internal/llm/anthropic.go internal/llm/metrics/store_test.go
git commit -m "feat(metrics): wire CachedTokens through metrics store and both LLM clients"
```

---

### Task 5: Wire cache data into agent events

**Files:**
- Modify: `internal/agent/events.go:282-290` (AfterProviderResponseData)
- Modify: `internal/agent/events.go:147-154` (TurnEndData)
- Modify: `internal/agent/loop.go:2334-2343` (event emission)
- Modify: `internal/agent/loop.go:3891-3906` (turn end event)

- [ ] **Step 1: Add CachedTokens to AfterProviderResponseData**

In `internal/agent/events.go`, add a field:

```go
type AfterProviderResponseData struct {
	ModelID        string        `json:"model_id"`
	StatusCode     int           `json:"status_code"`
	ResponseTokens int           `json:"response_tokens"`
	CachedTokens   int           `json:"cached_tokens"`
	Latency        time.Duration `json:"latency"`
	Cached         bool          `json:"cached"`
	Error          string        `json:"error,omitempty"`
}
```

- [ ] **Step 2: Add CachedTokens to TurnEndData**

In `internal/agent/events.go`, add a field:

```go
type TurnEndData struct {
	TurnNumber     int    `json:"turn_number"`
	HadToolCalls   bool   `json:"had_tool_calls"`
	ToolCallCount  int    `json:"tool_call_count"`
	ResponseTokens int    `json:"response_tokens"`
	CachedTokens   int    `json:"cached_tokens"`
	StoppedBy      string `json:"stopped_by"`
}
```

- [ ] **Step 3: Populate CachedTokens in AfterProviderResponseData emission**

In `internal/agent/loop.go`, update the event emission (around line 2339-2342):

```go
		l.emitSafeWithFields(ctx, AgentEvent{
			Type:           AgentEventAfterProviderResponse,
			ConversationID: conversationID,
			Iteration:      iteration,
			Data: AfterProviderResponseData{
				ModelID:        response.Model,
				ResponseTokens: response.Usage.TotalTokens,
				CachedTokens:   response.Usage.CachedTokens,
			},
		})
```

- [ ] **Step 4: Populate CachedTokens in TurnEndData emission**

In `internal/agent/loop.go`, update the turn end event emission (around line 3899-3905). The agent loop needs to track the last response's cached tokens. Add a local accumulator:

```go
		Data: TurnEndData{
			TurnNumber:     iteration,
			HadToolCalls:   hadToolCalls,
			ToolCallCount:  toolCallCount,
			ResponseTokens: responseTokens,
			CachedTokens:   cachedTokens,
			StoppedBy:      stoppedBy,
		},
```

Where `cachedTokens` is accumulated alongside `responseTokens` in the same loop.

- [ ] **Step 5: Build and verify**

Run: `go build ./internal/agent/... ./internal/llm/...`
Expected: clean build, no errors

- [ ] **Step 6: Commit**

```bash
git add internal/agent/events.go internal/agent/loop.go
git commit -m "feat(agent): emit CachedTokens in AfterProviderResponseData and TurnEndData events"
```

---

### Task 6: Wire StabilizeToolPrefix into agent loop

**Files:**
- Modify: `internal/agent/loop.go`

- [ ] **Step 1: Find where tool definitions are assembled for the LLM call**

Search `internal/agent/loop.go` for where tools are collected and passed to `chatWithFailover`. The `StabilizeToolPrefix` method on `Conversation` must be called before each LLM call with the tool definitions.

- [ ] **Step 2: Add StabilizeToolPrefix call before LLM calls**

Wherever tool definitions are assembled (before passing to `chatWithFailover` or the equivalent), add:

```go
	if l.conversation != nil {
		tools = l.conversation.StabilizeToolPrefix(tools)
		if l.conversation.PrefixChanged() {
			l.logger.Debug("prefix cache invalidated", "hash", l.conversation.GetCachePrefixHash())
		}
	}
```

Note: Read the current code to find the exact insertion point. The method signature is:
```go
func (c *Conversation) StabilizeToolPrefix(tools []llm.ToolDefinition) []llm.ToolDefinition
```

The tools must be `[]llm.ToolDefinition`. If the current code uses a different tool type, you may need to convert.

- [ ] **Step 3: Build and verify**

Run: `go build ./internal/agent/...`
Expected: clean build

- [ ] **Step 4: Commit**

```bash
git add internal/agent/loop.go
git commit -m "feat(agent): wire StabilizeToolPrefix before LLM calls for cache hit optimization"
```

---

### Task 7: Integration test — full build + existing tests

**Files:** None (verification only)

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 2: Run all LLM tests**

Run: `go test ./internal/llm/... -v`
Expected: all PASS (excluding pre-existing failures unrelated to this change)

- [ ] **Step 3: Run agent tests**

Run: `go test ./internal/agent/... -v`
Expected: all PASS (excluding pre-existing integration test failures)

- [ ] **Step 4: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address test failures from prefix cache metrics integration"
```
