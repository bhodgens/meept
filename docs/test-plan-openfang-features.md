# Test Plan: OpenFang Feature Adoption

## Overview

This test plan documents the testing strategy for 6 features adopted from OpenFang:
1. Vector Embeddings (`internal/memory/vector/`)
2. Taint Tracking (`internal/security/taint/`)
3. Anthropic Driver (`internal/llm/anthropic.go`)
4. Knowledge Graph Tools (`internal/tools/builtin/knowledge_graph.go`)
5. Scheduling Tools (`internal/tools/builtin/tool_schedule_*.go`)
6. Web Search Tool (`internal/tools/builtin/tool_web_search.go`)

**Status Summary:**

| Feature | Implemented | Unit Tests | Integration Tests |
|---------|-------------|------------|-------------------|
| Vector Embeddings | Yes | No | No |
| Taint Tracking | Yes | No | No |
| Anthropic Driver | Yes | No | No |
| Knowledge Graph Tools | Yes | No | No |
| Scheduling Tools | Yes | No | No |
| Web Search Tool | Yes | Yes (partial) | No |

### Test Environment Setup

```bash
# Required dependencies
go get github.com/mattn/go-sqlite3

# Environment variables for integration tests
export OPENAI_API_KEY="sk-test-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export OLLAMA_BASE_URL="http://localhost:11434"
```

---

## 1. Vector Embeddings Tests

**Location:** `internal/memory/vector/`

### 1.1 Unit Tests

#### `embedding_test.go`

**Test Cases:**
- `TestOpenAIProvider_GenerateEmbedding`: Single text embedding generation
  - Mock OpenAI API response with 1536-dimension vector
  - Verify dimension matches configuration
  - Error handling for API failures

- `TestOpenAIProvider_GenerateEmbeddings`: Batch embedding generation
  - Multiple texts in single request
  - Verify response count matches input count
  - Handle partial failures

- `TestOllamaProvider_GenerateEmbedding`: Local Ollama embeddings
  - Mock Ollama `/api/embeddings` response
  - Verify 768-dimension vector (nomic-embed-text default)

- `TestProviderFromConfig`: Provider factory
  - Test "openai" provider selection
  - Test "ollama" provider selection
  - Test unsupported provider returns error

- `TestSerializeDeserializeVector`: Vector storage
  - Verify round-trip conversion preserves values
  - Test edge cases: empty vector, single dimension

#### `store_test.go`

**Test Cases:**
- `TestNewStore`: Store initialization
  - Create in-memory SQLite database
  - Verify schema creation
  - Verify WAL mode enabled

- `TestStore_Store`: Embedding storage
  - Store embedding with metadata
  - Verify embedding cache is populated
  - Test REPLACE behavior for existing memory_id

- `TestStore_Search`: Vector similarity search
  - Insert known vectors with specific similarities
  - Query and verify ranking by cosine similarity
  - Test limit parameter

- `TestStore_GetEmbedding`: Cache behavior
  - Retrieve from cache after store
  - Retrieve from database after cache miss
  - Test Delete removes from cache and DB

- `TestCosineSimilarity`: Math correctness
  - Test identical vectors return 1.0
  - Test orthogonal vectors return 0.0
  - Test opposite vectors return -1.0

#### `hybrid_test.go`

**Test Cases:**
- `TestNewHybridSearcher`: Initialization validation
  - Verify nil vector store returns error
  - Verify nil memory manager returns error
  - Verify alpha clamping (0 to 1)

- `TestHybridSearcher_Search`: Score fusion
  - Mock keyword results with scores
  - Mock vector results with scores
  - Verify combined score formula: `(1-alpha)*keyword + alpha*vector`

- `TestHybridSearcher_Alpha`: Weight adjustment
  - Test SetAlpha clamps values
  - Verify GetAlpha returns current value

### 1.2 Integration Tests

#### `tests/integration/openfang/vector_test.go`

