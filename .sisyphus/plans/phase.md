# LLM Intent Classifier

## TL;DR

> **Quick Summary**: Add an LLM-based IntentClassifier to the dispatcher that uses a lightweight model for routing decisions, with a fallback chain (LLM → Keyword → Chat) and adaptive confidence thresholds per intent type.
>
> **Deliverables**:
> - New `LLMClassifier` implementing `IntentClassifier` interface
> - `classifier_model` config (defaults to `small_model`)
> - Adaptive thresholds per intent type
> - Fallback chain: LLM fails → Keyword → Keyword fails → Chat
> - Automated tests for classifier pipeline
>
> **Estimated Effort**: Short
> **Parallel Execution**: NO (sequential with 2 waves)
> **Critical Path**: Config → LLMClassifier → Integration → Tests

---

## Context

### Original Request
Add LLM-based intent classification to dispatcher.go with:
1. New `classifier_model` config defaulting to `small_model`
2. Fallback chain: LLM → Keyword → Chat (no double keyword)
3. Adaptive confidence thresholds per intent type
4. Automated tests

### Interview Summary
**Key Discussions**:
- IntentClassifier interface already exists (dispatcher.go:59-62)
- Multi-classifier pipeline exists but only KeywordClassifier is registered
- `small_model` exists in models.json5 but is NOT wired to dispatcher
- User chose adaptive thresholds (Option A) over simple threshold
- User wants automated tests, not just manual QA

**Research Findings**:
- dispatcher.go:196-225 loops classifiers, picks highest confidence
- internal/llm/resolver.go has `SmallModel()` method
- models.json5 has `small_model: "zai/glm-4.5-air"` with comment "fast/cheap model"
- existing test patterns in dispatcher_test.go

### Metis Review
Not available (environment issue). Self-review identified:
- Need to handle LLM failure gracefully (fallback to keyword)
- Need to handle keyword failure gracefully (fallback to chat)
- Need timeout for LLM classification to prevent blocking
- Need to ensure no circular dependencies

---

## Work Objectives

### Core Objective
Improve intent classification accuracy by adding LLM-based routing alongside existing keyword matching, with a smart fallback chain.

### Concrete Deliverables
- `config/models.json5`: Add `classifier_model` field
- `internal/config/schema.go`: Add `ClassifierModel` to `MultiAgentConfig`
- `internal/agent/llm_classifier.go`: New LLM-based classifier
- `internal/agent/dispatcher.go`: Wire LLMClassifier + implement fallback chain
- `internal/agent/llm_classifier_test.go`: Automated tests

### Definition of Done
- [ ] `go test ./internal/agent/... -run Classifier -v` → PASS
- [ ] LLM classifier correctly identifies all 12 intent types
- [ ] Fallback chain: LLM failure → Keyword → Chat
- [ ] Adaptive thresholds applied correctly per intent type

### Must Have
- LLMClassifier implementing IntentClassifier interface
- classifier_model config (defaults to small_model if not set)
- Fallback chain: LLM → Keyword → Chat
- Timeout for LLM classification (5 seconds max)
- Automated tests for fallback behavior

### Must NOT Have
- Keyword classifier evaluated twice in the fallback chain
- Blocking on LLM classification (must have timeout)
- Circular dependencies in the classifier pipeline
- Changing existing KeywordClassifier behavior

---

## Verification Strategy

### Test Decision
- **Infrastructure exists**: YES (go test)
- **Automated tests**: YES (tests-after)
- **Framework**: Go's standard testing package
- **Test location**: `internal/agent/llm_classifier_test.go`

### QA Policy
Every task includes agent-executed QA via `go test`. Evidence captured via test output.

---

## Execution Strategy

### Waves

```
Wave 1 (Foundation — sequential):
├── Task 1: Add classifier_model to config/schema (quick)
├── Task 2: Add classifier_model to models.json5 (quick)
└── Task 3: Create LLMClassifier implementation (medium)

Wave 2 (Integration + Tests — sequential after Wave 1):
├── Task 4: Wire LLMClassifier into dispatcher + implement fallback chain (medium)
└── Task 5: Add automated tests for LLMClassifier (medium)

Final Verification:
├── Task F1: go test ./internal/agent/... -v
└── Task F2: Verify fallback chain with mock LLM failure
```

### Dependency Matrix

- **1**: — — 3, 4
- **2**: — — 3, 4
- **3**: 1, 2 — 4, 5
- **4**: 3 — 5, F1, F2
- **5**: 4 — F1, F2
- **F1**: 4, 5 — —
- **F2**: 4, 5 — —

---

## TODOs

---

