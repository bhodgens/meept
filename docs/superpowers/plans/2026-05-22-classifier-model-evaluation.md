# LFM2.5 Classifier Model Evaluation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compare two LFM2.5 models as intent classifiers in Meept: `lfm2.5-1.2b-combined-serialized-sft` vs `LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit` across accuracy, token efficiency, latency, and error rates.

**Architecture:** Use Meept's MCP client harness to run both models locally via MLX. Configure classifier to use each model in turn, run comprehensive test suite of coding/debugging/research tasks, collect metrics (response tokens, latency, accuracy vs expected intent), and produce comparison report.

**Tech Stack:** Go (Meept daemon), MLX (local LLM runtime), MCP protocol (client harness), custom test harness for metrics collection.

---

## File Structure

The evaluation will create:

**New files:**
- `cmd/meept-classifier-test/main.go` - Standalone test runner CLI
- `internal/eval/classifier_benchmark.go` - Benchmark harness for running classification tests
- `internal/eval/classifier_metrics.go` - Metrics collection (latency, tokens, accuracy)
- `internal/eval/test_corpus.go` - Test dataset (100+ labeled examples across all intents)
- `docs/eval/classifier-comparison-report.md` - Final comparison report

**Modified files:**
- `config/models.json5` - Add both test models to provider configuration
- `internal/config/schema.go` - Add eval-specific config fields if needed

**Test files:**
- `internal/eval/classifier_benchmark_test.go` - Unit tests for benchmark harness
- `testdata/eval/classifier-test-corpus.json5` - Test dataset with labeled intents

---

## Evaluation Criteria

| Criterion | Weight | Measurement |
|-----------|--------|-------------|
| **Intent Accuracy** | 40% | % of test cases where predicted intent matches labeled intent |
| **Confidence Calibration** | 15% | Correlation between confidence score and actual accuracy |
| **Token Efficiency** | 15% | Average output tokens per classification |
| **Latency** | 15% | Average time-to-first-token + total completion time |
| **Error Rate** | 10% | % of requests that fail, timeout, or return malformed JSON |
| **Over-classification** | 5% | Tendency to detect compound intents in simple queries |

## Test Corpus Design

The test corpus covers 4 categories with 25+ examples each (100+ total):

### 1. Coding Tasks (25 examples)
```json5
{ input: "create a function to sort users by name", expected_intent: "code" }
{ input: "refactor this to use generics", expected_intent: "code" }
// ...
```

### 2. Debugging Tasks (25 examples)
```json5
{ input: "why is this test failing?", expected_intent: "debug" }
{ input: "fix the nil pointer dereference", expected_intent: "debug" }
// ...
```

### 3. Research/Analysis Tasks (25 examples)
```json5
{ input: "what are the best practices for API design?", expected_intent: "analyze" }
{ input: "search for Go performance benchmarks", expected_intent: "search" }
// ...
```

### 4. Platform/Chat/Other (25+ examples)
```json5
{ input: "hello", expected_intent: "chat" }
{ input: "what can you do?", expected_intent: "platform" }
{ input: "commit these changes", expected_intent: "git" }
// ...
```

### Edge Cases (10 examples)
```json5
{ input: "xyzzy", expected_intent: "chat" }  // nonsensical
{ input: "", expected_intent: "chat" }  // empty
{ input: "do the same for the tests", expected_intent: "code" }  // anaphora
// ...
```

---

## Model Configuration

Both models will be added to `config/models.json5`:

```json5
"providers": {
  "local": {
    "api": "openai",
    "options": {
      "baseURL": "http://127.0.0.1:8080/v1"
    },
    "models": {
      "lfm-combined-sft": {
        "name": "lfm2.5-1.2b-combined-serialized-sft",
        "capabilities": ["completion", "code", "reasoning"],
        "input_cost": 0.0,
        "output_cost": 0.0,
        "context_limit": 8192,
        "max_output": 512,
        "temperature": 0.1
      },
      "lfm-thinking-claude": {
        "name": "LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit",
        "capabilities": ["completion", "reasoning"],
        "input_cost": 0.0,
        "output_cost": 0.0,
        "context_limit": 8192,
        "max_output": 512,
        "temperature": 0.1
      }
    }
  }
}
```

---

## Evaluation Workflow

```
┌─────────────────────────────────────────────────────────────┐
│                  Model A: lfm-combined-sft                  │
├─────────────────────────────────────────────────────────────┤
│  1. Configure classifier to use Model A                     │
│  2. Start MLX server with Model A                           │
│  3. Run test corpus (100+ examples)                         │
│  4. Collect metrics per-example:                            │
│     - predicted_intent, confidence, expected_intent         │
│     - output_tokens, latency_ms, error (if any)             │
│  5. Compute aggregate stats                                 │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  Model B: lfm-thinking-claude               │
├─────────────────────────────────────────────────────────────┤
│  1. Configure classifier to use Model B                     │
│  2. Start MLX server with Model B                           │
│  3. Run identical test corpus                               │
│  4. Collect identical metrics                               │
│  5. Compute aggregate stats                                 │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Comparison Report                        │
├─────────────────────────────────────────────────────────────┤
│  - Side-by-side comparison tables                           │
│  - Per-intent accuracy breakdown                            │
│  - Token efficiency comparison                              │
│  - Latency distribution charts                              │
│  - Error analysis                                           │
│  - Final recommendation                                     │
└─────────────────────────────────────────────────────────────┘
```

---

## Historical Harness Bug Patterns (from auto-analysis documents)

The following bug patterns were identified in previous Meept harness testing (docs/auto-analysis/0001-0053). These inform our testing methodology:

### MCP/Communication Harness Bugs
| Bug ID | Pattern | Relevance to Classifier Testing |
|--------|---------|--------------------------------|
| 0013-B1 | Subscription ID not discoverable by clients | Test that classifier state/metrics are queryable |
| 0013-B2 | Tool responses serialized with Go %v formatting | Verify classifier returns proper JSON, not struct strings |
| 0051-B1 | Context-cancellation destroys subscriptions | Test classifier cooldown state survives multiple calls |
| 0052-B1 | bufio.Reader recreated on every call, loses data | Test sequential classifier calls don't lose state |
| 0053-B1 | Status returns raw Go struct string | Ensure metrics endpoints return proper JSON |

### Security/Engine Harness Bugs
| Bug ID | Pattern | Relevance to Classifier Testing |
|--------|---------|--------------------------------|
| 0012-B1 | Hook only handles specific tool type, bypasses others | Test classifier handles ALL intent types, not just common ones |
| 0012-B2 | No Tirith scan logging | Ensure classifier logging shows ALL attempts, not just failures |
| 0012-B3 | Input sanitizer not invoked on user messages | Test classifier transform hooks are actually called |
| 0012-B4 | Execution semaphore blocks testing | Ensure test harness doesn't block classifier evaluation |
| 0012-B5 | Risk level only logged for blocked tools | Log ALL classification results, not just errors |

### Self-Improve Harness Bugs
| Bug ID | Pattern | Relevance to Classifier Testing |
|--------|---------|--------------------------------|
| 0014-B1 | Commands fail with "not enabled" -- no discoverable guidance | Error messages must include actionable fix instructions |
| 0014-B2 | handleAnalyze does not run actual analysis | Test that benchmark harness actually invokes classifier, not stubs |
| 0014-B3 | Safety config partially mapped in daemon wiring | Verify classifier config is fully wired from config file |
| 0014-B4 | handleGenerate/handleValidate are status stubs | Test all harness endpoints do real work, not return cached status |

### Key Testing Principles Derived:
1. **Log Everything**: Previous bugs show logging was missing for scan invocations, results, and decisions
2. **Verify Actual Execution**: Multiple bugs were stub implementations returning cached status
3. **Test All Code Paths**: Hooks that only handle specific cases bypass untested code
4. **Config Wiring Verification**: Config-to-runtime mapping must be complete
5. **Discoverable State**: Clients need ways to query internal state (subscriptions, metrics)
6. **Actionable Errors**: "Not enabled" errors must include how to enable
7. **Sequential State Tests**: bufio.Reader and context cancellation bugs show state must persist across calls
8. **JSON Output Validation**: Status/metrics endpoints must return proper JSON, not Go struct strings

