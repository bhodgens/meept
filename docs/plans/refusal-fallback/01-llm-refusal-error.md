# LLM Refusal Detection - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Typed `RefusalError` plus signal-to-error mapping in internal/llm.
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Goal

Meept cannot react to a refusal it cannot identify. This leaf defines the
`*llm.RefusalError` type and two pure detection functions so that every
provider client can classify refusal signals (Anthropic `stop_reason:
"refusal"`, OpenAI-compatible `finish_reason: "content_filter"`, typed
safeguard error bodies) into one sentinel type. It mirrors the existing
`QuotaResetError` pattern exactly.

## Context

Meept is a Go daemon. The LLM layer (internal/llm) already has one
"recoverable but not retryable" typed error: `QuotaResetError`
(internal/llm/errors_quota.go). The client retry loops check
`NonRetryable()` BEFORE any retryable-status check (see the quota
early-exit placement comment in internal/llm/client.go around line 1966) —
new non-retryable types ride the same path for free once they implement
`NonRetryableError`.

Key files to understand before implementing:
- internal/llm/errors_quota.go - the pattern to mirror (type, Error(),
  UserMessage(), Unwrap, NonRetryable, compile-time interface assertion)
- internal/llm/models.go - Response struct carries FinishReason (line ~183)
- internal/llm/client.go - openai-compatible parsing; FinishReason flows
  from choice.FinishReason (line ~1831) and streaming (line ~2347)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/llm/errors_refusal.go
package llm

type RefusalError struct {
    ProviderID   string
    ModelID      string
    Source       string // "finish_reason" | "stop_reason" | "error_body"
    FinishReason string // raw signal: "refusal", "content_filter", or ""
    Message      string // provider detail, truncated to 500 chars
    StatusCode   int    // HTTP status when from an error body; 0 otherwise
    Cause        error
}

func (e *RefusalError) Error() string
// Format: "model refusal: provider=<p> model=<m> source=<s> reason=<finishReason or body code>"

func (e *RefusalError) Unwrap() error  // returns Cause

func (e *RefusalError) NonRetryable() bool // true
var _ NonRetryableError = (*RefusalError)(nil)

func DetectRefusal(providerID, modelID, finishReason string) *RefusalError
// Triggers, exact strings, case-insensitive:
//   "refusal"         => Source "stop_reason"
//   "content_filter"  => Source "finish_reason"
// Anything else => nil. Empty modelID is allowed (streaming paths may not
// have it); fill from context when known.

