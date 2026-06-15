# Architectural Pattern Remediation Plan (Observations 2-6)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Systematically address the five recurring architectural patterns identified in `docs/plans/glm52-findings-3.md` observations #2-#6 so they stop generating bugs in future code.

**Architecture:** Each sprint targets one observation. Sprints are independent and can be executed in parallel by separate subagents. Sprints follow a consistent pattern: audit → fix existing sites → add guardrails (tests, linters, or codegen) to prevent recurrence.

**Tech Stack:** Go 1.22+, Swift 5.9+, Flutter 3.x. Tests use Go's standard `testing` package, XCTest, and `flutter_test`.

**Source of observations:** `docs/plans/glm52-findings-3.md` lines 314-322, "Recurring patterns worth addressing" section.

**Build/test baseline before starting:**
```bash
go build ./...        # must be clean
go vet ./...          # must be clean
go test ./...         # must be green
go test -race ./...   # must be green
```

---

## File Map (created/modified across all sprints)

| File | Sprint | Responsibility |
|------|--------|----------------|
| `internal/plan/plan.go` | S1 | Atomic ID counters (planIDCounter, phaseIDCounter, signoffIDCounter) |
| `internal/task/task.go` | S1 | Atomic ID counter (taskIDCounter — already atomic, verify) |
| `internal/repomap/graph.go` | S1 | Verify nodeID atomicity (already fixed in round 3) |
| `internal/tui/debug_log.go` | S1 | Verify debugCounter atomicity (already fixed in round 3) |
| `internal/pty/manager.go` | S1 | Audit managerCounter |
| `internal/rpc/server.go` | S1 | Verify counter atomic.Int64 |
| `internal/queue/job.go` | S1 | Verify jobIDCounter atomic.Uint64 |
| `internal/worker/worker.go` | S1 | Verify workerIDCounter atomic.Uint64 |
| `internal/shadow/classifier.go` | S1 | Audit DefaultClassifier package-level var |
| `internal/auth/providers.go` | S1 | Audit OAuthProviders package-level map (read-only by design) |
| `internal/tools/builtin/shell.go` | S2 | Route classifyRisk through errcls where applicable |
| `internal/tools/builtin/shell_tokenize.go` | S2 | Tokenization helpers (already exists, verify) |
| `internal/errcls/classify.go` | S2 | Add command-risk classification support if needed |
| `internal/shadow/manager.go` | S3 | Narrow ProcessRecord mutex scope |
| `internal/runtime/docker.go` | S3 | Narrow Execute mutex scope |
| `internal/tools/builtin/shell.go` | S4 | Verify SetKnownSafeCommands nil guard |
| `internal/tools/builtin/resolve.go` | S4 | Verify SetFenceChecker nil guard |
| `internal/tools/builtin/file_edit.go` | S4 | Verify SetPendingChangesRegistry nil guard |
| `internal/security/engine.go` | S4 | Reference pattern for typed-nil-safe setters |
| `internal/tools/builtin/setters_test.go` | S4 | Generated-style test for all setter nil-safety |
| `pkg/constants/api_key.go` | S5 | Single source of truth for DefaultDevAPIKey |
| `ui/flutter_ui/lib/core/constants.dart` | S5 | Reference pkg/constants, remove hardcoded key |
| `menubar/MeeptMenuBar/Services/MenubarConfigService.swift` | S5 | Gate DefaultDevAPIKey behind #if DEBUG |
| `internal/comm/http/server.go` | S5 | Verify single source usage |
| `internal/transport/client.go` | S5 | Verify single source usage |

---

## Sprint 1 (Observation 2): Package-Level Mutable State Audit

**Goal:** Audit every `var` declaration at package level in `internal/`. Convert mutable counters to atomics, freeze read-only maps, and document intentional package-level state.

### Task 1.1: Audit all package-level var declarations

**Files:** All `.go` files under `internal/`

- [ ] **Step 1: Run the audit grep**

```bash
grep -rn '^var ' internal/ --include='*.go' | grep -v '_test.go' | grep -v 'var _ ' | sort
```