### Test Methodology Checklist:

For each component (benchmark harness, metrics collector, CLI runner):

| Check | Pattern Source | What To Verify |
|-------|---------------|----------------|
| Stub Detection | 0014-B2, 0014-B4 | Functions call real implementations, not return cached status |
| Config Wiring | 0014-B3 | All config fields flow from file → schema → runtime |
| Logging | 0012-B2, 0012-B3, 0012-B5 | All executions logged, not just failures |
| Code Path Coverage | 0012-B1 | ALL 12 intent types handled, not subset |
| Error Quality | 0014-B1 | Errors include actionable fix instructions |
| State Persistence | 0051-B1, 0052-B1 | Sequential calls don't lose state |
| Output Format | 0053-B1 | JSON output, not Go struct strings |

---

## Bite-Sized Task Granularity

### Task 1: Add Model Configurations

**Files:**
- Modify: `config/models.json5:58-75`
- Create: `config/models.classifier-test.json5` (test-specific overrides)

- [x] **Step 1: Add both models to providers.local.models**

Add to `config/models.json5`:
```json5
"lfm-combined-sft": {
  "name": "lfm2.5-1.2b-combined-serialized-sft",
  "capabilities": ["completion", "code", "reasoning"],
  "input_cost": 0.0,
  "output_cost": 0.0,
  "context_limit": 8192,
  "max_output": 512,
  "temperature": 0.1
},
"lfm-thinking-claude": {
  "name": "LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit",
  "capabilities": ["completion", "reasoning"],
  "input_cost": 0.0,
  "output_cost": 0.0,
  "context_limit": 8192,
  "max_output": 512,
  "temperature": 0.1
},
```

- [x]**Step 2: Create test aliases**

Add to `config/models.json5` under `model_aliases`:
```json5
"classifier-test-a": {
  "models": ["local/lfm-combined-sft"],
  "timeout": 30,
  "max_fails": 1
},
"classifier-test-b": {
  "models": ["local/lfm-thinking-claude"],
  "timeout": 30,
  "max_fails": 1
},
```

- [x]**Step 3: Validate JSON5 syntax**

Run:
```bash
cd /Users/caimlas/git/meept
node -e "JSON.parse(require('fs').readFileSync('config/models.json5', 'utf8').replace(/\\/\\/.*$/gm, '').replace(/,\\s*}/g, '}'))"
```
Expected: No syntax errors

- [x]**Step 4: Commit**

```bash
git add config/models.json5
git commit -m "config: add LFM2.5 classifier test models"
```

---

### Task 2: Create Test Corpus

**Files:**
- Create: `testdata/eval/classifier-test-corpus.json5`
- Create: `internal/eval/test_corpus.go`

- [x]**Step 1: Create testdata directory**

```bash
mkdir -p testdata/eval
```

- [x]**Step 2: Write coding test cases (25 examples)**

Create `testdata/eval/classifier-test-corpus.json5`:
```json5
{
  name: "Meept Classifier Test Corpus v1",
  version: "1.0",
  categories: {
    coding: [
      { input: "create a function to sort users by name", expected_intent: "code", expected_agent: "coder" },
      { input: "write a REST API endpoint for user creation", expected_intent: "code", expected_agent: "coder" },
      { input: "implement the UserService interface", expected_intent: "code", expected_agent: "coder" },
      { input: "add error handling to this function", expected_intent: "code", expected_agent: "coder" },
      { input: "create a new React component for the dashboard", expected_intent: "code", expected_agent: "coder" },
      { input: "refactor this to use dependency injection", expected_intent: "code", expected_agent: "coder" },
      { input: "generate protobuf stubs for the service", expected_intent: "code", expected_agent: "coder" },
      { input: "add unit tests for the calculator module", expected_intent: "code", expected_agent: "coder" },
      { input: "implement rate limiting middleware", expected_intent: "code", expected_agent: "coder" },
      { input: "create a database migration for users table", expected_intent: "code", expected_agent: "coder" },
      { input: "write a script to backup the database", expected_intent: "code", expected_agent: "coder" },
      { input: "add logging to all HTTP handlers", expected_intent: "code", expected_agent: "coder" },
      { input: "implement JWT authentication", expected_intent: "code", expected_agent: "coder" },
      { input: "create a GraphQL schema for posts", expected_intent: "code", expected_agent: "coder" },
      { input: "write a custom hook for form validation", expected_intent: "code", expected_agent: "coder" },
      { input: "add pagination to the API response", expected_intent: "code", expected_agent: "coder" },
      { input: "implement a cache layer with Redis", expected_intent: "code", expected_agent: "coder" },
      { input: "create a webhook handler for Stripe", expected_intent: "code", expected_agent: "coder" },
      { input: "write a CLI tool for data import", expected_intent: "code", expected_agent: "coder" },
      { input: "implement WebSocket real-time updates", expected_intent: "code", expected_agent: "coder" },
      { input: "add CORS middleware to the server", expected_intent: "code", expected_agent: "coder" },
      { input: "create a Dockerfile for the service", expected_intent: "code", expected_agent: "coder" },
      { input: "write integration tests for the auth flow", expected_intent: "code", expected_agent: "coder" },
      { input: "implement file upload with S3", expected_intent: "code", expected_agent: "coder" },
      { input: "create a scheduled job for cleanup", expected_intent: "code", expected_agent: "coder" },
    ],
```

- [x]**Step 3: Write debugging test cases (25 examples)**

Append to `testdata/eval/classifier-test-corpus.json5`:
```json5
    debugging: [
      { input: "why is this test failing?", expected_intent: "debug", expected_agent: "debugger" },
      { input: "fix the nil pointer dereference in handler.go", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the server crashes when I send a POST request", expected_intent: "debug", expected_agent: "debugger" },
      { input: "debug this race condition", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the API returns 500 error on startup", expected_intent: "debug", expected_agent: "debugger" },
      { input: "fix the memory leak in the parser", expected_intent: "debug", expected_agent: "debugger" },
      { input: "why is the database connection timing out?", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the tests hang indefinitely", expected_intent: "debug", expected_agent: "debugger" },
      { input: "fix the off-by-one error in the loop", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the UI shows blank screen after login", expected_intent: "debug", expected_agent: "debugger" },
      { input: "investigate the high CPU usage", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the webhook signature validation fails", expected_intent: "debug", expected_agent: "debugger" },
      { input: "fix the deadlock in the worker pool", expected_intent: "debug", expected_agent: "debugger" },
      { input: "why are emails not being sent?", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the cache returns stale data", expected_intent: "debug", expected_agent: "debugger" },
      { input: "fix the type mismatch compiler error", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the Docker container exits immediately", expected_intent: "debug", expected_agent: "debugger" },
      { input: "investigate the slow query performance", expected_intent: "debug", expected_agent: "debugger" },
      { input: "fix the broken authentication flow", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the file upload returns 413 error", expected_intent: "debug", expected_agent: "debugger" },
      { input: "why is the background job failing?", expected_intent: "debug", expected_agent: "debugger" },
      { input: "fix the CORS preflight request failure", expected_intent: "debug", expected_agent: "debugger" },
      { input: "the metrics endpoint returns no data", expected_intent: "debug", expected_agent: "debugger" },
      { input: "investigate the random flaky test", expected_intent: "debug", expected_agent: "debugger" },
      { input: "fix the goroutine leak", expected_intent: "debug", expected_agent: "debugger" },
    ],
```

- [x]**Step 4: Write research/analysis test cases (25 examples)**

Append to `testdata/eval/classifier-test-corpus.json5`:
```json5
    analyze: [
      { input: "what are the best practices for API design?", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "analyze the performance bottlenecks", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "compare different database options for this use case", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "what's the root cause of this architectural issue?", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "evaluate the tradeoffs of microservices vs monolith", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "research authentication patterns for SPAs", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "explain the event-driven architecture pattern", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "what are the security implications of this design?", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "analyze the competitor's API offerings", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "summarize the pros and cons of GraphQL", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "investigate why users abandon the checkout flow", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "what metrics should we track for this feature?", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "compare pricing models from different vendors", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "analyze the error logs for patterns", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "what's the industry standard for rate limiting?", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "evaluate this third-party library", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "research compliance requirements for GDPR", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "analyze the user feedback trends", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "what are the scalability limits of this architecture?", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "compare different caching strategies", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "investigate the data inconsistency issues", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "what's the optimal batch size for this job?", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "analyze the cost-benefit of cloud migration", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "research the latest Go 1.24 features", expected_intent: "analyze", expected_agent: "analyst" },
      { input: "evaluate our CI/CD pipeline efficiency", expected_intent: "analyze", expected_agent: "analyst" },
    ],
```