func DetectRefusalFromBody(providerID, modelID string, statusCode int, body string) *RefusalError
// Conservative marker list (case-insensitive substring, pinned constants):
//   "safeguards flagged", "content policy violation", "flagged this message"
//   (Anthropic safeguard error text), "content_filter" as an error code key.
// Match => RefusalError{Source: "error_body", StatusCode: statusCode,
// Message: body truncated to 500 chars}. No match => nil.
```

### What This Leaf Consumes

Nothing new — stdlib only. `NonRetryableError` already exists in package llm.

## Tasks

### Task 1: RefusalError type

**Objective:** Define the typed refusal error mirroring QuotaResetError.

**Files:**
- Create: `internal/llm/errors_refusal.go`
- Test: `internal/llm/errors_refusal_test.go`

**Step 1: Write failing test**

```go
func TestRefusalError_NonRetryable(t *testing.T) {
    e := &RefusalError{ProviderID: "zai", ModelID: "glm-4.7", Source: "finish_reason", FinishReason: "content_filter"}
    if !e.NonRetryable() {
        t.Fatal("refusal must be NonRetryable")
    }
    var _ NonRetryableError = e
    if !strings.Contains(e.Error(), "model refusal") || !strings.Contains(e.Error(), "zai") {
        t.Fatalf("Error() missing facts: %q", e.Error())
    }
    if errors.Unwrap(e) != nil {
        t.Fatal("Unwrap of Cause-less error must be nil")
    }
}
```

**Step 2: Run test to verify failure**

Run: `go test -p 2 ./internal/llm/ -run TestRefusalError_NonRetryable -v`
Expected: FAIL - undefined: RefusalError

**Step 3: Write minimal implementation**

Type per contract. Error(): build the message with fmt.Sprintf; truncate
Message to 500 chars before storing (do it in the constructors, mirror
errors_quota.go).

**Step 4: Run test to verify pass**

Run: `go test -p 2 ./internal/llm/ -run TestRefusalError_NonRetryable -v`
Expected: PASS

### Task 2: DetectRefusal (finish/stop reason signals)

**Objective:** Map finish/stop reason strings onto RefusalError.

**Files:**
- Modify: `internal/llm/errors_refusal.go`
- Test: `internal/llm/errors_refusal_test.go`

**Step 1: Write failing test** (table-driven)

Cases: ("refusal" -> Source stop_reason), ("REFUSAL" -> match,
case-insensitive), ("content_filter" -> Source finish_reason),
("CONTENT_FILTER" -> match), ("stop" -> nil), ("" -> nil), ("length" -> nil).

**Step 2: Run to verify failure** (undefined: DetectRefusal)

**Step 3: Implement** — strings.EqualFold against the two pinned constants.

**Step 4: Run to verify pass.**

### Task 3: DetectRefusalFromBody (typed error bodies)

**Objective:** Classify safeguard/refusal error-body text conservatively.

**Files:**
- Modify: `internal/llm/errors_refusal.go`
- Test: `internal/llm/errors_refusal_test.go`

**Step 1: Write failing test**

Positive: body containing "safeguards flagged" (status 403) returns error
with StatusCode 403, Source "error_body", Message contains the body text.
Negative: "connection refused", "internal server error", empty body => nil
on each.

**Step 2: Run to verify failure.**

**Step 3: Implement** — pinned lowercase marker list, strings.Contains on
strings.ToLower(body). Truncate Message at 500 chars.

**Step 4: Run to verify pass.**

### Task 4: Client-side refusal surfacing (non-streaming + streaming finish)

**Objective:** Where clients already parse FinishReason / error bodies,
call the detectors so refusals surface as *RefusalError instead of a
generic completion.

**Files:**
- Modify: `internal/llm/client.go` — after choice.FinishReason parse
  (~line 1831 region, function parseResponseWithTools / Chat return path)
  and after streaming finishReason is known (~line 2347-2436), call
  DetectRefusal; if non-nil, return it as the Response's terminal error
  via the same channel quota uses (follow how existing typed errors are
  returned from these paths — do not invent a new return channel).
- Modify: `internal/llm/anthropic.go` — where apiResp.StopReason becomes
  FinishReason (~line 1794) and the streaming stop_reason site (~line 1661),
  same treatment.
- Test: `internal/llm/errors_refusal_client_test.go`

**Step 1: Write failing test** — httptest server returning an
OpenAI-format body with `"finish_reason": "content_filter"`; assert
`errors.As(err, &refusalErr)` is true on the Chat path. Repeat for a
streaming request asserting the same on the stream terminal error.

**Step 2: Run to verify failure.**

**Step 3: Implement minimally** at the two/three parse sites.

**Step 4: Run to verify pass** plus the full package:
`go test -p 2 ./internal/llm/ -short`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] `go vet ./internal/llm/` passes; gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths)
- [ ] RefusalError implements NonRetryableError (compile-time assertion present)
- [ ] Markers list is CONSERVATIVE (no broad keywords like "policy" alone)
- [ ] No client retry loop ordering was changed
- [ ] Code follows project conventions
- [ ] No bugs, no scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Do NOT touch the retry loops themselves — NonRetryable is honored by
  existing early-exit checks. This is the same free ride QuotaResetError gets.
- Do NOT wire the agent loop (leaf 03's job). This leaf is detection only.
- The Anthropic `refusal` stop_reason is real API surface:
  https://docs.anthropic.com — stop_reason values include "refusal".