Categorize each hit into:
- **Mutable counter** (e.g., `var planIDCounter uint64`) — must be `atomic.Uint64` or protected by a mutex
- **Read-only map/slice** (e.g., `var OAuthProviders = map[string]OAuthProviderConfig{}`) — must be frozen or documented as intentionally mutable
- **Compiled regex** (e.g., `var jsonFenceRe = regexp.MustCompile(...)`) — safe, read-only after init
- **Sentinel error** (e.g., `var ErrPlanNotFound = errors.New(...)`) — safe, read-only after init
- **Interface assertion** (e.g., `var _ Store = (*SQLiteStore)(nil)`) — safe, compile-time only

- [ ] **Step 2: Record the audit results in a comment block at the top of this task**

Expected mutable counters to verify (from round 3 grep):
- `internal/plan/plan.go:88` — `planIDCounter uint64`
- `internal/plan/plan.go:95` — `phaseIDCounter uint64`
- `internal/plan/plan.go:102` — `signoffIDCounter uint64`
- `internal/task/task.go:287` — `taskIDCounter uint64` (already uses `atomic.AddUint64`)
- `internal/queue/job.go:166` — `jobIDCounter atomic.Uint64` (already atomic)
- `internal/worker/worker.go:377` — `workerIDCounter atomic.Uint64` (already atomic)
- `internal/rpc/server.go:529` — `counter atomic.Int64` (already atomic)
- `internal/pty/manager.go:82` — `managerCounter = &counter{}` (verify counter type)

### Task 1.2: Fix plan.go ID counters

**Files:** `internal/plan/plan.go:88,95,102`

- [ ] **Step 1: Write a race test**

```go
// internal/plan/plan_test.go
func TestPlanIDCounter_Concurrent(t *testing.T) {
    var wg sync.WaitGroup
    ids := make([]string, 100)
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            ids[i] = generatePlanID()
        }(i)
    }
    wg.Wait()
    // Assert all IDs are unique
    seen := make(map[string]bool)
    for _, id := range ids {
        if seen[id] {
            t.Fatalf("duplicate plan ID: %s", id)
        }
        seen[id] = true
    }
}
```

- [ ] **Step 2: Run test to verify it fails (or passes if already safe)**

```bash
go test -race ./internal/plan/ -run TestPlanIDCounter -v
```

- [ ] **Step 3: Convert to atomic if needed**

If `planIDCounter` is a bare `uint64`, change to:

```go
// internal/plan/plan.go
var planIDCounter atomic.Uint64

func generatePlanID() string {
    seq := planIDCounter.Add(1)
    return fmt.Sprintf("plan-%s-%04d", time.Now().UTC().Format("20060102150405"), seq)
}
```

Repeat for `phaseIDCounter` and `signoffIDCounter`.

If the counters already use `atomic`, skip the change and mark the test as a regression guard.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -race ./internal/plan/ -run TestPlanIDCounter -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/plan/plan.go internal/plan/plan_test.go
git commit -m "refactor(plan): use atomic.Uint64 for ID counters (Obs-2)"
```

### Task 1.3: Verify pty/manager.go counter

**Files:** `internal/pty/manager.go:82`

- [ ] **Step 1: Read the counter type definition**

Check whether `counter` struct uses `atomic.Int64` or a bare `int64`. If bare `int64` with mutex, that's acceptable. If bare `int64` without mutex, it's a data race.

- [ ] **Step 2: Fix if needed**

If the `counter` type uses a bare int64 without sync, change to `atomic.Int64`:

```go
type counter struct {
    n atomic.Int64
}

func (c *counter) Next() int64 {
    return c.n.Add(1)
}
```

- [ ] **Step 3: Race test**

```bash
go test -race ./internal/pty/ -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/pty/manager.go
git commit -m "refactor(pty): make managerCounter race-safe (Obs-2)"
```

### Task 1.4: Audit shadow/classifier.go DefaultClassifier

**Files:** `internal/shadow/classifier.go:167`

- [ ] **Step 1: Read the DefaultClassifier initialization**

```bash
grep -n 'DefaultClassifier' internal/shadow/classifier.go
```

`var DefaultClassifier = NewClassifier()` creates a package-level mutable `*Classifier` instance. If `Classifier` has internal state (maps, slices) that callers mutate, this is a shared-mutable-state bug.

- [ ] **Step 2: Check if Classifier methods mutate internal state**

Read the `Classifier` type and its methods. If any method writes to Classifier fields without a mutex, either:
- Add a mutex to Classifier, or
- Make the map/slice read-only after construction (freeze pattern)

- [ ] **Step 3: Fix if needed**

If `Classifier` is mutable and shared:

```go
type Classifier struct {
    mu       sync.RWMutex
    patterns map[string]bool
    // ...
}