**Test Cases:**
- `TestVectorE2E_OpenAI`: End-to-end with real OpenAI API
  - Requires `OPENAI_API_KEY` env var
  - Skip if key not present
  - Store actual embeddings, verify retrieval

- `TestVectorE2E_Ollama`: End-to-end with local Ollama
  - Requires Ollama running on localhost:11434
  - Skip if connection fails

- `TestHybridE2E`: Combined search
  - Real FTS keyword search via memory.Manager
  - Real vector search
  - Verify result fusion

---

## 2. Taint Tracking Tests

**Location:** `internal/security/taint/`

### 2.1 Unit Tests

#### `taint_test.go`

**Test Cases:**
- `TestNewTaintedValue`: Value creation
  - Verify labels are deduplicated
  - Verify value and source stored correctly

- `TestTaintedValue_IsTainted`: Taint checking
  - Clean value returns false
  - Single label returns true
  - Multiple labels returns true

- `TestTaintedValue_HasLabel`: Label-specific check
  - Test matching label returns true
  - Test non-matching label returns false

- `TestTaintedValue_Merge`: Label union
  - Merge two values with different labels
  - Verify result has union of labels
  - Test idempotency

- `TestTaintedValue_Declassify`: Label removal
  - Remove existing label
  - Remove non-existent label (no-op)
  - Verify other labels preserved

- `TestTaintLabel_String`: Display names
  - Verify TaintNone displays as "none"
  - Verify custom labels display correctly

#### `tracker_test.go`

**Test Cases:**
- `TestNewTracker`: Initialization
  - Verify default logger is used if nil provided
  - Verify empty variables map

- `TestTracker_MarkUserInput`: Label application
  - Verify TaintUserInput label applied
  - Verify stored in variables map

- `TestTracker_MarkSecret`: Secret labeling
  - Verify TaintSecret label applied

- `TestTracker_MarkExternal`: External labeling
  - Verify TaintExternal label applied

- `TestTracker_StoreRetrieve`: Variable storage
  - Store tainted value
  - Retrieve and verify equality
  - Test non-existent key returns nil

- `TestTracker_Propagate`: Label propagation
  - Merge multiple values
  - Verify result has union of all labels
  - Test with nil values

- `TestTracker_CheckSink`: Sink validation
  - Test allowed label passes
  - Test blocked label returns violation
  - Verify violation details are correct

- `TestTracker_CheckShellCommand`: Pattern detection
  - Test suspicious patterns are caught
  - Test safe commands pass
  - Verify suspicious patterns list

- `TestTracker_CheckWebFetch`: Exfiltration detection
  - Test URL with secret patterns blocked
  - Test safe URLs pass

#### `patterns_test.go`

**Test Cases:**
- `TestDetectSuspiciousPatterns`: Pattern matching
  - Test each pattern in SuspiciousPatterns
  - Verify detection rate

- `TestCalculateEntropy`: Entropy calculation
  - Low entropy (regular text)
  - High entropy (API key, random data)

- `TestExtractURLs`: URL extraction
  - Extract HTTP/HTTPS URLs
  - Handle multiple URLs
  - Ignore non-URL text

### 2.2 Integration Tests

#### `tests/integration/openfang/taint_test.go`

**Test Cases:**
- `TestTaintE2E_ToolExecution`: Full taint flow
  - Mark user input as tainted
  - Execute tool with tainted input
  - Verify sink enforcement blocks if needed

- `TestTaintE2E_ShellExecution`: Shell command safety
  - Attempt shell command with tainted input
  - Verify violation logged
  - Verify command not executed

- `TestTaintE2E_Declassification`: Explicit declassification
  - Mark input as tainted
  - Apply sanitization
  - Call Declassify
  - Verify command now allowed

---

## 3. Anthropic Driver Tests

**Location:** `internal/llm/`

### 3.1 Unit Tests

#### `anthropic_test.go`

**Test Cases:**
- `TestNewAnthropicClient`: Client initialization
  - Verify default timeout (5 minutes)
  - Verify default logger

- `TestBuildRequest`: Request construction
  - System prompt extraction
  - Tool conversion to Anthropic format
  - Extended thinking configuration

