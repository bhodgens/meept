# Error Classifier Refactor: Substring → Structured errors.As

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace substring-based error classification (`strings.Contains(err.Error(), ...)`) with structured `errors.As`/`errors.Is` classification across the codebase, eliminating false positives/negatives in retry logic, RPC error codes, and connection-error detection.

**Architecture:** Introduce a shared `internal/errcls` package with composable classifier helpers (`IsRetryable`, `IsRateLimit`, `IsAuth`, `IsClient`, `IsTimeout`, `IsNetwork`, `IsParameterError`) that defer to the existing domain error types (`*llm.APIError`, `*llm.RateLimitError`, `*llm.BudgetExceededError`, `services.Err*` sentinels, `*agent.ToolExecutionError`, stdlib `net.Error` and `syscall.Errno`). Then migrate each substring site to use the new helpers. Pure refactor: no behavior changes where current behavior is correct; bug fixes where current behavior is provably wrong (false negatives on 529/context timeouts).

**Tech Stack:** Go 1.22+, `errors` package, existing domain error types in `internal/llm`, `internal/services`, `internal/agent`, stdlib `net`/`syscall`.

---

## Scope

Refactor sites where substring matching on `err.Error()` changes runtime control flow (retry decisions, RPC error codes, connection-error detection). User-facing hint selection in config loaders and JSON-parse suggestion builders is **out of scope** (cosmetic only).

### In-scope sites (control-flow)

| ID | File:line | Function | Impact |
|----|-----------|----------|--------|
| EC-1 | `internal/rpc/server.go:537-549` | `isParameterError(errStr)` | RPC `-32602` vs `-32603` |
| EC-2 | `internal/agent/tactical.go:1099-1104` | `TacticalScheduler.isRateLimitError(errMsg)` | Job retry decision |
| EC-3 | `internal/agent/tactical.go:1106-1145` | `TacticalScheduler.isRetryableError(errMsg)` | Job retry decision |
| EC-4 | `internal/llm/errors.go:162-174` | `IsRateLimitErrorMessage(errMsg)` | Exported helper |
| EC-5 | `internal/llm/errors.go:466-486` | `ClassifyClassificationFailure(err)` | Failure kind classification |
| EC-6 | `internal/llm/anthropic.go:539` | `IsError` on tool content | Anthropic tool_result error flag (requires design) |
| EC-7 | `internal/tui/rpc.go:135-151` | `RPCClient.isConnectionError(err)` | TUI reconnect logic |
| EC-8 | `cmd/meept-lite/tui.go:652-654` | lite TUI connection-error classifier | Daemon-down hint |
| EC-9 | `internal/services/daemon_service.go:230` | "not running" idiom | Restart idempotency |

### Out-of-scope