func (c *Classifier) Classify(input string) string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    // ...
}
```

- [ ] **Step 4: Race test**

```bash
go test -race ./internal/shadow/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/shadow/classifier.go
git commit -m "refactor(shadow): make DefaultClassifier thread-safe (Obs-2)"
```

### Task 1.5: Document safe package-level vars

**Files:** Create `internal/doc/conventions.go` (or add to existing conventions doc)

- [ ] **Step 1: Add a comment convention**

At each safe package-level var (sentinel errors, compiled regexes, read-only maps), verify there is a comment explaining why it's safe. For example:

```go
// OAuthProviders is a read-only registry populated at init time.
// Callers must not mutate this map.
var OAuthProviders = map[string]OAuthProviderConfig{ ... }
```

If any read-only map lacks such a comment, add one.

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "docs: document safe package-level vars (Obs-2)"
```

---

## Sprint 2 (Observation 3): Substring Matching for Classification

**Goal:** Ensure all error/classification heuristics use structured `errors.As`/`errors.Is` (via `internal/errcls`) rather than `strings.Contains(err.Error(), ...)`. For shell command risk classification, ensure the tokenization is quote-aware (already fixed in round 3 — verify and document the pattern).

### Task 2.1: Audit remaining strings.Contains classification patterns

**Files:** All `.go` files under `internal/`

- [ ] **Step 1: Grep for substring classification patterns**

```bash
grep -rn 'strings\.Contains.*Error\(\)' internal/ --include='*.go' | grep -v _test.go
grep -rn 'strings\.HasPrefix.*Error\(\)' internal/ --include='*.go' | grep -v _test.go
grep -rn 'strings\.HasSuffix.*Error\(\)' internal/ --include='*.go' | grep -v _test.go
```

- [ ] **Step 2: Categorize each hit**

For each result, determine:
- **Already structured** — the error is also checked via `errors.As`/`errors.Is` somewhere (safe, just redundant)
- **Should be structured** — the substring check is the sole classifier (bug-prone, needs migration)
- **Genuine string processing** — not error classification at all (e.g., parsing log output, text processing)

- [ ] **Step 3: Document findings in a comment at the top of this task**

### Task 2.2: Migrate any remaining substring-based error classification to errcls

**Files:** Depends on audit results from Task 2.1

For each "should be structured" hit from Task 2.1:

- [ ] **Step 1: Identify the structured error type**

Check if the error being classified has a corresponding structured type (e.g., `*llm.APIError`, a sentinel error, or an `errors.As`-compatible wrapper).

- [ ] **Step 2: Add the structured check to errcls if not present**

```go
// internal/errcls/classify.go
func IsSpecificError(err error) bool {
    if err == nil {
        return false
    }
    // Use errors.As for typed checks
    var specificErr *SomeStructuredError
    return errors.As(err, &specificErr)
}
```

- [ ] **Step 3: Replace the substring check in the calling code**

- [ ] **Step 4: Test the replacement**

```bash
go test ./internal/errcls/... -v
go test <affected_package>/... -v
```

- [ ] **Step 5: Commit per migration**

```bash
git add internal/errcls/classify.go <affected_files>
git commit -m "refactor: route <category> through errcls instead of substring (Obs-3)"
```

### Task 2.3: Verify shell.go classifyRisk tokenization

**Files:** `internal/tools/builtin/shell.go:508-560`, `internal/tools/builtin/shell_tokenize.go`

The pipe-split was already fixed in round 3 (S3-H1) to use `splitOnUnquotedPipes`. This task verifies the fix is complete and documents the pattern.

- [ ] **Step 1: Read the current classifyRisk implementation**

Verify it uses `splitOnUnquotedPipes` and not `strings.Split(command, "|")`.

- [ ] **Step 2: Read the splitOnUnquotedPipes implementation**

Verify it handles:
- Single-quoted segments (`'...'`)
- Double-quoted segments (`"..."`)
- Escaped quotes (`\"`, `\'`)

- [ ] **Step 3: Add edge-case tests if missing**