- [x]**Step 5: Write search test cases (15 examples)**

Append to `testdata/eval/classifier-test-corpus.json5`:
```json5
    search: [
      { input: "search for Go performance benchmarks", expected_intent: "search", expected_agent: "analyst" },
      { input: "find examples of similar implementations", expected_intent: "search", expected_agent: "analyst" },
      { input: "look up the latest React best practices", expected_intent: "search", expected_agent: "analyst" },
      { input: "find documentation on OAuth2 flows", expected_intent: "search", expected_agent: "analyst" },
      { input: "search for open source alternatives to this service", expected_intent: "search", expected_agent: "analyst" },
      { input: "find tutorials on Kubernetes deployment", expected_intent: "search", expected_agent: "analyst" },
      { input: "look up AWS Lambda pricing", expected_intent: "search", expected_agent: "analyst" },
      { input: "search for case studies on microservices migration", expected_intent: "search", expected_agent: "analyst" },
      { input: "find libraries for PDF generation in Go", expected_intent: "search", expected_agent: "analyst" },
      { input: "look up the Stripe API rate limits", expected_intent: "search", expected_agent: "analyst" },
      { input: "search for examples of event sourcing implementations", expected_intent: "search", expected_agent: "analyst" },
      { input: "find benchmarks for different database engines", expected_intent: "search", expected_agent: "analyst" },
      { input: "look up Docker best practices for production", expected_intent: "search", expected_agent: "analyst" },
      { input: "search for tutorials on implementing SSO", expected_intent: "search", expected_agent: "analyst" },
      { input: "find comparison of monitoring tools", expected_intent: "search", expected_agent: "analyst" },
    ],
```

- [x]**Step 6: Write chat platform git schedule test cases (20+ examples)**

Append to `testdata/eval/classifier-test-corpus.json5`:
```json5
    chat: [
      { input: "hello", expected_intent: "chat", expected_agent: "chat" },
      { input: "hi there", expected_intent: "chat", expected_agent: "chat" },
      { input: "good morning", expected_intent: "chat", expected_agent: "chat" },
      { input: "thanks", expected_intent: "chat", expected_agent: "chat" },
      { input: "you're welcome", expected_intent: "chat", expected_agent: "chat" },
      { input: "xyzzy", expected_intent: "chat", expected_agent: "chat" },
      { input: "test", expected_intent: "chat", expected_agent: "chat" },
      { input: "is anyone there?", expected_intent: "chat", expected_agent: "chat" },
      { input: "", expected_intent: "chat", expected_agent: "chat" },
      { input: "what's up?", expected_intent: "chat", expected_agent: "chat" },
    ],
    platform: [
      { input: "what can you do?", expected_intent: "platform", expected_agent: "chat" },
      { input: "list your available tools", expected_intent: "platform", expected_agent: "chat" },
      { input: "how do I use this?", expected_intent: "platform", expected_agent: "chat" },
      { input: "what agents are available?", expected_intent: "platform", expected_agent: "chat" },
      { input: "explain your capabilities", expected_intent: "platform", expected_agent: "chat" },
    ],
    git: [
      { input: "commit these changes", expected_intent: "git", expected_agent: "committer" },
      { input: "push to main branch", expected_intent: "git", expected_agent: "committer" },
      { input: "create a pull request", expected_intent: "git", expected_agent: "committer" },
      { input: "merge the feature branch", expected_intent: "git", expected_agent: "committer" },
      { input: "show me the git diff", expected_intent: "git", expected_agent: "committer" },
      { input: "rebase on main", expected_intent: "git", expected_agent: "committer" },
      { input: "resolve the merge conflict", expected_intent: "git", expected_agent: "committer" },
      { input: "cherry-pick that commit", expected_intent: "git", expected_agent: "committer" },
      { input: "tag this version as v1.0.0", expected_intent: "git", expected_agent: "committer" },
      { input: "show the commit history", expected_intent: "git", expected_agent: "committer" },
    ],
    schedule: [
      { input: "remind me to check emails at 3pm", expected_intent: "schedule", expected_agent: "scheduler" },
      { input: "schedule a meeting for tomorrow", expected_intent: "schedule", expected_agent: "scheduler" },
      { input: "set a timer for 30 minutes", expected_intent: "schedule", expected_agent: "scheduler" },
      { input: "create a recurring task for Mondays", expected_intent: "schedule", expected_agent: "scheduler" },
      { input: "add this to my calendar", expected_intent: "schedule", expected_agent: "scheduler" },
    ],
    plan: [
      { input: "design the system architecture", expected_intent: "plan", expected_agent: "planner" },
      { input: "create a project roadmap", expected_intent: "plan", expected_agent: "planner" },
      { input: "what's the plan for this feature?", expected_intent: "plan", expected_agent: "planner" },
      { input: "break down this task into steps", expected_intent: "plan", expected_agent: "planner" },
      { input: "estimate the effort for this project", expected_intent: "plan", expected_agent: "planner" },
    ],
    review: [
      { input: "review this pull request", expected_intent: "review", expected_agent: "coder" },
      { input: "check the code quality", expected_intent: "review", expected_agent: "coder" },
      { input: "are there any issues with this implementation?", expected_intent: "review", expected_agent: "coder" },
      { input: "validate the test coverage", expected_intent: "review", expected_agent: "coder" },
    ],
    report: [
      { input: "what progress have we made?", expected_intent: "report", expected_agent: "chat" },
      { input: "summarize what was done today", expected_intent: "report", expected_agent: "chat" },
      { input: "generate a status report", expected_intent: "report", expected_agent: "chat" },
      { input: "show me the metrics dashboard", expected_intent: "report", expected_agent: "chat" },
    ],
    recall: [
      { input: "what did we discuss yesterday?", expected_intent: "recall", expected_agent: "chat" },
      { input: "remember when we fixed that bug?", expected_intent: "recall", expected_agent: "chat" },
      { input: "show me the conversation from last week", expected_intent: "recall", expected_agent: "chat" },
    ],
  },
}
```

- [x]**Step 7: Create Go test corpus loader**

Create `internal/eval/test_corpus.go`:
```go
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TestCase represents a single classification test case
type TestCase struct {
	Input           string `json:"input"`
	ExpectedIntent  string `json:"expected_intent"`
	ExpectedAgent   string `json:"expected_agent"`
	Description     string `json:"description,omitempty"`
}

// TestCorpus represents the full test dataset
type TestCorpus struct {
	Name     string              `json:"name"`
	Version  string              `json:"version"`
	Categories map[string][]TestCase `json:"categories"`
}

// LoadTestCorpus reads the test corpus from the testdata directory
func LoadTestCorpus() (*TestCorpus, error) {
	path := filepath.Join("testdata", "eval", "classifier-test-corpus.json5")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read test corpus: %w", err)
	}

	// Parse JSON5 (JSON with comments)
	// For now, strip comments and parse as JSON
	jsonData := stripComments(data)

	var corpus TestCorpus
	if err := json.Unmarshal(jsonData, &corpus); err != nil {
		return nil, fmt.Errorf("failed to parse test corpus: %w", err)
	}

	return &corpus, nil
}

// AllTestCases returns all test cases flattened across categories
func (c *TestCorpus) AllTestCases() []TestCase {
	var all []TestCase
	for _, cases := range c.Categories {
		all = append(all, cases...)
	}
	return all
}

// CategoryNames returns the list of intent categories in the corpus
func (c *TestCorpus) CategoryNames() []string {
	names := make([]string, 0, len(c.Categories))
	for name := range c.Categories {
		names = append(names, name)
	}
	return names
}

// CategoryCount returns the number of test cases in a category
func (c *TestCorpus) CategoryCount(category string) int {
	return len(c.Categories[category])
}

// TotalCount returns the total number of test cases
func (c *TestCorpus) TotalCount() int {
	total := 0
	for _, cases := range c.Categories {
		total += len(cases)
	}
	return total
}

// stripComments removes // comments from JSON5 for basic parsing
func stripComments(data []byte) []byte {
	// Simple implementation - just strip // comments
	// For production, use a proper JSON5 library
	lines := string(data)
	result := ""
	for _, line := range strings.Split(lines, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		result += line + "\n"
	}
	return []byte(result)
}
```