- `TestBuildRequest_WithExtendedThinking`: Extended thinking mode
  - Verify `thinking: {type: "enabled"}` added
  - Verify budget_tokens optional

- `TestBuildRequest_WithTools`: Tool formatting
  - Verify tool schema marshaling
  - Verify tool_use content blocks

- `TestParseResponse`: Response parsing
  - Text content extraction
  - Tool call extraction
  - Thinking content extraction
  - Verify thinking prepended to content

- `TestAPIError_Retryable`: Retry logic
  - Verify 429 is retryable
  - Verify 5xx errors are retryable
  - Verify 4xx (except 429) not retryable

#### `sse_test.go` (new file)

**Test Cases:**
- `TestSSEScanner_Basic`: SSE parsing
  - Parse simple event stream
  - Verify event type and data extraction

- `TestSSEScanner_MultiLineData`: Multi-line data
  - Handle data split across multiple lines
  - Verify concatenation

- `TestSSEScanner_Ping`: Skip comments
  - Verify ":ping" lines skipped

### 3.2 Integration Tests

#### `tests/integration/openfang/anthropic_test.go`

**Test Cases:**
- `TestAnthropicE2E_SimpleChat`: Basic chat
  - Requires `ANTHROPIC_API_KEY`
  - Simple message exchange
  - Verify response content

- `TestAnthropicE2E_ExtendedThinking`: Thinking mode
  - Use claude-opus-4-5-20251101 (or model with extended_thinking capability)
  - Verify thinking block in response

- `TestAnthropicE2E_WithProgress`: Progress callbacks
  - Verify ProgressStageStarting fired
  - Verify ProgressStageThinking fired
  - Verify ProgressStageStreaming fired
  - Verify ProgressStageDone fired

- `TestAnthropicE2E_WithTools`: Tool calling
  - Provide tools in request
  - Verify tool_use blocks returned
  - Send tool results, verify final response

---

## 4. Knowledge Graph Tools Tests

**Location:** `internal/tools/builtin/knowledge_graph.go`

### 4.1 Unit Tests

#### `knowledge_graph_test.go`

**Test Cases:**
- `TestEntityCreateTool_NameParameters`: Tool metadata
  - Verify name is "entity_create"
  - Verify required parameters

- `TestEntityCreateTool_Execute_Success`: Successful creation
  - Mock graph.EnsureNode returns nil
  - Verify success response

- `TestEntityCreateTool_Execute_MissingParams`: Validation
  - Missing entity_id returns error
  - Missing entity_type returns error

- `TestEntityLinkTool_Execute`: Edge creation
  - Mock graph.AddEdge
  - Verify relation_type mapping
  - Verify weight defaulting

- `TestEntityQueryTool_Execute`: Query functionality
  - Mock graph.GetEdges
  - Verify filtering by relation_type
  - Verify limit applied

- `TestGraphStatsTool_Execute`: Statistics
  - Mock graph.GetStats
  - Verify stats returned

- `TestComputePageRankTool_Execute`: PageRank computation
  - Mock graph.ComputePageRank
  - Verify success response

- `TestDetectCommunitiesTool_Execute`: Community detection
  - Mock graph.DetectCommunities
  - Verify mapping included when requested

### 4.2 Integration Tests

#### `tests/integration/openfang/knowledge_graph_test.go`

**Test Cases:**
- `TestKnowledgeGraphE2E_CRUD`: Entity lifecycle
  - Create entity
  - Create another entity
  - Link them
  - Query relationships
  - Delete

- `TestKnowledgeGraphE2E_Pagerank`: Importance scoring
  - Create graph structure
  - Run compute_pagerank
  - Verify scores computed

- `TestKnowledgeGraphE2E_Communities`: Clustering
  - Create clustered graph
  - Run detect_communities
  - Verify community assignment

---

## 5. Scheduling Tools Tests

**Location:** `internal/tools/builtin/tool_schedule_*.go`

### 5.1 Unit Tests