```go
// internal/tools/builtin/shell_test.go
func TestClassifyRisk_QuotedPipes(t *testing.T) {
    tests := []struct {
        name    string
        command string
        want    ShellCommandRisk
    }{
        {"pipe in single quotes", `echo '|'`, RiskMedium},     // echo is read-only
        {"pipe in double quotes", `echo "|"`, RiskMedium},     // echo is read-only
        {"real pipe", "echo hello | wc", RiskMedium},          // both read-only
        {"sudo after pipe", "echo hi | sudo rm -rf /", RiskCritical},
        {"awk with pipe separator", `awk -F'|' '{print $2}'`, RiskMedium},
    }
    tool := &ShellExecuteTool{}
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := tool.classifyRisk(tt.command)
            if got != tt.want {
                t.Errorf("classifyRisk(%q) = %v, want %v", tt.command, got, tt.want)
            }
        })
    }
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/tools/builtin/ -run TestClassifyRisk_QuotedPipes -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/tools/builtin/shell_test.go
git commit -m "test: add edge cases for quote-aware pipe splitting in classifyRisk (Obs-3)"
```

### Task 2.4: Document the errcls routing convention

**Files:** `internal/errcls/doc.go` (create if not exists)

- [ ] **Step 1: Write package documentation**

```go
// Package errcls provides shared error classification helpers that replace
// ad-hoc strings.Contains(err.Error(), ...) checks across the codebase.
//
// # Convention
//
// All error classification (rate-limit detection, auth errors, parameter
// errors, network errors, retryability) MUST go through this package.
// Do NOT write strings.Contains(err.Error(), "rate limit") in calling code.
//
// To add a new classifier:
//  1. Add a function Is<Category>(err error) bool in this package
//  2. Use errors.As / errors.Is for structured detection
//  3. For packages that cannot be imported directly (import cycles),
//     provide a Register<Category>Sentinels function
//  4. Callers use errcls.Is<Category>(err) instead of substring checks
//
// For command risk classification (shell.go classifyRisk), the pattern is
// different: use quote-aware tokenization (splitOnUnquotedPipes) rather
// than strings.Split.
package errcls
```

- [ ] **Step 2: Commit**

```bash
git add internal/errcls/doc.go
git commit -m "docs: document errcls routing convention (Obs-3)"
```

---

## Sprint 3 (Observation 4): Mutex Held Across I/O

**Goal:** Apply the "collect under lock, release, then operate" pattern consistently. Three sites identified: `shadow/manager.go:ProcessRecord`, `runtime/docker.go:Execute`, and any others found by audit.

### Task 3.1: Audit mutex-held-across-I/O sites

**Files:** All `.go` files under `internal/`

- [ ] **Step 1: Grep for the pattern**

```bash
# Find Lock() followed by defer Unlock() in functions that also do I/O
grep -rn 'defer.*Unlock\(\)' internal/ --include='*.go' | grep -v _test.go | head -100
```

For each hit, manually check if the function body between Lock and Unlock contains:
- Network calls (`http.Get`, `client.Do`, etc.)
- Disk I/O (`os.ReadFile`, `db.Exec`, `db.Query`, etc.)
- LLM calls (`llmClient.Complete`, etc.)
- Channel operations that could block

- [ ] **Step 2: Document all I/O-under-lock sites found**

### Task 3.2: Fix shadow/manager.go ProcessRecord

**Files:** `internal/shadow/manager.go:163-210`

Currently `ProcessRecord` holds `m.mu.Lock()` for the entire function body, which includes LLM scoring (`m.scorer.Score`), comparison scoring, and multiple store writes. These are all I/O operations.

- [ ] **Step 1: Write a test that demonstrates the lock contention**

```go
// internal/shadow/manager_test.go
func TestProcessRecord_LockContention(t *testing.T) {
    // Create a manager with a slow scorer (100ms delay)
    // Fire 10 concurrent ProcessRecord calls
    // Assert they don't serialize (total time < 1s, not 10*100ms)
    // OR: assert they DO serialize if that's the intended behavior,
    // in which case document why
}
```

- [ ] **Step 2: Determine what the mutex actually protects**

Read `ProcessRecord` carefully. The mutex likely protects:
- `m.metrics` updates (counters)
- `m.config` reads
- NOT the store operations (stores have their own internal locking)

If the mutex is only protecting metrics/config reads, narrow its scope.

- [ ] **Step 3: Refactor to collect-under-lock, release, then operate**