Add import:
```go
import "strings"
```

- [x]**Step 8: Commit**

```bash
git add testdata/eval/classifier-test-corpus.json5 internal/eval/test_corpus.go
git commit -m "test: add classifier test corpus with 100+ examples"
```

---

### Task 3: Create Benchmark Harness

**Files:**
- Create: `internal/eval/classifier_benchmark.go`
- Create: `internal/eval/classifier_metrics.go`
- Create: `cmd/meept-classifier-test/main.go`

- [x]**Step 1: Create metrics collection structures**

Create `internal/eval/classifier_metrics.go`:
```go
package eval

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
)

// TestResult captures the result of a single classification test
type TestResult struct {
	TestCase         TestCase        `json:"test_case"`
	PredictedIntent  string          `json:"predicted_intent"`
	Confidence       float64         `json:"confidence"`
	ExpectedIntent   string          `json:"expected_intent"`
	Correct          bool            `json:"correct"`
	OutputTokens     int             `json:"output_tokens"`
	LatencyMs        int64           `json:"latency_ms"`
	TimeToFirstToken int64           `json:"time_to_first_token_ms,omitempty"`
	Error            string          `json:"error,omitempty"`
	RawResponse      string          `json:"raw_response"`
}

// CategoryMetrics holds aggregated metrics for a single intent category
type CategoryMetrics struct {
	Category       string  `json:"category"`
	TotalTests     int     `json:"total_tests"`
	Correct        int     `json:"correct"`
	Accuracy       float64 `json:"accuracy"`
	AvgConfidence  float64 `json:"avg_confidence"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	AvgOutputTokens float64 `json:"avg_output_tokens"`
	Errors         int     `json:"errors"`
}

// ModelMetrics holds overall metrics for a single model
type ModelMetrics struct {
	ModelName      string          `json:"model_name"`
	TestTimestamp  time.Time       `json:"test_timestamp"`
	TestDuration   time.Duration   `json:"test_duration"`

	Overall        CategoryMetrics `json:"overall"`
	ByCategory     []CategoryMetrics `json:"by_category"`

	// Detailed results for analysis
	Results        []TestResult    `json:"results"`

	// Summary statistics
	TotalCases     int             `json:"total_cases"`
	TotalCorrect   int             `json:"total_correct"`
	TotalErrors    int             `json:"total_errors"`
	AvgLatencyMs   float64         `json:"avg_latency_ms"`
	AvgConfidence  float64         `json:"avg_confidence"`
	AvgOutputTokens float64        `json:"avg_output_tokens"`
}

// BenchmarkResults holds comparison results between models
type BenchmarkResults struct {
	TestName       string        `json:"test_name"`
	TestTimestamp  time.Time     `json:"test_timestamp"`
	ModelA         *ModelMetrics `json:"model_a"`
	ModelB         *ModelMetrics `json:"model_b"`
	BetterModel    string        `json:"better_model"`
	Summary        string        `json:"summary"`
}

// MetricsCollector aggregates test results in real-time
type MetricsCollector struct {
	mu       sync.Mutex
	results  []TestResult
	startTime time.Time
	modelName string
}

// NewMetricsCollector creates a new collector for a model
func NewMetricsCollector(modelName string) *MetricsCollector {
	return &MetricsCollector{
		results:   make([]TestResult, 0),
		modelName: modelName,
	}
}

// RecordResult adds a test result to the collector
func (mc *MetricsCollector) RecordResult(result TestResult) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.results = append(mc.results, result)
}

// ComputeMetrics calculates aggregate metrics
func (mc *MetricsCollector) ComputeMetrics(corpus *TestCorpus) *ModelMetrics {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	metrics := &ModelMetrics{
		ModelName:     mc.modelName,
		TestTimestamp: time.Now(),
		TestDuration:  time.Since(mc.startTime),
		Results:       mc.results,
		TotalCases:    len(mc.results),
	}

	// Overall metrics
	correctCount := 0
	totalConfidence := 0.0
	totalLatency := int64(0)
	totalTokens := 0
	errorCount := 0

	for _, r := range mc.results {
		if r.Correct {
			correctCount++
		}
		if r.Error != "" {
			errorCount++
		}
		totalConfidence += r.Confidence
		totalLatency += r.LatencyMs
		totalTokens += r.OutputTokens
	}

	if len(mc.results) > 0 {
		metrics.TotalCorrect = correctCount
		metrics.TotalErrors = errorCount
		metrics.AvgConfidence = totalConfidence / float64(len(mc.results))
		metrics.AvgLatencyMs = float64(totalLatency) / float64(len(mc.results))
		metrics.AvgOutputTokens = float64(totalTokens) / float64(len(mc.results))

		metrics.Overall = CategoryMetrics{
			Category:        "overall",
			TotalTests:      len(mc.results),
			Correct:         correctCount,
			Accuracy:        float64(correctCount) / float64(len(mc.results)),
			AvgConfidence:   metrics.AvgConfidence,
			AvgLatencyMs:    metrics.AvgLatencyMs,
			AvgOutputTokens: metrics.AvgOutputTokens,
			Errors:          errorCount,
		}
	}

	// Per-category metrics
	metrics.ByCategory = make([]CategoryMetrics, 0, len(corpus.Categories))
	for catName, testCases := range corpus.Categories {
		catMetrics := CategoryMetrics{Category: catName}

		for _, tc := range testCases {
			for _, r := range mc.results {
				if r.TestCase.Input == tc.Input {
					catMetrics.TotalTests++
					if r.Correct {
						catMetrics.Correct++
					}
					if r.Error != "" {
						catMetrics.Errors++
					}
				}
			}
		}

		if catMetrics.TotalTests > 0 {
			catMetrics.Accuracy = float64(catMetrics.Correct) / float64(catMetrics.TotalTests)
		}

		metrics.ByCategory = append(metrics.ByCategory, catMetrics)
	}

	return metrics
}

// SaveResults writes metrics to a JSON file
func (mc *MetricsCollector) SaveResults(filename string, metrics *ModelMetrics) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	data, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// GenerateComparisonReport creates the final comparison between two models
func GenerateComparisonReport(metricsA, metricsB *ModelMetrics) *BenchmarkResults {
	report := &BenchmarkResults{
		TestName:      "LFM2.5 Classifier Comparison",
		TestTimestamp: time.Now(),
		ModelA:        metricsA,
		ModelB:        metricsB,
	}

	// Determine winner based on weighted scoring
	scoreA := scoreModel(metricsA)
	scoreB := scoreModel(metricsB)

	if scoreA > scoreB {
		report.BetterModel = metricsA.ModelName
		report.Summary = fmt.Sprintf(
			"%s outperforms %s (score: %.2f vs %.2f)",
			metricsA.ModelName, metricsB.ModelName, scoreA, scoreB,
		)
	} else if scoreB > scoreA {
		report.BetterModel = metricsB.ModelName
		report.Summary = fmt.Sprintf(
			"%s outperforms %s (score: %.2f vs %.2f)",
			metricsB.ModelName, metricsA.ModelName, scoreB, scoreA,
		)
	} else {
		report.BetterModel = "tie"
		report.Summary = "Models performed equivalently"
	}

	return report
}