- [ ] 1. Add ClassifierModel to config schema

  **What to do**:
  - Add `ClassifierModel string` field to `MultiAgentConfig` struct in `internal/config/schema.go`
  - Add to `DefaultConfig()`: `ClassifierModel: "",` (empty = use small_model)
  - Add TOML tag: `toml:"classifier_model"`

  **Must NOT do**:
  - Don't add validation logic yet (handled in LLMClassifier)
  - Don't modify other config structs

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Simple config field addition, well-defined scope
  - **Skills**: none
  - **Skills Evaluated but Omitted**:
    - No specialized skills needed for this task

  **Parallelization**:
  - **Can Run In Parallel**: NO (Wave 1, sequential)
  - **Blocks**: Tasks 2, 3
  - **Blocked By**: None

  **References**:
  - `internal/config/schema.go:261-267` - MultiAgentConfig struct as reference pattern
  - `internal/config/schema.go:609-615` - DefaultConfig() for MultiAgent defaults

  **Acceptance Criteria**:
  - [ ] `go build ./internal/config/...` → PASS

  **QA Scenarios**:

  ```
  Scenario: Build succeeds with new field
    Tool: Bash
    Steps:
      1. cd /Users/caimlas/git/meept && go build ./internal/config/...
      2. Assert exit code = 0
    Expected Result: Build passes without errors
    Evidence: .sisyphus/evidence/task-1-build.log

  Scenario: DefaultConfig() includes ClassifierModel
    Tool: Bash
    Steps:
      1. grep -n "ClassifierModel" internal/config/schema.go
      2. Assert output contains "ClassifierModel"
    Expected Result: Field exists in schema
    Evidence: .sisyphus/evidence/task-1-grep.log
  ```

  **Evidence to Capture**:
  - [ ] Build log: task-1-build.log
  - [ ] Grep output: task-1-grep.log

  **Commit**: YES (group with Task 2)
  - Message: `feat(config): add classifier_model field`
  - Files: `internal/config/schema.go`
  - Pre-commit: `go build ./internal/config/...`

---

- [ ] 2. Add classifier_model to models.json5

  **What to do**:
  - Add `classifier_model` field to `config/models.json5`
  - Set default value to `"zai/glm-4.5-air"` (same as small_model)
  - Add comment: `// Model for intent classification (defaults to small_model)`
  - Add to `.gitignore` pattern if needed for user overrides

  **Must NOT do**:
  - Don't change the default model or small_model values
  - Don't add new providers

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Simple config update, no code changes
  - **Skills**: none

  **Parallelization**:
  - **Can Run In Parallel**: NO (Wave 1, sequential)
  - **Blocks**: Task 3
  - **Blocked By**: None (can start in parallel with Task 1)

  **References**:
  - `config/models.json5:1-10` - Current top-level config structure

  **Acceptance Criteria**:
  - [ ] `classifier_model` field present in models.json5
  - [ ] Defaults to `zai/glm-4.5-air`

  **QA Scenarios**:

  ```
  Scenario: classifier_model field exists in config
    Tool: Bash
    Steps:
      1. grep -n "classifier_model" config/models.json5
      2. Assert output contains "glm-4.5-air"
    Expected Result: Field found with correct default
    Evidence: .sisyphus/evidence/task-2-grep.log
  ```

  **Evidence to Capture**:
  - [ ] Grep output: task-2-grep.log

  **Commit**: YES (group with Task 1)
  - Message: `feat(config): add classifier_model field`
  - Files: `config/models.json5`

---