#### `tool_schedule_create_test.go`

**Test Cases:**
- `TestScheduleCreateTool_Execute_AgentJob`: Agent job creation
  - Valid parameters
  - Verify prompt required
  - Verify job_id generated

- `TestScheduleCreateTool_Execute_ShellJob`: Shell job creation
  - Valid shell command
  - Verify command required
  - Verify working_dir optional

- `TestScheduleCreateTool_Execute_ReminderJob`: Reminder job creation
  - Valid reminder
  - Verify message required
  - Verify channels optional

- `TestScheduleCreateTool_Execute_Validation`: Parameter validation
  - Missing name
  - Invalid cron expression
  - Invalid job_type

#### `tool_schedule_list_test.go`

**Test Cases:**
- `TestScheduleListTool_Execute`: List jobs
  - Verify all jobs returned
  - Verify filter by agent_id
  - Verify filter by status

#### `tool_schedule_delete_test.go`

**Test Cases:**
- `TestScheduleDeleteTool_Execute`: Delete job
  - Delete existing job
  - Verify success
  - Test non-existent job

### 5.2 Integration Tests

#### `tests/integration/openfang/schedule_test.go`

**Test Cases:**
- `TestScheduleE2E_Lifecycle`: Job lifecycle
  - Create job
  - List to verify creation
  - Pause job
  - Resume job
  - Run immediately
  - Delete job

- `TestScheduleE2E_CronExecution`: Cron timing
  - Create job with "* * * * *" (every minute)
  - Wait for execution
  - Verify job ran

---

## 6. Web Search Tests

**Location:** `internal/tools/builtin/tool_web_search.go`

**Status:** Has unit tests in `tool_web_search_test.go`

### 6.1 Additional Unit Tests Needed

**Test Cases to Add:**
- `TestWebSearchTool_Execute_Success`: Successful search
  - Mock HTTP client with HTML response
  - Verify results parsed

- `TestWebSearchTool_Execute_APIError`: API error handling
  - Mock 500 response
  - Verify error returned

- `TestWebSearchTool_Execute_RateLimit`: Rate limiting
  - Verify minimum interval between requests
  - Test concurrent requests serialized

### 6.2 Integration Tests

#### `tests/integration/openfang/web_search_test.go`

**Test Cases:**
- `TestWebSearchE2E_RealSearch`: Real DuckDuckGo search
  - Perform actual search
  - Verify results contain valid URLs
  - Skip if no network

---

## Test Execution

```bash
# Run all tests
go test ./... -v

# Run specific package tests
go test ./internal/memory/vector/... -v
go test ./internal/security/taint/... -v
go test ./internal/llm/... -v
go test ./internal/tools/builtin/... -v

# Run integration tests only
go test ./tests/integration/openfang/... -v

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html

# Run tests with race detection
go test -race ./internal/memory/vector/...
go test -race ./internal/security/taint/...
```

### Coverage Goals

| Package | Target Coverage | Current |
|---------|-----------------|---------|
| `internal/memory/vector` | 80% | 0% |
| `internal/security/taint` | 80% | 0% |
| `internal/llm` (Anthropic) | 70% | 0% |
| `internal/tools/builtin` (KG) | 75% | 0% |
| `internal/tools/builtin` (Schedule) | 75% | 0% |
| `internal/tools/builtin` (WebSearch) | 80% | ~40% |

---

## Dependencies

### Testing Libraries
```go
// Use standard library testing package
import "testing"

// For test assertions (optional, can add later)
// import "github.com/stretchr/testify/assert"
// import "github.com/stretchr/testify/mock"
```

### Mock HTTP Server
```go
// Use httptest for API mocking
import "net/http/httptest"
```

---

## Notes

1. **Priority**: Start with unit tests before integration tests
2. **Mocking**: Use `httptest` for external API mocking
3. **Test Data**: Create test fixtures for known embeddings, vectors
4. **CI/CD**: Add test execution to CI pipeline
5. **Coverage**: Aim for 70%+ coverage before considering features "production-ready"