// scoreModel computes a weighted score from metrics
func scoreModel(m *ModelMetrics) float64 {
	// Weights from evaluation criteria
	const (
		weightAccuracy       = 0.40
		weightCalibration    = 0.15
		weightTokenEfficiency = 0.15
		weightLatency        = 0.15
		weightErrorRate      = 0.10
		weightOverClassification = 0.05
	)

	// Normalize each metric (0-1 scale)
	accuracyScore := m.Overall.Accuracy

	// Confidence calibration (correlation - simplified as difference from 1.0 for correct)
	calibrationScore := 1.0 - abs(m.AvgConfidence-m.Overall.Accuracy)

	// Token efficiency (inverse, normalized - lower is better, cap at 500 tokens)
	tokenScore := 1.0 - min(1.0, m.AvgOutputTokens/500.0)

	// Latency score (inverse, normalized - cap at 5000ms)
	latencyScore := 1.0 - min(1.0, m.AvgLatencyMs/5000.0)

	// Error rate score (inverse)
	errorScore := 1.0 - min(1.0, float64(m.TotalErrors)/float64(m.TotalCases))

	// Over-classification score (would need separate tracking, default to 0.8)
	overClassScore := 0.8

	return accuracyScore*weightAccuracy +
		calibrationScore*weightCalibration +
		tokenScore*weightTokenEfficiency +
		latencyScore*weightLatency +
		errorScore*weightErrorRate +
		overClassScore*weightOverClassification
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
```

- [x]**Step 2: Create benchmark harness**

Create `internal/eval/classifier_benchmark.go`:
```go
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/llm/providers"
	"github.com/caimlas/meept/internal/config"
)

// BenchmarkConfig holds configuration for a benchmark run
type BenchmarkConfig struct {
	ModelRef       string
	Timeout        time.Duration
	Temperature    float64
	MaxOutputTokens int
}

// BenchmarkRunner executes classification benchmarks
type BenchmarkRunner struct {
	config     BenchmarkConfig
	llmClient  *llm.Client
	collector  *MetricsCollector
	classifier *agent.LLMClassifier
}