- [ ] 3. Create LLMClassifier implementation

  **What to do**:
  - Create `internal/agent/llm_classifier.go`
  - Implement `IntentClassifier` interface
  - Use ProviderManager or Client for LLM calls
  - Implement adaptive confidence thresholds per intent type
  - Add timeout (5 seconds) for LLM classification
  - Return structured JSON from LLM response

  **Adaptive Thresholds** (per intent type):
  | Intent | Threshold | Rationale |
  |--------|-----------|-----------|
  | git | 0.85 | Destructive operations - don't guess |
  | schedule | 0.80 | Timed actions precision |
  | code | 0.75 | Modifying files - be sure |
  | debug | 0.75 | Fixing issues - accuracy matters |
  | plan | 0.70 | Planning is expensive - want right routing |
  | platform | 0.70 | Info queries - less risky |
  | report | 0.70 | Status queries |
  | recall | 0.70 | Memory queries |
  | analyze | 0.60 | Research can adapt |
  | search | 0.60 | Search queries |
  | chat | 0.50 | Low stakes - chat can handle anything |
  | review | 0.75 | Code review |

  **Must NOT do**:
  - Don't implement fallback logic (that's in dispatcher)
  - Don't modify KeywordClassifier
  - Don't add logging that could leak sensitive data

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: New implementation with careful design decisions
  - **Skills**: none
  - **Skills Evaluated but Omitted**:
    - No specific skill needed

  **Parallelization**:
  - **Can Run In Parallel**: NO (Wave 1, sequential)
  - **Blocks**: Tasks 4, 5
  - **Blocked By**: Tasks 1, 2

  **References**:
  - `internal/agent/dispatcher.go:59-62` - IntentClassifier interface
  - `internal/agent/dispatcher.go:371-444` - KeywordClassifier for pattern reference
  - `internal/llm/provider_manager.go` - ProviderManager for LLM calls
  - `internal/llm/resolver.go:62-65` - SmallModel() method

  **Acceptance Criteria**:
  - [ ] `go build ./internal/agent/...` → PASS
  - [ ] LLMClassifier implements IntentClassifier interface
  - [ ] Timeout of 5 seconds enforced
  - [ ] Adaptive thresholds applied per intent type

  **QA Scenarios**:

  ```
  Scenario: LLMClassifier compiles and implements interface
    Tool: Bash
    Steps:
      1. go build ./internal/agent/...
      2. Assert exit code = 0
    Expected Result: Build succeeds
    Evidence: .sisyphus/evidence/task-3-build.log

  Scenario: Threshold for git intent is 0.85
    Tool: Read
    Steps:
      1. Read internal/agent/llm_classifier.go
      2. Verify git threshold = 0.85
    Expected Result: Correct threshold found
    Evidence: .sisyphus/evidence/task-3-threshold.txt

  Scenario: Timeout is enforced
    Tool: Read
    Steps:
      1. grep -n "timeout\|Timeout\|context.WithTimeout" internal/agent/llm_classifier.go
      2. Assert timeout value = 5 seconds
    Expected Result: 5 second timeout present
    Evidence: .sisyphus/evidence/task-3-timeout.txt
  ```

  **Evidence to Capture**:
  - [ ] Build log: task-3-build.log
  - [ ] Threshold verification: task-3-threshold.txt
  - [ ] Timeout verification: task-3-timeout.txt

  **Commit**: YES
  - Message: `feat(classifier): add LLMClassifier implementation`
  - Files: `internal/agent/llm_classifier.go`
  - Pre-commit: `go build ./internal/agent/...`

---

- [ ] 4. Wire LLMClassifier into dispatcher + implement fallback chain

  **What to do**:
  - Add LLMClassifier to NewDispatcher() alongside KeywordClassifier
  - Modify classifyIntent() to implement fallback chain:
    1. Try LLMClassifier
    2. If fails OR confidence < threshold → try KeywordClassifier
    3. If KeywordClassifier fails → return intent for Chat
  - Pass classifier_model to LLMClassifier constructor
  - Handle LLM failures gracefully (log warning, fall through)

  **Fallback Chain Logic**:
  ```
  func classifyIntent(input, context):
      // Step 1: Try LLM classifier
      intent, err := llmClassifier.Classify(input, context)
      if err == nil && intent.Confidence >= threshold(intent.Type):
          return intent
      
      // Step 2: Try Keyword classifier
      intent, err := keywordClassifier.Classify(input, context)
      if err == nil && intent != nil:
          return intent
      
      // Step 3: Fallback to Chat for clarification
      return &Intent{
          Type: "chat",
          Confidence: 0.3,
          AgentType: "chat",
          Summary: "Could not determine intent, clarifying with user",
      }
  ```

  **Must NOT do**:
  - Don't call KeywordClassifier twice
  - Don't skip the LLM classifier on error (only on failure)
  - Don't change the Intent struct or interface

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Integration work with careful fallback logic
  - **Skills**: none

  **Parallelization**:
  - **Can Run In Parallel**: NO (Wave 2, sequential after Wave 1)
  - **Blocks**: Task 5, Final Verification
  - **Blocked By**: Task 3

  **References**:
  - `internal/agent/dispatcher.go:76-95` - NewDispatcher() implementation
  - `internal/agent/dispatcher.go:196-225` - classifyIntent() method
  - `internal/agent/dispatcher.go:91-92` - KeywordClassifier registration

  **Acceptance Criteria**:
  - [ ] `go build ./internal/agent/...` → PASS
  - [ ] Both classifiers registered in NewDispatcher()
  - [ ] Fallback chain: LLM → Keyword → Chat
  - [ ] KeywordClassifier NOT called twice

  **QA Scenarios**:

  ```
  Scenario: Both classifiers registered
    Tool: Bash
    Steps:
      1. grep -n "LLMClassifier\|KeywordClassifier" internal/agent/dispatcher.go
      2. Assert both found in NewDispatcher
    Expected Result: Both classifiers registered
    Evidence: .sisyphus/evidence/task-4-registration.txt

  Scenario: Fallback chain implemented
    Tool: Read
    Steps:
      1. Read internal/agent/dispatcher.go classifyIntent()
      2. Verify LLM → Keyword → Chat fallback
    Expected Result: Fallback chain present
    Evidence: .sisyphus/evidence/task-4-fallback.txt

  Scenario: Build succeeds
    Tool: Bash
    Steps:
      1. go build ./internal/agent/...
      2. Assert exit code = 0
    Expected Result: Build passes
    Evidence: .sisyphus/evidence/task-4-build.log
  ```

  **Evidence to Capture**:
  - [ ] Registration check: task-4-registration.txt
  - [ ] Fallback logic: task-4-fallback.txt
  - [ ] Build log: task-4-build.log

  **Commit**: YES
  - Message: `feat(dispatcher): wire LLMClassifier with fallback chain`
  - Files: `internal/agent/dispatcher.go`
  - Pre-commit: `go build ./internal/agent/...`