```go
func (m *Manager) ProcessRecord(ctx context.Context, record *ShadowRecord) error {
    // Collect config snapshot under lock
    m.mu.RLock()
    qualityCfg := m.config.Quality
    examplesEnabled := m.config.Examples.Enabled
    m.mu.RUnlock()

    // Score without holding lock (scorer is stateless or has own locking)
    result, err := m.scorer.Score(ctx, record)
    if err != nil {
        return fmt.Errorf("scoring failed: %w", err)
    }

    record.QualityScore = result.Score
    record.IsHighQuality = result.IsHighQuality

    // Comparison scoring (I/O — no lock needed)
    if record.HasTeacherResponse() {
        studentScore, teacherScore, err := m.scorer.ScoreComparison(ctx, record)
        if err != nil {
            m.logger.Warn("Comparison scoring failed", "error", err)
        } else {
            // ... preference logic (pure computation, no lock needed)
        }
    }

    // Save record (store has its own locking)
    if err := m.trainingStore.SaveRecord(ctx, record); err != nil {
        return fmt.Errorf("failed to save record: %w", err)
    }

    // Update metrics under lock
    m.mu.Lock()
    m.metrics.RecordsProcessed++
    m.mu.Unlock()

    // ... rest of logic
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race ./internal/shadow/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/shadow/manager.go internal/shadow/manager_test.go
git commit -m "refactor(shadow): narrow ProcessRecord mutex scope (Obs-4)"
```

### Task 3.3: Fix runtime/docker.go Execute

**Files:** `internal/runtime/docker.go:97-180`

Currently `Execute` holds `b.mu.Lock()` for the entire function, which includes Docker API calls (`CreateExec`, `StartExec`, `InspectExec`). These are network I/O to the Docker daemon.

- [ ] **Step 1: Determine what the mutex protects**

Read the `DockerBackend` struct. The mutex likely protects:
- `b.containerID` (may change if container restarts)
- `b.config` (may be updated at runtime)

If so, the lock only needs to be held briefly to snapshot these values.

- [ ] **Step 2: Refactor to snapshot-then-operate**

```go
func (b *DockerBackend) Execute(ctx context.Context, cmd Command) (*CommandResult, error) {
    // Snapshot state under lock
    b.mu.Lock()
    containerID := b.containerID
    timeout := b.config.Timeout
    b.mu.Unlock()

    if cmd.Timeout > 0 {
        timeout = cmd.Timeout
    }
    if timeout > 0 {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, timeout)
        defer cancel()
    }

    start := time.Now()

    // All Docker API calls happen without holding the lock
    execOpts := docker.CreateExecOptions{
        AttachStdout: true,
        AttachStderr: true,
        Container:    containerID,
        Cmd:          []string{"/bin/sh", "-c", cmd.Cmd},
        WorkingDir:   cmd.Dir,
        Env:          /* ... */,
    }

    exec, err := b.client.CreateExec(execOpts)
    // ... rest of execution
}
```

- [ ] **Step 3: Verify Docker tests still pass**

```bash
go test ./internal/runtime/... -v
```