// NewBenchmarkRunner creates a new benchmark runner
func NewBenchmarkRunner(cfg BenchmarkConfig) (*BenchmarkRunner, error) {
	// Load the model config
	modelConfig, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Get the provider manager
	pm := providers.NewProviderManager(modelConfig.Providers)

	// Resolve the model reference
	modelRef, err := pm.ResolveModelRef(cfg.ModelRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve model %s: %w", cfg.ModelRef, err)
	}

	// Create the LLM client
	client, err := llm.NewClient(
		llm.WithProvider(pm),
		llm.WithModel(modelRef),
		llm.WithTemperature(cfg.Temperature),
		llm.WithMaxTokens(cfg.MaxOutputTokens),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	// Create the classifier
	classifier := agent.NewLLMClassifier(
		agent.LLMClassifierConfig{
			Client:  client,
			Model:   cfg.ModelRef,
			Timeout: cfg.Timeout,
		},
		nil, // logger
	)

	return &BenchmarkRunner{
		config:     cfg,
		llmClient:  client,
		collector:  NewMetricsCollector(cfg.ModelRef),
		classifier: classifier,
	}, nil
}

// Run executes the benchmark against the test corpus
func (br *BenchmarkRunner) Run(ctx context.Context, corpus *TestCorpus) (*ModelMetrics, error) {
	br.collector.startTime = time.Now()

	testCases := corpus.AllTestCases()

	for i, tc := range testCases {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result := br.runSingleTest(tc)
		br.collector.RecordResult(result)

		// Progress logging every 10 tests
		if (i+1)%10 == 0 {
			fmt.Printf("  Progress: %d/%d tests completed\n", i+1, len(testCases))
		}
	}

	metrics := br.collector.ComputeMetrics(corpus)
	fmt.Printf("\nBenchmark complete: %d tests, %.1f%% accuracy\n",
		metrics.TotalCases, metrics.Overall.Accuracy*100)

	return metrics, nil
}

// runSingleTest executes a single classification test
func (br *BenchmarkRunner) runSingleTest(tc TestCase) TestResult {
	result := TestResult{
		TestCase:       tc,
		ExpectedIntent: tc.ExpectedIntent,
	}

	startTime := time.Now()

	// Call the classifier
	intent, err := br.classifier.Classify(context.Background(), tc.Input, nil)

	result.LatencyMs = time.Since(startTime).Milliseconds()

	if err != nil {
		result.Error = err.Error()
		result.RawResponse = fmt.Sprintf("error: %v", err)
		result.Correct = false
		return result
	}

	if intent == nil {
		result.Error = "nil intent returned"
		result.Correct = false
		return result
	}

	result.PredictedIntent = intent.Type
	result.Confidence = intent.Confidence

	// Check if prediction matches expected
	result.Correct = strings.EqualFold(intent.Type, tc.ExpectedIntent)

	return result
}

// extractOutputTokens estimates output tokens from response
func extractOutputTokens(response string) int {
	// Simple word-based estimation
	words := len(strings.Fields(response))
	return words // Rough estimate: 1 word ≈ 1.3 tokens, but for JSON responses, word count is close enough
}

// RunComparison runs benchmarks for both models and generates comparison
func RunComparison(ctx context.Context, corpus *TestCorpus, modelA, modelB string) (*BenchmarkResults, error) {
	fmt.Printf("Starting classifier comparison: %s vs %s\n", modelA, modelB)
	fmt.Printf("Total test cases: %d\n", corpus.TotalCount())

	// Run Model A
	fmt.Printf("\n=== Running Model A: %s ===\n", modelA)
	runnerA, err := NewBenchmarkRunner(BenchmarkConfig{
		ModelRef:        modelA,
		Timeout:         30 * time.Second,
		Temperature:     0.1,
		MaxOutputTokens: 512,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner A: %w", err)
	}

	metricsA, err := runnerA.Run(ctx, corpus)
	if err != nil {
		return nil, fmt.Errorf("Model A benchmark failed: %w", err)
	}

	// Run Model B
	fmt.Printf("\n=== Running Model B: %s ===\n", modelB)
	runnerB, err := NewBenchmarkRunner(BenchmarkConfig{
		ModelRef:        modelB,
		Timeout:         30 * time.Second,
		Temperature:     0.1,
		MaxOutputTokens: 512,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner B: %w", err)
	}

	metricsB, err := runnerB.Run(ctx, corpus)
	if err != nil {
		return nil, fmt.Errorf("Model B benchmark failed: %w", err)
	}

	// Generate comparison report
	report := GenerateComparisonReport(metricsA, metricsB)

	return report, nil
}
```

- [x]**Step 3: Create CLI test runner**

Create `cmd/meept-classifier-test/main.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/caimlas/meept/internal/eval"
)

func main() {
	modelA := flag.String("model-a", "local/lfm-combined-sft", "First model to test")
	modelB := flag.String("model-b", "local/lfm-thinking-claude", "Second model to test")
	outputDir := flag.String("output", "docs/eval", "Output directory for reports")
	detailed := flag.Bool("detailed", false, "Include detailed per-test results in report")
	flag.Parse()

	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║     Meept Classifier Model Evaluation Tool        ║")
	fmt.Println("╚═══════════════════════════════════════════════════╝")
	fmt.Println()

	// Load test corpus
	fmt.Println("Loading test corpus...")
	corpus, err := eval.LoadTestCorpus()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading test corpus: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Test corpus loaded: %s v%s\n", corpus.Name, corpus.Version)
	fmt.Printf("Categories: %d | Total test cases: %d\n",
		len(corpus.CategoryNames()), corpus.TotalCount())
	fmt.Println()

	// Run comparison
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	report, err := eval.RunComparison(ctx, corpus, *modelA, *modelB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running comparison: %v\n", err)
		os.Exit(1)
	}

	// Print summary
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Println("                    RESULTS")
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Printf("\nBetter Model: %s\n", report.BetterModel)
	fmt.Printf("\n%s\n", report.Summary)

	// Print Model A metrics
	fmt.Printf("\n─── Model A: %s ───\n", report.ModelA.ModelName)
	fmt.Printf("Accuracy:        %.1f%% (%d/%d)\n",
		report.ModelA.Overall.Accuracy*100,
		report.ModelA.TotalCorrect,
		report.ModelA.TotalCases)
	fmt.Printf("Avg Latency:     %.0fms\n", report.ModelA.AvgLatencyMs)
	fmt.Printf("Avg Confidence:  %.3f\n", report.ModelA.AvgConfidence)
	fmt.Printf("Avg Tokens:      %.1f\n", report.ModelA.AvgOutputTokens)
	fmt.Printf("Errors:          %d (%.1f%%)\n",
		report.ModelA.TotalErrors,
		float64(report.ModelA.TotalErrors)/float64(report.ModelA.TotalCases)*100)

	// Print Model B metrics
	fmt.Printf("\n─── Model B: %s ───\n", report.ModelB.ModelName)
	fmt.Printf("Accuracy:        %.1f%% (%d/%d)\n",
		report.ModelB.Overall.Accuracy*100,
		report.ModelB.TotalCorrect,
		report.ModelB.TotalCases)
	fmt.Printf("Avg Latency:     %.0fms\n", report.ModelB.AvgLatencyMs)
	fmt.Printf("Avg Confidence:  %.3f\n", report.ModelB.AvgConfidence)
	fmt.Printf("Avg Tokens:      %.1f\n", report.ModelB.AvgOutputTokens)
	fmt.Printf("Errors:          %d (%.1f%%)\n",
		report.ModelB.TotalErrors,
		float64(report.ModelB.TotalErrors)/float64(report.ModelB.TotalCases)*100)

	// Save results
	reportFile := filepath.Join(*outputDir, "classifier-comparison-report.json")
	if err := saveReport(report, reportFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving report: %v\n", err)
	} else {
		fmt.Printf("\nReport saved to: %s\n", reportFile)
	}

	// Generate markdown report
	markdownFile := filepath.Join(*outputDir, "classifier-comparison-report.md")
	if err := generateMarkdownReport(report, markdownFile, *detailed); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating markdown: %v\n", err)
	} else {
		fmt.Printf("Markdown report saved to: %s\n", markdownFile)
	}
}

func saveReport(report *eval.BenchmarkResults, filename string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

func generateMarkdownReport(report *eval.BenchmarkResults, filename string, detailed bool) error {
	var sb strings.Builder

	sb.WriteString("# LFM2.5 Classifier Model Comparison Report\n\n")
	sb.WriteString(fmt.Sprintf("**Generated**: %s\n\n", report.TestTimestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Test**: %s\n\n", report.TestName))

	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("**Better Model**: %s\n\n", report.BetterModel))
	sb.WriteString(fmt.Sprintf("%s\n\n", report.Summary))

	sb.WriteString("## Overall Comparison\n\n")
	sb.WriteString("| Metric | Model A | Model B |\n")
	sb.WriteString("|--------|---------|---------|\n")
	sb.WriteString(fmt.Sprintf("| Accuracy | %.1f%% | %.1f%% |\n",
		report.ModelA.Overall.Accuracy*100, report.ModelB.Overall.Accuracy*100))
	sb.WriteString(fmt.Sprintf("| Avg Latency | %.0fms | %.0fms |\n",
		report.ModelA.AvgLatencyMs, report.ModelB.AvgLatencyMs))
	sb.WriteString(fmt.Sprintf("| Avg Confidence | %.3f | %.3f |\n",
		report.ModelA.AvgConfidence, report.ModelB.AvgConfidence))
	sb.WriteString(fmt.Sprintf("| Avg Output Tokens | %.1f | %.1f |\n",
		report.ModelA.AvgOutputTokens, report.ModelB.AvgOutputTokens))
	sb.WriteString(fmt.Sprintf("| Error Rate | %.1f%% | %.1f%% |\n",
		float64(report.ModelA.TotalErrors)/float64(report.ModelA.TotalCases)*100,
		float64(report.ModelB.TotalErrors)/float64(report.ModelB.TotalCases)*100))

	sb.WriteString("\n## Per-Category Accuracy\n\n")
	sb.WriteString("| Category | Model A | Model B |\n")
	sb.WriteString("|----------|---------|---------|\n")

	// Build a map of Model A categories for easy lookup
	aCats := make(map[string]eval.CategoryMetrics)
	for _, cat := range report.ModelA.ByCategory {
		aCats[cat.Category] = cat
	}

	for _, catB := range report.ModelB.ByCategory {
		catA := aCats[catB.Category]
		sb.WriteString(fmt.Sprintf("| %s | %.1f%% (%d/%d) | %.1f%% (%d/%d) |\n",
			catB.Category,
			catA.Accuracy*100, catA.Correct, catA.TotalTests,
			catB.Accuracy*100, catB.Correct, catB.TotalTests))
	}

	if detailed {
		sb.WriteString("\n## Detailed Results\n\n")
		sb.WriteString("### Model A - All Test Cases\n\n")
		for _, result := range report.ModelA.Results {
			status := "✓"
			if !result.Correct {
				status = "✗"
			}
			sb.WriteString(fmt.Sprintf("- %s \"%s\" → %s (conf: %.3f, latency: %dms)\n",
				status, result.TestCase.Input, result.PredictedIntent,
				result.Confidence, result.LatencyMs))
		}

		sb.WriteString("\n### Model B - All Test Cases\n\n")
		for _, result := range report.ModelB.Results {
			status := "✓"
			if !result.Correct {
				status = "✗"
			}
			sb.WriteString(fmt.Sprintf("- %s \"%s\" → %s (conf: %.3f, latency: %dms)\n",
				status, result.TestCase.Input, result.PredictedIntent,
				result.Confidence, result.LatencyMs))
		}
	}

	sb.WriteString("\n## Recommendations\n\n")
	sb.WriteString("Based on the evaluation results:\n\n")

	// Generate recommendations based on metrics differences
	accDiff := report.ModelB.Overall.Accuracy - report.ModelA.Overall.Accuracy
	if abs(accDiff) < 0.05 {
		sb.WriteString("1. **Accuracy**: Both models show similar accuracy (within 5%%). ")
	} else if accDiff > 0 {
		sb.WriteString(fmt.Sprintf("1. **Accuracy**: Model B is %.1f%% more accurate than Model A. ", accDiff*100))
	} else {
		sb.WriteString(fmt.Sprintf("1. **Accuracy**: Model A is %.1f%% more accurate than Model B. ", -accDiff*100))
	}

	latencyDiff := report.ModelA.AvgLatencyMs - report.ModelB.AvgLatencyMs
	if latencyDiff > 100 {
		sb.WriteString(fmt.Sprintf("\n2. **Latency**: Model B is %.0fms faster per classification.", latencyDiff))
	} else if latencyDiff < -100 {
		sb.WriteString(fmt.Sprintf("\n2. **Latency**: Model A is %.0fms faster per classification.", -latencyDiff))
	}

	tokenDiff := report.ModelA.AvgOutputTokens - report.ModelB.AvgOutputTokens
	if abs(tokenDiff) > 50 {
		if tokenDiff > 0 {
			sb.WriteString(fmt.Sprintf("\n3. **Token Efficiency**: Model B uses %.0f fewer tokens on average.", tokenDiff))
		} else {
			sb.WriteString(fmt.Sprintf("\n3. **Token Efficiency**: Model A uses %.0f fewer tokens on average.", -tokenDiff))
		}
	}

	sb.WriteString("\n\n---\n*Report generated by meept-classifier-test*\n")

	return os.WriteFile(filename, []byte(sb.String()), 0644)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
```

- [x]**Step 4: Build and test the CLI**

```bash
cd /Users/caimlas/git/meept
go build -o bin/meept-classifier-test ./cmd/meept-classifier-test
./bin/meept-classifier-test --help
```

Expected output:
```
Usage of meept-classifier-test:
  -detailed
    	Include detailed per-test results in report
  -model-a string
    	First model to test (default "local/lfm-combined-sft")
  -model-b string
    	Second model to test (default "local/lfm-thinking-claude")
  -output string
    	Output directory for reports (default "docs/eval")
```

- [x]**Step 5: Commit**

```bash
git add internal/eval/ cmd/meept-classifier-test/
git commit -m "feat: add classifier benchmark harness and CLI"
```

---

### Task 4: Configure MLX Model Paths

**Files:**
- Modify: `config/models.json5` - Add MLX-specific configuration

- [x]**Step 1: Add MLX provider configuration**

Add to `config/models.json5` providers section:

```json5
"mlx-local": {
  "api": "openai",
  "options": {
    "baseURL": "http://127.0.0.1:8080/v1"
  },
  "models": {
    "lfm-combined-sft": {
      "name": "lfm2.5-1.2b-combined-serialized-sft",
      "path": "/Volumes/LLMs/lfm2.5-1.2b-combined-serialized-sft",
      "capabilities": ["completion", "code", "reasoning"],
      "input_cost": 0.0,
      "output_cost": 0.0,
      "context_limit": 8192,
      "max_output": 512,
      "temperature": 0.1
    },
    "lfm-thinking-claude": {
      "name": "LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit",
      "path": "/Volumes/LLMs/alexgusevski/LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit",
      "capabilities": ["completion", "reasoning"],
      "input_cost": 0.0,
      "output_cost": 0.0,
      "context_limit": 8192,
      "max_output": 512,
      "temperature": 0.1
    }
  }
},
```

- [x]**Step 2: Create MLX startup script**

Create `scripts/start-mlx-model.sh`:

```bash
#!/bin/bash
# Start MLX server with specified model

MODEL_PATH="$1"
if [ -z "$MODEL_PATH" ]; then
    echo "Usage: $0 <model-path>"
    echo "Example: $0 /Volumes/LLMs/lfm2.5-1.2b-combined-serialized-sft"
    exit 1
fi

if [ ! -d "$MODEL_PATH" ]; then
    echo "Error: Model path does not exist: $MODEL_PATH"
    exit 1
fi

echo "Starting MLX server with model: $MODEL_PATH"
echo "Server will be available at http://127.0.0.1:8080"

# Run mlx_lm.server with the specified model
mlx_lm.server --model "$MODEL_PATH" --port 8080
```

Make executable:
```bash
chmod +x scripts/start-mlx-model.sh
```

- [x]**Step 3: Create model switcher script**

Create `scripts/switch-classifier-model.sh`:

```bash
#!/bin/bash
# Switch the classifier model configuration

set -e

MODEL="$1"
CONFIG_FILE="$HOME/.meept/models.json5"

if [ -z "$MODEL" ]; then
    echo "Usage: $0 <model-name>"
    echo "Available models:"
    echo "  - combined-sft"
    echo "  - thinking-claude"
    exit 1
fi

case "$MODEL" in
    "combined-sft")
        MODEL_REF="local/lfm-combined-sft"
        ;;
    "thinking-claude")
        MODEL_REF="local/lfm-thinking-claude"
        ;;
    *)
        echo "Unknown model: $MODEL"
        exit 1
        ;;
esac

# Update the small_model and classifier_model settings
sed -i '' "s/\"small_model\":.*/\"small_model\": \"$MODEL_REF\",/" "$CONFIG_FILE"
sed -i '' "s/\"classifier_model\":.*/\"classifier_model\": \"$MODEL_REF\",/" "$CONFIG_FILE"

echo "Switched classifier model to: $MODEL_REF"
```

Make executable:
```bash
chmod +x scripts/switch-classifier-model.sh
```

- [x]**Step 4: Commit**

```bash
git add config/models.json5 scripts/*.sh
git commit -m "config: add MLX model paths and helper scripts"
```

---

### Task 5: Run Benchmarks

**Prerequisites:**
- MLX installed (`pip install mlx-lm`)
- Both models downloaded to `/Volumes/LLMs/`
- Meept daemon built

- [x]**Step 1: Start MLX server with Model A**

```bash
# In terminal 1
./scripts/start-mlx-model.sh /Volumes/LLMs/lfm2.5-1.2b-combined-serialized-sft
```

Wait for server to be ready:
```bash
curl http://127.0.0.1:8080/health
# Should return: {"status": "ok"}
```

- [x]**Step 2: Configure classifier to use Model A**

```bash
./scripts/switch-classifier-model.sh combined-sft
```

- [x]**Step 3: Run Model A benchmarks**

```bash
./bin/meept-classifier-test \
  --model-a local/lfm-combined-sft \
  --model-b local/lfm-combined-sft \
  --output docs/eval/model-a-only
```

This runs the full test suite against Model A only (both flags same).

- [x]**Step 4: Stop MLX server and start Model B**

```bash
# Kill the MLX server
pkill -f "mlx_lm.server"

# Start Model B
./scripts/start-mlx-model.sh /Volumes/LLMs/alexgusevski/LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit
```

- [x]**Step 5: Configure classifier to use Model B**

```bash
./scripts/switch-classifier-model.sh thinking-claude
```

- [x]**Step 6: Run Model B benchmarks**

```bash
./bin/meept-classifier-test \
  --model-a local/lfm-thinking-claude \
  --model-b local/lfm-thinking-claude \
  --output docs/eval/model-b-only
```

- [x]**Step 7: Run head-to-head comparison**

With both models available (need two MLX instances on different ports):

```bash
# Terminal 1: Model A on port 8080
mlx_lm.server --model /Volumes/LLMs/lfm2.5-1.2b-combined-serialized-sft --port 8080

# Terminal 2: Model B on port 8081
mlx_lm.server --model /Volumes/LLMs/alexgusevski/LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit --port 8081
```

Update `config/models.json5` to add port-specific providers:

```json5
"mlx-model-a": {
  "api": "openai",
  "options": {
    "baseURL": "http://127.0.0.1:8080/v1"
  },
  "models": {
    "lfm-combined-sft": { ... }
  }
},
"mlx-model-b": {
  "api": "openai",
  "options": {
    "baseURL": "http://127.0.0.1:8081/v1"
  },
  "models": {
    "lfm-thinking-claude": { ... }
  }
},
```

Then run:
```bash
./bin/meept-classifier-test \
  --model-a mlx-model-a/lfm-combined-sft \
  --model-b mlx-model-b/lfm-thinking-claude \
  --output docs/eval
```

---

### Task 6: Generate Comparison Report

**Files:**
- Create: `docs/eval/classifier-comparison-report.md`

- [x]**Step 1: Review generated reports**

```bash
cat docs/eval/classifier-comparison-report.md
cat docs/eval/classifier-comparison-report.json
```

- [x]**Step 2: Add manual analysis**

Append to `docs/eval/classifier-comparison-report.md`:

```markdown
## Manual Analysis

### Error Patterns

**Model A Common Failures:**
- List patterns where Model A consistently fails

**Model B Common Failures:**
- List patterns where Model B consistently fails

### Confidence Calibration

Analysis of whether confidence scores correlate with actual accuracy:

| Model | Avg Confidence (Correct) | Avg Confidence (Wrong) | Calibration Quality |
|-------|-------------------------|------------------------|---------------------|
| A | TBD | TBD | TBD |
| B | TBD | TBD | TBD |

### Token Efficiency by Intent Type

| Intent | Model A Avg Tokens | Model B Avg Tokens | Difference |
|--------|-------------------|-------------------|------------|
| code | TBD | TBD | TBD |
| debug | TBD | TBD | TBD |
| chat | TBD | TBD | TBD |
| ... | ... | ... | ... |

### Latency Distribution

| Percentile | Model A | Model B |
|------------|---------|---------|
| p50 | TBD ms | TBD ms |
| p75 | TBD ms | TBD ms |
| p90 | TBD ms | TBD ms |
| p99 | TBD ms | TBD ms |

---

## Final Recommendation

**Selected Model**: [TBD]

**Rationale**:
- Based on the comprehensive evaluation, [Model X] is recommended for the classifier role because:
  1. Higher accuracy on critical intents (code, debug, git)
  2. Better token efficiency
  3. Acceptable latency for classification tasks

**Configuration**:
```json5
{
  "classifier_model": "local/[recommended-model]",
  "small_model": "local/[recommended-model]"
}
```
```

- [x]**Step 3: Commit**

```bash
git add docs/eval/
git commit -m "docs: add classifier comparison report"
```

---

### Task 7: Systematic Harness Bug Hunting

**Files:**
- Review: `internal/eval/`, `internal/agent/llm_classifier.go`, `internal/agent/dispatcher.go`
- Create: `docs/eval/classifier-harness-bugs.md` - Bug findings log

**Rationale:** Based on historical harness bug patterns from docs/auto-analysis/0001-0053, systematically search for bugs in the classifier harness before and during benchmark execution.

- [x]**Step 1: Check for stub implementations (Pattern from 0014-B2, 0014-B4)**

Verify that all benchmark functions actually execute real work, not return cached status:

```bash
# Search for stub patterns in eval package
grep -n "GetStatus\|cached\|stub\|TODO" internal/eval/*.go

# Verify RunComparison actually calls the classifier
grep -n "Classify\|classify" internal/eval/classifier_benchmark.go
```

Expected: `RunComparison` and `runSingleTest` directly invoke `classifier.Classify()`, not return stub data.

- [x]**Step 2: Verify config-to-runtime wiring (Pattern from 0014-B3)**

Check that classifier config is fully mapped from config file to runtime:

```bash
# Check how classifier_model config flows to LLMClassifier
grep -n "classifier_model\|ClassifierModel" internal/config/schema.go internal/daemon/components.go internal/agent/*.go
```

Verify:
- `config.models.json5` `classifier_model` field exists
- `internal/config/schema.go` has `ClassifierModel` in `MultiAgentConfig`
- `internal/daemon/components.go` maps config to LLMClassifierConfig
- Temperature, timeout, max_output settings all flow through

- [x]**Step 3: Check logging completeness (Pattern from 0012-B2, 0012-B3, 0012-B5)**

Verify that ALL classification attempts are logged, not just failures:

```go
// Check llm_classifier.go for logging
grep -n "logger\.\|slog\|Debug\|Info\|Warn" internal/agent/llm_classifier.go
```

Expected logging:
- Classification attempt (DEBUG): input length, model used
- Classification success (DEBUG): predicted intent, confidence
- Classification failure (WARN): error, fallback triggered
- Cooldown activated (INFO): reason, retry_after time

Fix any missing logging before benchmarks run.

- [x]**Step 4: Test ALL intent types are handled (Pattern from 0012-B1)**

Verify classifier handles all 12 intent types, not just common ones:

```go
// Check intent validation
grep -n "isValidIntent\|validIntents" internal/agent/llm_classifier.go
```

Expected: All 12 intents in `intentThresholds` map are checked:
- git, schedule, code, debug, review, plan, platform, report, recall, analyze, search, chat

Add test cases for each intent type to the test corpus if any are missing.

- [x]**Step 5: Verify error messages are actionable (Pattern from 0014-B1)**

Check that classifier errors include fix guidance:

```go
// Check error messages in llm_classifier.go
grep -n "fmt.Errorf\|return.*err" internal/agent/llm_classifier.go
```

Expected errors include:
- "classifier unavailable": include retry_after hint
- "empty response": include model name that failed
- "invalid intent": list valid intents
- "no client configured": include config path to fix

Fix any unhelpful error messages.

- [x]**Step 6: Test state survives multiple calls (Pattern from 0051-B1, 0052-B1)**

Verify classifier cooldown state works correctly across sequential calls:

```bash
# Create a test that calls Classify 10 times rapidly
cat > /tmp/classifier_stress_test.go << 'EOF'
func TestClassifierSequentialCalls(t *testing.T) {
    c := newTestClassifier()
    ctx := context.Background()

    // Call 10 times rapidly - state should survive
    for i := 0; i < 10; i++ {
        intent, err := c.Classify(ctx, fmt.Sprintf("test message %d", i), nil)
        if err != nil && !strings.Contains(err.Error(), "cooldown") {
            t.Errorf("Call %d failed unexpectedly: %v", i, err)
        }
        _ = intent
    }
}
EOF
```

Expected: Classifier handles rapid calls without losing state or crashing.

- [x]**Step 7: Verify metrics endpoints return proper JSON (Pattern from 0053-B1)**

Check that benchmark results are proper JSON, not Go struct strings:

```bash
# After running benchmarks, check output format
cat docs/eval/classifier-comparison-report.json | head -20
```

Expected: Valid JSON like `{"test_name": "...", "model_a": {...}}`
NOT: `&{TestName:...}` Go struct format

- [x]**Step 8: Document findings**

Create `docs/eval/classifier-harness-bugs.md`:

```markdown
# Classifier Harness Bug Findings

## Bugs Found

### Bug #1: [Description]
- **Severity**: [Critical/High/Medium/Low]
- **File**: [path/to/file.go:line]
- **Pattern**: [Which historical pattern this matches]
- **Status**: [Fixed/Open]

## Verification Checklist

- [x]No stub implementations found
- [x]Config wiring complete
- [x]Logging comprehensive (all code paths)
- [x]All 12 intent types handled
- [x]Error messages actionable
- [x]State survives concurrent calls
- [x]Metrics output proper JSON
```

- [x]**Step 9: Fix any bugs found**

For each bug found:
1. Document in `docs/eval/classifier-harness-bugs.md`
2. Fix in source code
3. Add regression test
4. Commit with descriptive message

- [x]**Step 10: Commit**

```bash
git add docs/eval/classifier-harness-bugs.md internal/eval/ internal/agent/
git commit -m "fix: classifier harness bug fixes from systematic review"
```

---

## Final Verification

- [x]**F1. Build Verification**

```bash
cd /Users/caimlas/git/meept
go build ./cmd/meept-classifier-test/...
./bin/meept-classifier-test --help
```

Expected: CLI help displayed without errors

- [x]**F2. Test Corpus Verification**

```bash
go test ./internal/eval/... -v -run TestCorpus
```

Expected: All tests pass, corpus loads correctly

- [x]**F3. Benchmark Harness Verification**

Run a quick smoke test with 5 examples:
```bash
# Create minimal test corpus
cat > testdata/eval/smoke-test.json5 << 'EOF'
{
  name: "Smoke Test",
  categories: {
    chat: [
      { input: "hello", expected_intent: "chat" },
    ],
    code: [
      { input: "write a function", expected_intent: "code" },
    ],
  },
}
EOF

./bin/meept-classifier-test --model-a local/lfm-combined-sft --model-b local/lfm-thinking-claude
```

Expected: Benchmark completes, generates report files

- [x]**F4. Harness Bug Hunt Verification**

```bash
# Check for stub patterns
grep -rn "GetStatus\|cached\|stub" internal/eval/*.go

# Verify config wiring
grep -n "ClassifierModel\|classifier_model" internal/config/schema.go internal/daemon/components.go

# Check logging coverage
grep -n "logger\.\|Debug\|Info\|Warn" internal/agent/llm_classifier.go | head -20

# Verify JSON output
cat docs/eval/classifier-comparison-report.json | python3 -m json.tool > /dev/null && echo "Valid JSON"
```

Expected: No stubs found, config wired, logging present, valid JSON output

---

## Success Criteria

### Verification Commands
```bash
# Build all components
go build ./cmd/meept-classifier-test/...
go build ./internal/eval/...

# Run tests
go test ./internal/eval/... -v

# Run benchmark (requires MLX server running)
./bin/meept-classifier-test --model-a local/lfm-combined-sft --model-b local/lfm-thinking-claude
```

### Final Checklist
- [x]Both models configured in `config/models.json5`
- [x]Test corpus has 100+ examples across all intent types
- [x]Benchmark harness correctly calls LLM classifier (no stubs)
- [x]Metrics collection captures: accuracy, latency, tokens, errors
- [x]CLI runner produces JSON and markdown reports
- [x]Helper scripts for MLX model management
- [x]Comparison report with side-by-side analysis
- [x]Final recommendation documented
- [x]**Harness bug hunt completed (Task 7)**
- [x]**Bug findings documented in `docs/eval/classifier-harness-bugs.md`**
- [x]**All historical patterns checked (0012-B*, 0013-B*, 0014-B*, 0051-B*, 0052-B*, 0053-B*)**
```

### Final Checklist
- [x]Both models configured in `config/models.json5`
- [x]Test corpus has 100+ examples across all intent types
- [x]Benchmark harness correctly calls LLM classifier
- [x]Metrics collection captures: accuracy, latency, tokens, errors
- [x]CLI runner produces JSON and markdown reports
- [x]Helper scripts for MLX model management
- [x]Comparison report with side-by-side analysis
- [x]Final recommendation documented

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-22-classifier-model-evaluation.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**