---

- [ ] 5. Add automated tests for LLMClassifier

  **What to do**:
  - Create `internal/agent/llm_classifier_test.go`
  - Test adaptive thresholds for each intent type
  - Test fallback chain behavior (LLM fails → Keyword → Chat)
  - Test timeout behavior
  - Test that KeywordClassifier is NOT called twice
  - Test confidence scoring

  **Test Cases**:
  1. Git intent with high confidence → returns git intent
  2. Chat intent with low confidence → returns chat intent
  3. LLM failure → falls back to KeywordClassifier
  4. Keyword failure → returns Chat fallback
  5. Adaptive threshold applied correctly per intent type
  6. Timeout enforced (use mock that hangs)
  7. Null input handling

  **Must NOT do**:
  - Don't test the actual LLM (use mock)
  - Don't create integration tests that require running daemon

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Writing comprehensive tests with mocking
  - **Skills**: none

  **Parallelization**:
  - **Can Run In Parallel**: NO (Wave 2, sequential)
  - **Blocks**: Final Verification
  - **Blocked By**: Task 4

  **References**:
  - `internal/agent/dispatcher_test.go:140-211` - TestKeywordClassifier patterns
  - `internal/agent/dispatcher_test.go:10-138` - TestShouldDispatchAsync patterns

  **Acceptance Criteria**:
  - [ ] `go test ./internal/agent/... -run Classifier -v` → PASS
  - [ ] All 7 test cases pass
  - [ ] `go test ./internal/agent/...` → PASS (no regressions)

  **QA Scenarios**:

  ```
  Scenario: All classifier tests pass
    Tool: Bash
    Steps:
      1. go test ./internal/agent/... -run "Classifier|Fallback" -v
      2. Assert exit code = 0
      3. Assert output contains "PASS"
    Expected Result: All tests pass
    Evidence: .sisyphus/evidence/task-5-tests.log

  Scenario: No regressions in existing tests
    Tool: Bash
    Steps:
      1. go test ./internal/agent/... -v
      2. Assert no failures in existing tests
    Expected Result: All existing tests still pass
    Evidence: .sisyphus/evidence/task-5-regression.log
  ```

  **Evidence to Capture**:
  - [ ] Test output: task-5-tests.log
  - [ ] Regression check: task-5-regression.log

  **Commit**: YES
  - Message: `test(classifier): add LLMClassifier tests`
  - Files: `internal/agent/llm_classifier_test.go`
  - Pre-commit: `go test ./internal/agent/...`

---

## Final Verification Wave

- [ ] F1. **Build Verification** — `quick`
  Run `go build ./...` and `go test ./internal/agent/...` to verify everything compiles and tests pass.
  Output: `Build [PASS/FAIL] | Tests [N/N pass]`

- [ ] F2. **Fallback Chain Verification** — `unspecified-high`
  Verify that:
  1. LLMClassifier is called first
  2. On LLM failure, KeywordClassifier is called
  3. On KeywordClassifier failure, Chat fallback is returned
  4. KeywordClassifier is NOT called twice
  Output: `Fallback Chain [VERIFIED/FAILED]`

---

## Commit Strategy

- **Wave 1**: `feat(config): add classifier_model field` — schema.go, models.json5
- **Wave 2**: `feat(classifier): add LLMClassifier implementation` — llm_classifier.go
- **Wave 3**: `feat(dispatcher): wire LLMClassifier with fallback chain` — dispatcher.go
- **Wave 4**: `test(classifier): add LLMClassifier tests` — llm_classifier_test.go

---

## Success Criteria

### Verification Commands
```bash
go build ./internal/agent/...  # Expected: success
go test ./internal/agent/... -run Classifier -v  # Expected: all pass
```

### Final Checklist
- [ ] All classifiers registered in NewDispatcher()
- [ ] LLMClassifier uses classifier_model config (defaults to small_model)
- [ ] Fallback chain: LLM → Keyword → Chat
- [ ] KeywordClassifier NOT called twice
- [ ] Adaptive thresholds applied per intent type
- [ ] 5 second timeout for LLM classification
- [ ] All tests pass
- [ ] No regressions in existing tests