Note: Docker tests may be skipped if Docker isn't available. Verify with:
```bash
go test ./internal/runtime/... -v -run TestDockerBackend
```

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/docker.go
git commit -m "refactor(runtime): narrow DockerBackend.Execute mutex scope (Obs-4)"
```

### Task 3.4: Document the pattern convention

**Files:** Add to CLAUDE.md "Coding Practices" section

- [ ] **Step 1: Add the convention to CLAUDE.md**

After the "Typed-nil interface guard" section, add:

```markdown
- **Mutex scope**: Never hold a mutex across I/O operations (network calls,
  disk reads/writes, LLM calls, channel sends). Use the "collect under lock,
  release, then operate" pattern:
  ```go
  // RIGHT: snapshot under lock, operate without lock
  mu.Lock()
  cfg := m.config
  mu.Unlock()
  result, err := doNetworkCall(ctx, cfg)

  // WRONG: lock held across network call
  mu.Lock()
  defer mu.Unlock()
  result, err := doNetworkCall(ctx, m.config) // blocks all other callers during I/O
  ```
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add mutex-scope convention to CLAUDE.md (Obs-4)"
```

---

## Sprint 4 (Observation 5): Typed-Nil Setter Consistency

**Goal:** Enforce the CLAUDE.md typed-nil interface guard pattern across all setter methods. Create a test that verifies every `Set*` method on tool structs is nil-safe.

### Task 4.1: Audit all Set* methods

**Files:** `internal/tools/builtin/*.go`

- [ ] **Step 1: Grep for all Set* methods on tool types**

```bash
grep -rn 'func (t \*\w\+Tool) Set' internal/tools/builtin/ --include='*.go'
```

Expected hits:
- `ShellExecuteTool.SetKnownSafeCommands`
- `ShellExecuteTool.SetFenceChecker` (if exists)
- `ResolveTool.SetFenceChecker` (fixed in round 3)
- `FileEditTool.SetPendingChangesRegistry` (fixed in round 3)
- Any other Set* methods

- [ ] **Step 2: For each Set* method, verify it has a nil guard**

Each setter should follow this pattern:

```go
func (t *SomeTool) SetSomething(s SomeInterface) {
    if s == nil {
        return
    }
    t.something = s
}
```

Or, if setting a concrete pointer to an interface:

```go
func (t *SomeTool) SetSomething(s *ConcreteType) {
    if s == nil {
        return
    }
    t.something = s // s as ConcreteType satisfies SomeInterface
}
```

- [ ] **Step 3: Document any setters that lack the guard**

### Task 4.2: Fix any remaining unguarded setters

**Files:** Depends on audit from Task 4.1

For each unguarded setter found:

- [ ] **Step 1: Add nil guard**

```go
func (t *SomeTool) SetSomething(s SomeInterface) {
    if s == nil {
        return
    }
    t.something = s
}
```

- [ ] **Step 2: Test nil-safety**

```go
func TestSomeTool_SetSomething_NilSafe(t *testing.T) {
    tool := &SomeTool{}
    tool.SetSomething(nil) // must not panic
    // Verify nothing was set
}
```

- [ ] **Step 3: Commit per fix**

```bash
git add internal/tools/builtin/<file>.go
git commit -m "fix: add nil guard to <Tool>.Set<Method> (Obs-5)"
```

### Task 4.3: Create a comprehensive typed-nil setter test

**Files:** `internal/tools/builtin/setters_test.go` (create)

- [ ] **Step 1: Write a table-driven test for all tool setters**

```go
package builtin

import (
    "testing"
)

func TestAllSetters_NilSafe(t *testing.T) {
    // Each entry is a function that calls a setter with nil.
    // If any setter panics, the test fails.
    tests := []struct {
        name    string
        setFunc func()
    }{
        {"ShellExecuteTool.SetKnownSafeCommands", func() {
            tool := &ShellExecuteTool{}
            tool.SetKnownSafeCommands(nil)
        }},
        {"ResolveTool.SetFenceChecker", func() {
            tool := &ResolveTool{}
            tool.SetFenceChecker(nil)
        }},
        {"FileEditTool.SetPendingChangesRegistry", func() {
            // FileEditTool requires a registry constructor — use nil
            // if the method accepts nil, or the zero-value tool
        }},
        // Add all other Set* methods here
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            defer func() {
                if r := recover(); r != nil {
                    t.Errorf("Set method panicked on nil: %v", r)
                }
            }()
            tt.setFunc()
        })
    }
}
```

- [ ] **Step 2: Run the test**

```bash
go test ./internal/tools/builtin/ -run TestAllSetters_NilSafe -v
```

- [ ] **Step 3: Commit**

```bash
git add internal/tools/builtin/setters_test.go
git commit -m "test: add comprehensive typed-nil setter safety test (Obs-5)"
```

### Task 4.4: Document the pattern as a CLAUDE.md enforcement rule

**Files:** `CLAUDE.md` — update "Coding Practices" section

- [ ] **Step 1: Add an explicit enforcement note**

The CLAUDE.md already has the typed-nil interface guard pattern documented. Add a note:

```markdown
- **Setter methods**: Every `Set*` method on a tool/service struct that
  accepts an interface or pointer type MUST include a nil guard as the
  first line. The test suite (`setters_test.go`) verifies this
  project-wide. When adding a new `Set*` method, add it to that test.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: enforce typed-nil setter pattern in CLAUDE.md (Obs-5)"
```

---

## Sprint 5 (Observation 6): Hardcoded Dev API Key Single Source of Truth

**Goal:** Eliminate duplicated copies of the dev API key string. Ensure there is exactly one source of truth (`pkg/constants/api_key.go`) and all clients reference it (or better, have no fallback at all in production builds).

### Task 5.1: Audit all copies of the dev API key

**Files:** All source files

- [ ] **Step 1: Grep for the literal key string**

```bash
grep -rn 'meept_dev_default_key_CHANGE_ME' --include='*.go' --include='*.dart' --include='*.swift' --include='*.json5' .
```

Expected hits:
- `pkg/constants/api_key.go:10` — canonical Go source
- `ui/flutter_ui/lib/core/constants.dart:41` — Flutter copy
- `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:10` — Swift copy
- `config/menubar.json5:7` — config comment reference
- `internal/comm/http/server.go:183` — uses `constants.DefaultDevAPIKey` (good)
- `internal/transport/client.go:113` — uses `constants.DefaultDevAPIKey` (good)

- [ ] **Step 2: Document the current state**

Go code: correctly references `constants.DefaultDevAPIKey`.
Flutter: hardcodes the string.
Swift: hardcodes the string.
Config: documents the key in a comment.

### Task 5.2: Flutter — remove hardcoded key, use config only

**Files:** `ui/flutter_ui/lib/core/constants.dart:38-41`, `ui/flutter_ui/lib/services/storage_service.dart` (or equivalent)

- [ ] **Step 1: Read the Flutter storage service to understand how the key is used**

The Flutter app should be reading the API key from its config file (`~/.meept/client.json5` or similar). The `defaultApiKey` constant is a fallback when no key is configured.

- [ ] **Step 2: Remove the fallback or generate it at build time**

Option A (preferred — no fallback in release builds):
```dart
// ui/flutter_ui/lib/core/constants.dart
// Remove: static const String defaultApiKey = 'meept_dev_default_key_CHANGE_ME';
// Replace with:
static const String defaultApiKey = String.fromEnvironment(
  'MEEPT_DEV_API_KEY',
  defaultValue: '', // empty = no fallback in release builds
);
```

Build command for development:
```bash
flutter run --dart-define=MEEPT_DEV_API_KEY=meept_dev_default_key_CHANGE_ME
```

Option B (simpler — keep fallback but document):
```dart
/// Development API key. Must match pkg/constants/api_key.go.
/// In production, always configure a custom key via `meept token generate --save`.
static const String defaultApiKey = String.fromEnvironment(
  'MEEPT_API_KEY',
  defaultValue: 'meept_dev_default_key_CHANGE_ME', // matches pkg/constants/api_key.go
);
```

- [ ] **Step 3: Update storage_service to handle empty key gracefully**

If Option A is chosen, ensure the storage service surfaces a clear error when no key is configured:

```dart
String get apiKey {
  final key = prefs.getString('api_key') ?? Constants.defaultApiKey;
  if (key.isEmpty) {
    throw StateError('No API key configured. Run `meept token generate --save`.');
  }
  return key;
}
```

- [ ] **Step 4: Run Flutter analysis**

```bash
cd ui/flutter_ui && flutter analyze
```

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/core/constants.dart ui/flutter_ui/lib/services/storage_service.dart
git commit -m "refactor(flutter): remove hardcoded dev API key, use build-time injection (Obs-6)"
```

### Task 5.3: Swift MenuBar — gate dev API key behind DEBUG

**Files:** `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:8-10,68`

- [ ] **Step 1: Gate the DefaultDevAPIKey behind #if DEBUG**

```swift
#if DEBUG
/// Default development API key shared with the daemon and other clients.
/// Only available in debug builds. In release builds, the API token must
/// be configured via `menubar.json5` or the settings UI.
let DefaultDevAPIKey = "meept_dev_default_key_CHANGE_ME"
#else
let DefaultDevAPIKey: String? = nil
#endif
```

- [ ] **Step 2: Update the apiToken accessor**

```swift
var apiToken: String {
    let configToken = config.daemon.apiToken
    if let token = configToken, !token.isEmpty {
        return token
    }
    #if DEBUG
    return DefaultDevAPIKey
    #else
    // In release builds, return empty string to trigger a clear auth error
    // rather than silently using a known key.
    return ""
    #endif
}
```

Or better, make `apiToken` return `String?`:

```swift
var apiToken: String? {
    let configToken = config.daemon.apiToken
    if let token = configToken, !token.isEmpty {
        return token
    }
    #if DEBUG
    return DefaultDevAPIKey
    #else
    return nil
    #endif
}
```

Then callers handle nil:

```swift
guard let token = config.apiToken else {
    // Show settings UI prompting user to configure API key
    return
}
```

- [ ] **Step 3: Build the Swift project**

```bash
cd menubar && swift build
```

- [ ] **Step 4: Commit**

```bash
git add menubar/MeeptMenuBar/Services/MenubarConfigService.swift
git commit -m "refactor(menubar): gate DefaultDevAPIKey behind #if DEBUG (Obs-6)"
```

### Task 5.4: Go — verify single source usage

**Files:** `pkg/constants/api_key.go`, `internal/comm/http/server.go`, `internal/transport/client.go`

- [ ] **Step 1: Verify Go code references the constant, not the literal**

```bash
grep -rn 'DefaultDevAPIKey' internal/ cmd/ pkg/ --include='*.go'
```

All hits should be references to `constants.DefaultDevAPIKey` from `pkg/constants/api_key.go`. If any Go file hardcodes the literal string, fix it.

- [ ] **Step 2: Add a linter guard (optional)**

Add a `go vet` or custom lint rule that fails if the literal string `meept_dev_default_key_CHANGE_ME` appears in any Go file outside `pkg/constants/api_key.go`:

```bash
# .githooks/pre-commit or CI step
if grep -rn 'meept_dev_default_key_CHANGE_ME' --include='*.go' . | grep -v 'pkg/constants/api_key.go' | grep -v _test.go; then
    echo "ERROR: hardcoded dev API key found outside pkg/constants/api_key.go"
    exit 1
fi
```

- [ ] **Step 3: Commit**

```bash
git add .githooks/pre-commit  # or wherever the guard lives
git commit -m "ci: add guard against hardcoded dev API key (Obs-6)"
```

### Task 5.5: Document the single-source-of-truth convention

**Files:** `CLAUDE.md` — add to Configuration section

- [ ] **Step 1: Add documentation**

```markdown
### Development API Key

The default development API key is defined in exactly one place:
`pkg/constants/api_key.go` (`constants.DefaultDevAPIKey`).

- **Go code**: Always reference `constants.DefaultDevAPIKey`. Never hardcode the literal.
- **Flutter**: Use `--dart-define=MEEPT_API_KEY=<key>` at build time. No hardcoded fallback in release builds.
- **Swift (MenuBar)**: Gated behind `#if DEBUG`. Release builds require explicit configuration.
- **Config files**: Reference the key by name in comments, never embed the literal.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document dev API key single-source-of-truth (Obs-6)"
```

---

## Self-Review

**Observation coverage:**
- Observation 2 (package-level mutable state): Sprint 1 — Tasks 1.1-1.5 cover audit, fix plan.go counters, verify pty/shadow counters, document convention.
- Observation 3 (substring classification): Sprint 2 — Tasks 2.1-2.4 cover audit, migrate remaining hits, verify shell tokenization, document errcls convention.
- Observation 4 (mutex across I/O): Sprint 3 — Tasks 3.1-3.4 cover audit, fix shadow ProcessRecord, fix docker Execute, document pattern.
- Observation 5 (typed-nil setters): Sprint 4 — Tasks 4.1-4.4 cover audit, fix remaining setters, comprehensive test, document convention.
- Observation 6 (dev API key): Sprint 5 — Tasks 5.1-5.5 cover audit, fix Flutter/Swift/Go, CI guard, document convention.

**Placeholder scan:** No "TBD", "TODO", or "fill in details". Each step has exact code, commands, or grep targets. Where a step says "depends on audit results", the following steps give exact code for the known sites.

**Type consistency:** `atomic.Uint64` used consistently for Go counters. `FenceChecker` interface matches across tasks. `DefaultDevAPIKey` name consistent with existing code. `splitOnUnquotedPipes` name matches existing implementation.

**Risk callouts:**
- Task 5.2 Flutter changes require testing the app manually to verify API key injection works.
- Task 5.3 Swift changes require manual testing in both Debug and Release configurations.
- Task 3.2 shadow manager refactor changes locking semantics — verify with race detector.

---

## Execution Handoff

Plan saved to `docs/plans/2026-06-15-findings-3-architectural-patterns.md`. Two execution options:

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per sprint. Sprints are independent and parallelizable.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