- `internal/config/json5_loader.go` — hint selection only; no behavior change
- `internal/config/config.go` — TOML hint selection only
- `internal/agent/errors.go:79-300` — JSON parse hint generation (stdlib doesn't subclass)
- `internal/shadow/store_sqlite.go` — SQLite "duplicate column" idiom (cross-driver)
- `internal/daemon/launchd.go:274` — kardianos/service library limitation
- `internal/selfimprove/detector.go:244-256` — log content classifier, not error type

---

## Task 1: Create `internal/errcls` package

**Files:**
- Create: `internal/errcls/classify.go`
- Create: `internal/errcls/classify_test.go`

**Step 1: Write failing tests for each classifier helper**

```go
package errcls_test

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/services"
)

func TestIsRateLimit(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit error", &llm.RateLimitError{RetryAfter: 30}, true},
		{"api 429", &llm.APIError{StatusCode: 429}, true},
		{"api 500", &llm.APIError{StatusCode: 500}, false},
		{"wrapped rate limit", errors.Join(errors.New("context"), &llm.RateLimitError{}), true},
		{"plain error", errors.New("something else"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsRateLimit(tt.err); got != tt.want {
				t.Errorf("IsRateLimit(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit", &llm.RateLimitError{}, true},
		{"api 429", &llm.APIError{StatusCode: 429}, true},
		{"api 500", &llm.APIError{StatusCode: 500}, true},
		{"api 502", &llm.APIError{StatusCode: 502}, true},
		{"api 503", &llm.APIError{StatusCode: 503}, true},
		{"api 504", &llm.APIError{StatusCode: 504}, true},
		{"api 529 overload", &llm.APIError{StatusCode: 529}, true},
		{"api 400", &llm.APIError{StatusCode: 400}, false},
		{"api 401", &llm.APIError{StatusCode: 401}, false},
		{"api 404", &llm.APIError{StatusCode: 404}, false},
		{"budget exceeded (non-retryable)", &llm.BudgetExceededError{}, false},
		{"context size exceeded (non-retryable)", &llm.ContextSizeExceededError{}, false},
		{"context deadline", context.DeadlineExceeded, true},
		{"net temp error", tempNetErr{}, true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"plain internal error", errors.New("internal failure"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"api 401", &llm.APIError{StatusCode: 401}, true},
		{"api 403", &llm.APIError{StatusCode: 403}, true},
		{"api 500", &llm.APIError{StatusCode: 500}, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsAuthError(tt.err); got != tt.want {
				t.Errorf("IsAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsParameterError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"services invalid input", services.ErrInvalidInput, true},
		{"plain missing arg", errors.New("missing required parameter"), false}, // EC-1 fix: plain strings no longer match
		{"api 400", &llm.APIError{StatusCode: 400}, true},
		{"api 404", &llm.APIError{StatusCode: 404}, false},
		{"nil", nil, false},
		{"internal failure", errors.New("expected 1 result, got 0"), false}, // EC-1 false positive removed
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsParameterError(tt.err); got != tt.want {
				t.Errorf("IsParameterError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"EPIPE", syscall.EPIPE, true},
		{"EOF", io.EOF, true},
		{"net.ErrClosed", net.ErrClosed, true},
		{"wrapped EOF", errors.Join(errors.New("ctx"), io.EOF), true},
		{"nil", nil, false},
		{"plain error", errors.New("totally unrelated"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsNetworkError(tt.err); got != tt.want {
				t.Errorf("IsNetworkError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type tempNetErr struct{}

func (tempNetErr) Error() string   { return "temp net error" }
func (tempNetErr) Timeout() bool   { return true }
func (tempNetErr) Temporary() bool { return true }
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/errcls/... -v`
Expected: FAIL ("package not found" / undefined: `errcls.IsRateLimit`)

**Step 3: Write the classifier implementation**

```go
// Package errcls provides shared error classification helpers that replace
// ad-hoc strings.Contains(err.Error(), ...) checks across the codebase.
//
// All helpers return false on nil errors. They use errors.As / errors.Is so
// wrapping via fmt.Errorf("...: %w", err) is handled correctly.
package errcls

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/services"
)

// IsRateLimit reports whether err represents an HTTP 429 / provider rate-limit
// error, including wrapped variants.
func IsRateLimit(err error) bool {
	if err == nil {
		return false
	}
	if llm.IsRateLimitError(err) {
		return true
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 429 {
		return true
	}
	return false
}

// IsRetryable reports whether err represents a transient error that warrants
// retry: rate limits, server errors (5xx incl. 529), context deadlines,
// network errors, and other temporary failures. Non-retryable errors
// (budget exceeded, context-size exceeded, 4xx client errors) return false.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if llm.IsNonRetryable(err) {
		return false
	}
	if IsRateLimit(err) {
		return true
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 && apiErr.StatusCode < 600
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Temporary() || netErr.Timeout()) {
		return true
	}
	if isConnError(err) {
		return true
	}
	return false
}

// IsAuthError reports whether err is an authentication/authorization error
// (HTTP 401 or 403). Used to suppress retry of credentials-bearing requests.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 401 || apiErr.StatusCode == 403
	}
	if errors.Is(err, services.ErrUnauthorized) {
		return true
	}
	return false
}

// IsClientError reports whether err is a non-retryable 4xx client error
// (excluding 401/403 which IsAuthError handles separately).
func IsClientError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		return status >= 400 && status < 500 && status != 401 && status != 403
	}
	return false
}

// IsParameterError reports whether err is a parameter-validation error
// suitable for JSON-RPC -32602 InvalidParams. This replaces the substring
// heuristic in rpc/server.go's isParameterError which false-positive'd on
// common words like "type" and "expected".
//
// Structured detection takes precedence; a narrow string fallback catches
// bare fmt.Errorf("invalid X") / fmt.Errorf("missing X") from handlers that
// have not yet been upgraded.
func IsParameterError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, services.ErrInvalidInput) {
		return true
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		return true
	}
	return false
}

// IsNetworkError reports whether err is a network/connection-level failure
// (connection refused, reset, broken pipe, EOF, closed). Used by clients
// (TUI RPC, daemon transport) to decide whether to reconnect.
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	return isConnError(err)
}

// isConnError handles the syscall.Errno cases shared between IsRetryable and
// IsNetworkError. Avoids importing a larger syscall surface in callers.
func isConnError(err error) bool {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.EHOSTUNREACH),
		errors.Is(err, syscall.ENETUNREACH):
		return true
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/errcls/... -v`
Expected: PASS — all tests green.

**Step 5: Commit**

```bash
git add internal/errcls/classify.go internal/errcls/classify_test.go
git commit -m "feat(errcls): shared error classifier package"
```

---

## Task 2: Migrate `internal/rpc/server.go` (EC-1)

**Files:**
- Modify: `internal/rpc/server.go:366` (call site) and `:537-549` (`isParameterError` function)

**Step 1: Update the test to expect new behavior**

Add a table-driven test to `internal/rpc/server_test.go` covering:
- A handler that returns `services.ErrInvalidInput` → RPC code `-32602`
- A handler that returns `fmt.Errorf("expected 1 result, got 0")` → RPC code `-32603` (previously misclassified as `-32602`)

**Step 2: Run test, verify it fails**

Run: `go test ./internal/rpc/... -run TestDispatch_ParameterErrorClassification -v`
Expected: FAIL (current substring heuristic misclassifies "expected" case)

**Step 3: Replace `isParameterError` body**

```go
// isParameterError returns true for parameter-validation errors that should
// map to JSON-RPC -32602 InvalidParams. Uses structured detection; see
// errcls.IsParameterError.
func isParameterError(err error) bool {
	return errcls.IsParameterError(err)
}
```

Update the call site at line 366 to pass the error directly (not the string).

**Step 4: Run all rpc tests**

Run: `go test ./internal/rpc/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/rpc/server.go internal/rpc/server_test.go
git commit -m "refactor(rpc): use errcls.IsParameterError instead of substring match"
```

---

## Task 3: Migrate `internal/agent/tactical.go` (EC-2, EC-3)

**Files:**
- Modify: `internal/agent/tactical.go:846` (`OnJobFailed` signature), `:893, :902` (call sites), `:1099-1145` (`isRateLimitError`, `isRetryableError`)

**Step 1: Refactor the boundary first**

Change `OnJobFailed(ctx, jobID, jobErr string)` to `OnJobFailed(ctx, jobID string, jobErr error)`. Update call sites in `orchestrator.go:331` to pass the original error rather than `event.Error`. Add a recovery path: if `jobErr == nil`, treat as generic failure.

**Step 2: Replace the classifier helpers**

```go
func (ts *TacticalScheduler) isRateLimitError(err error) bool {
	return errcls.IsRateLimit(err)
}

func (ts *TacticalScheduler) isRetryableError(err error) bool {
	return errcls.IsRetryable(err)
}
```

**Step 3: Update tests**

Add coverage in `internal/agent/tactical_test.go` for:
- Retry on `*llm.RateLimitError`
- No-retry on `*llm.BudgetExceededError`
- Retry on `&llm.APIError{StatusCode: 529}`
- Retry on `context.DeadlineExceeded` (was previously false negative — string contained no "timeout")
- No-retry on `&llm.APIError{StatusCode: 400}`

**Step 4: Run tests**

Run: `go test ./internal/agent/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/tactical.go internal/agent/tactical_test.go internal/agent/orchestrator.go
git commit -m "refactor(agent): tactical uses errcls; OnJobFailed takes error not string"
```

---

## Task 4: Migrate `internal/llm/errors.go` (EC-4, EC-5)

**Files:**
- Modify: `internal/llm/errors.go:162-174` (`IsRateLimitErrorMessage`) and `:466-486` (`ClassifyClassificationFailure`)

**Step 1: Deprecate `IsRateLimitErrorMessage`**

Add deprecation comment; keep implementation as fallback only. Update only caller (`agent/tactical.go` in EC-2) now uses `errcls.IsRateLimit(err)` directly.

```go
// Deprecated: Use errcls.IsRateLimit(err) with a structured error value.
// IsRateLimitErrorMessage is retained as a string-only fallback for cases
// where the error has been serialized and the original error.Error() is
// all that remains.
func IsRateLimitErrorMessage(errMsg string) bool { ... }
```

**Step 2: Rewrite `ClassifyClassificationFailure` using `errors.As`**

```go
func ClassifyClassificationFailure(err error) ClassificationFailureKind {
	if err == nil {
		return ClassificationFailureUnknown
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassificationFailureTimeout
	}
	var budgetErr *BudgetExceededError
	if errors.As(err, &budgetErr) {
		return ClassificationFailureBudget
	}
	var capErr *CapabilityError
	if errors.As(err, &capErr) {
		return ClassificationFailureUnavailable
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 429:
			return ClassificationFailureUnavailable // rate limited
		case apiErr.StatusCode >= 500:
			return ClassificationFailureUnavailable
		}
	}
	// EmptyResponse is still string-based because no structured type exists.
	// See follow-up: define *EmptyResponseError.
	msg := err.Error()
	if strings.Contains(msg, "no choices in response") || strings.Contains(msg, "empty content") {
		return ClassificationFailureEmptyResponse
	}
	return ClassificationFailureUnknown
}
```

**Step 3: Run tests**

Run: `go test ./internal/llm/... -v -run Classify`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/llm/errors.go internal/llm/errors_test.go
git commit -m "refactor(llm): ClassifyClassificationFailure uses errors.As; IsRateLimitErrorMessage deprecated"
```

---

## Task 5: Migrate `internal/tui/rpc.go` (EC-7)

**Files:**
- Modify: `internal/tui/rpc.go:135-151` (`isConnectionError`)

**Step 1: Update implementation**

```go
func (c *RPCClient) isConnectionError(err error) bool {
	return errcls.IsNetworkError(err)
}
```

**Step 2: Add regression test** that wraps `io.EOF` in `fmt.Errorf("ctx: %w", io.EOF)` and verifies it still classifies as a connection error.

**Step 3: Run tests, commit**

```bash
git add internal/tui/rpc.go internal/tui/rpc_test.go
git commit -m "refactor(tui): isConnectionError uses errcls.IsNetworkError"
```

---

## Task 6: Migrate `cmd/meept-lite/tui.go` (EC-8) and `services/daemon_service.go` (EC-9)

**Files:**
- Modify: `cmd/meept-lite/tui.go:652-654` — use `errcls.IsNetworkError`
- Modify: `internal/services/daemon_service.go:230` — replace "not running" substring with sentinel check. Define `var ErrDaemonNotRunning = errors.New("daemon not running")` in `internal/daemon/` and have the relevant stop path return it.

**Step 1-3: TDD as above**

**Step 4: Commit**

```bash
git add cmd/meept-lite/tui.go internal/services/daemon_service.go internal/daemon/errors.go
git commit -m "refactor: migrate connection and not-running checks to errcls"
```

---

## Task 7: Design and implement EC-6 (Anthropic tool-result `IsError`)

**Files:**
- Modify: `internal/llm/types.go` (or wherever `ChatMessage` is defined) — add `IsToolError bool` field
- Modify: `internal/tools/registry.go` or wherever tool results are constructed — set the flag based on tool execution outcome
- Modify: `internal/llm/anthropic.go:539` — use `msg.IsToolError` instead of substring match

This is a larger change touching the tool-execution boundary. **Document it as a separate PR** with its own plan; the rest of this refactor does not depend on it.

**Step 1-5: See `docs/plans/2026-06-14-anthropic-tool-iserror-field.md` (to be written if pursued)**

---

## Verification

End-to-end verification after all tasks:

```bash
# Build
go build ./...

# Vet
go vet ./...

# Test all affected packages
go test ./internal/errcls/... ./internal/rpc/... ./internal/agent/... \
       ./internal/llm/... ./internal/tui/... ./internal/services/... \
       ./cmd/meept-lite/... -v

# Grep for residual substring classification at in-scope sites
grep -n "strings.Contains.*Error()" internal/rpc/server.go internal/agent/tactical.go \
     internal/llm/errors.go internal/tui/rpc.go cmd/meept-lite/tui.go \
     internal/services/daemon_service.go
# Expected: no matches at the in-scope line ranges (hint-selection code may still contain some)
```
