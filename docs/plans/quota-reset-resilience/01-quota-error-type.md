# QuotaResetError type and parsing - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** The structured `QuotaResetError` type, its classification
  helpers, the `ParseQuotaResponse` parser for 429/402 responses, and the
  credential-fingerprint function.
- **Dependencies:** none
- **Estimated Context:** 80K
- **Concurrency Group:** A

## Goal

Give every downstream layer (client retry loop, broker rotation, agent state,
notifications) one structured way to say: "this provider credential hit a
recoverable-by-waiting cap; it resets at time T (or unknown), and waiting is
bounded by MaxWait." This leaf defines that type, parses it from the two HTTP
error sites (OpenAI-compat and Anthropic), and never guesses from free text.

## Context

Meept's LLM stack has two client implementations sharing the `internal/llm`
package: an OpenAI-compatible client (`internal/llm/client.go`, used for
most providers and for streaming) and an Anthropic Messages-API client
(`internal/llm/anthropic.go`). Both have their own `doRequest` and their own
retry loop. Existing error types live in `internal/llm/errors.go`:
`RateLimitError` (429 with RetryAfter/RetryStrategy/LimitBudget, parsed via
`ParseRateLimitBody`), `APIError` (bare status + detail),
`BudgetExceededError` (meept's own budget, non-retryable), and
`NonRetryableError` marker interface.

Key distinction this leaf encodes: a quota/usage-window error must NOT
short-retry inside the client loop (it would burn 3 attempts over ~30s for
a 4-hour window) but MUST be visible to the broker as "rotate and block."
`RateLimitError` keeps its current meaning (seconds-scale); quota-window
errors get the new type.

Provider shapes found in research (all reducible to structured fields or
headers — no body-text guessing):

- OpenAI/Codex: 429, `{"error":{"type":"usage_limit_reached","resets_at":<unix>}}`
  or `{"error":{"type":"insufficient_quota","code":"credit_balance_exhausted"}}`,
  Codex headers `X-Codex-Primary-Reset-At` / `X-Codex-Primary-Reset-After-Seconds`.
- Anthropic: 429, `{"type":"error","error":{"type":"rate_limit_error"|"quota_exceeded",...}}`
  with `retry-after` (seconds) and `anthropic-ratelimit-tokens-reset`
  (RFC3339) headers.
- OpenRouter: 429, outer `{"error":{"message":"...: {\"error\":{...inner}}","code":429}}`
  with inner `retry_after` (seconds) — already partially parsed by
  `ParseOpenRouterError`.
- Tencent: 429, integer `code` (429001-429006), `Retry-After` header seconds.
- Gemini-via-OpenAI-compat: 429, `{"error":{"status":"RESOURCE_EXHAUSTED","code":429}}`.
- xAI/Grok: 429/402, OpenAI-compatible shapes.
- Anything else on 429/402 with an unparseable body: still a quota error,
  with zero ResetAt (caller applies config DefaultEstimate).

Key files to understand before implementing:

- `internal/llm/errors.go` — existing error types, `parseRetryAfter` /
  `parseRetryAfterSeconds` / `parseRetryAfterDate` header helpers,
  `ParseRateLimitBody`, `IsRateLimitError`, `AsRateLimitError`,
  `UserMessage` dispatch, `IsNonRetryable`.
- `internal/llm/client.go` — 429 handling block in `doRequest`
  (search "Check for rate limit (429) specifically", ~line 989) and the
  retry loop (~lines 406-480) that must NOT pick up QuotaResetError.
- `internal/llm/anthropic.go` — retry loop (~lines 255-280) and its own
  `doRequest` error paths.
- `internal/llm/errors_test.go` — table-driven test style to follow.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/llm/errors.go (append; or errors_quota.go in package llm
// if errors.go would exceed ~700 lines — your call, state it in deviations)

type QuotaResetError struct {
    ProviderID  string
    ModelID     string
    Code        string        // "usage_limit_reached", "insufficient_quota", "quota_exceeded", "resource_exhausted", ...
    Message     string        // truncated raw detail (<= 500 chars)
    ResetAt     time.Time     // absolute; zero = unknown
    RetryAfter  time.Duration // derived wait; min(ResetAt-now, MaxWait); 0 when ResetAt zero
    MaxWait     time.Duration // caller-supplied cap (config MaxWait); 0 = no cap applied yet
    StatusCode  int           // 429 or 402
    Cause       error
}

func (e *QuotaResetError) Error() string
func (e *QuotaResetError) UserMessage() string
func (e *QuotaResetError) Unwrap() error
func (e *QuotaResetError) NonRetryable() bool   // true: blocks the client short-retry loop

var _ NonRetryableError = (*QuotaResetError)(nil)

func IsQuotaResetError(err error) bool
func AsQuotaResetError(err error) (*QuotaResetError, bool)

// ParseQuotaResponse classifies an HTTP error response. Returns nil unless
// statusCode is 429 or 402. Precedence: structured body fields, then
// headers, then bare status. An unparseable body on 429/402 still yields a
// non-nil error with zero ResetAt and Code "" (caller estimates).
// knownFields carries ProviderID/ModelID/MaxWait for population; MaxWait<=0
// means leave RetryAfter unset.
func ParseQuotaResponse(statusCode int, header http.Header, body []byte, known QuotaContext) *QuotaResetError

type QuotaContext struct {
    ProviderID string
    ModelID    string
    MaxWait    time.Duration
}

// QuotaCredentialKey: provider-scoped credential identity. Same billing
// pool => same string. Never contains raw key material.
//   literal apiKey        -> providerID + ":key:" + hex(sha256(apiKey))[:12]
//   env-based apiKey      -> providerID + ":env:" + envVarName
//   OAuth                 -> providerID + ":oauth:" + OAuthProvider
//   nothing identifiable  -> providerID + ":default"
func QuotaCredentialKey(cfg *ModelConfig) string
```

### What This Leaf Consumes

```
// Existing, internal/llm/errors.go:
func parseRetryAfter(header string) time.Duration   // reuse for Retry-After
// Existing ModelConfig fields (internal/llm/model or config types — verify
// actual field names/APIKey resolution path with search_files before coding):
// cfg.APIKey, cfg.OAuthProvider (and however env-var keys are resolved)
```

## Tasks

### Task 1: QuotaResetError type + helpers

**Objective:** Define the type with Error/UserMessage/Unwrap/NonRetryable
and the errors.As-based helpers.

**Files:**
- Modify: `internal/llm/errors.go` (or create `internal/llm/errors_quota.go`)
- Test: `internal/llm/errors_quota_test.go`

**Step 1: Write failing test**

Table-driven: `UserMessage` includes the reset horizon when ResetAt is set
(e.g. "quota limit reached on <model>; resets in 3h59m" — lowercase), falls
back to code/status when unknown; `NonRetryable() == true`;
`IsQuotaResetError` true for direct, wrapped (`fmt.Errorf("%w")`), and
nil-false; `AsQuotaResetError` returns the typed value. Assert
`IsNonRetryable(&QuotaResetError{...}) == true` via the existing marker
interface.

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestQuotaResetError -v`
Expected: FAIL (undefined: QuotaResetError)

**Step 3: Write minimal implementation**

Implement exactly the contract shape. `Error()` format:
`quota limit exceeded: provider=<p> model=<m> code=<code>[ resets_at=<t>]`.
`UserMessage()`: lowercase, human phrasing with reset horizon when known.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestQuotaResetError -v`
Expected: PASS

### Task 2: Structured body parsing

**Objective:** Parse quota bodies from known provider shapes into the error.

**Files:**
- Modify: same file as Task 1
- Test: same test file

**Step 1: Write failing test**

Table-driven over `ParseQuotaResponse` with 200 + body samples:

- OpenAI `usage_limit_reached` with `resets_at` unix seconds -> ResetAt set,
  Code "usage_limit_reached".
- OpenAI `insufficient_quota` with no reset field -> Code set, ResetAt zero.
- Anthropic `{"type":"error","error":{"type":"quota_exceeded"}}` ->
  Code "quota_exceeded".
- Anthropic `rate_limit_error` body on 429 WITHOUT quota signal -> still
  returns the error (status 429), Code "rate_limit_error", but with
  ResetAt taken from headers if present. (429 is quota-ish by default per
  the design: default-retry posture; broker blocks until estimate.)
- Gemini `RESOURCE_EXHAUSTED` status string -> Code "resource_exhausted".
- Tencent integer `code: 429004` (JSON number) -> Code "429004" (string
  form; parser must handle number-or-string code).
- OpenRouter nested inner JSON -> reuse/extend existing parse path;
  inner `retry_after` seconds populate RetryAfter when MaxWait >= it.
- 402 with OpenAI-compatible `insufficient_quota` body -> parsed.
- 500 body (any) -> nil. 429 with plain-text body -> non-nil, zero fields.
- Malformed JSON on 429 -> non-nil, zero fields (no panic).

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestParseQuotaResponse -v`
Expected: FAIL (undefined)

**Step 3: Write minimal implementation**

Reuse the existing `extractInnerJSON` + OpenRouter structs where possible.
Derive ResetAt from body `resets_at` (unix seconds). Derive RetryAfter from
min(ResetAt-now, known.MaxWait) when both known; leave 0 otherwise. Truncate
Message to 500 chars. NEVER match free text for reset times — structured
fields only.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestParseQuotaResponse -v`
Expected: PASS

### Task 3: Header extraction

**Objective:** Pull reset info from rate-limit headers when the body lacks it.

**Files:**
- Modify: same file as Tasks 1-2
- Test: same test file

**Step 1: Write failing test**

- `Retry-After: 120` (seconds) -> RetryAfter 120s (when MaxWait >= 120s).
- `Retry-After: <RFC1123 date>` -> converts to absolute -> ResetAt.
- `anthropic-ratelimit-tokens-reset: <RFC3339>` -> ResetAt.
- `X-Codex-Primary-Reset-At: <unix seconds>` -> ResetAt.
- `X-Codex-Primary-Reset-After-Seconds: 3600` -> RetryAfter.
- Header conflicting with body: body `resets_at` wins.
- Existing `parseRetryAfter` reused, not duplicated.

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestQuotaHeaderExtraction -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Extend ParseQuotaResponse (same function, header-aware). Keep precedence:
structured body > headers > bare status.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestQuotaHeaderExtraction -v`
Expected: PASS

### Task 4: Integrate into both clients' error paths

**Objective:** `client.go` and `anthropic.go` `doRequest` return
QuotaResetError for 429/402 (after the existing 429 RateLimitError check
stays for its shapes), while their retry loops do NOT short-retry it.

**Files:**
- Modify: `internal/llm/client.go` (429 block ~line 989; add 402 branch near
  the generic error-status branch ~line 1036)
- Modify: `internal/llm/anthropic.go` (its doRequest error path)
- Modify: `internal/llm/client.go` retry loop (~lines 408-418) and streaming
  equivalent (~lines 568-577): a returned QuotaResetError must exit the loop
  immediately (it already will via `errors.As(&rlErr)`-style checks only if
  you add an explicit QuotaResetError branch BEFORE the RateLimitError one —
  do so; the two error types are siblings, not nested)
- Test: `internal/llm/quota_client_test.go`

**Step 1: Write failing test**

httptest servers: (a) returns 429 `usage_limit_reached` body -> both clients
return QuotaResetError (assert type + ResetAt). (b) returns 402 with
`insufficient_quota` -> QuotaResetError. (c) counts requests: client must hit
the server exactly ONCE for a quota error (no 3-attempt loop) in both the
non-streaming and streaming paths. (d) plain 429 with `rate_limit_error`
Anthropic shape via AnthropicClient -> QuotaResetError with header-derived
ResetAt. (e) existing RateLimitError behavior for OpenRouter tpm shapes is
unchanged (regression guard: existing tests still pass).

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestQuotaClient -v`
Expected: FAIL (clients return APIError today)

**Step 3: Write minimal implementation**

In each doRequest: after existing 429 handling, if
`resp.StatusCode == 402` OR (429 and body indicates a quota/usage shape OR
ParseRateLimitBody found nothing), build via ParseQuotaResponse and return.
Order matters: keep the existing OpenRouter/tpm RateLimitError path intact —
quota detection must not swallow short-cycle rate limits that carry
`retry_after` seconds and `retriable: true`. Practical rule: if
ParseRateLimitBody returns a detail with Retriable==true and RetryAfter < 5m,
keep RateLimitError; otherwise classify as QuotaResetError. State your final
branch order in the leaf report.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestQuotaClient -v`
Expected: PASS
Also run the existing error tests:
`go test ./internal/llm/ -run 'TestParseOpenRouterError|TestParseRateLimitBody|TestRateLimit' -v`

### Task 5: QuotaCredentialKey

**Objective:** Stable per-credential fingerprint, no raw key material.

**Files:**
- Modify: same errors/quota file
- Test: same test file

**Step 1: Write failing test**

- Two ModelConfigs with the same literal APIKey -> same key.
- Different APIKeys -> different keys, neither contains the key substring.
- Env-var-based key (whatever field/mechanism exists — VERIFY with
  search_files; e.g. `APIKeyEnv` or `"${ENV}"` expansion in config) -> stable
  `:env:` form containing the variable NAME, not its value.
- OAuth config -> `:oauth:` + OAuthProvider.
- Empty everything -> `providerID + ":default"`.
- Output never contains more than 12 chars of derived hash.

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestQuotaCredentialKey -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Per the contract. sha256 the literal key when present.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestQuotaCredentialKey -v`
Expected: PASS

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] No free-text/timezone guessing anywhere: reset times come from
      structured fields or headers only
- [ ] Client retry loops exit immediately on QuotaResetError (exactly one
      HTTP attempt for a quota error)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, file paths)
- [ ] 402 + 429 both classified; non-429/402 statuses never classified
- [ ] Existing RateLimitError/OpenRouter behavior unchanged (regression
      tests still green)
- [ ] No raw key material in QuotaCredentialKey output or logs
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The classification order inside doRequest is the riskiest part. Existing
  OpenRouter tpm errors (`retry_after: 2.0`, `retriable: true`) are
  seconds-scale and MUST stay RateLimitError. Multi-hour windows
  (`usage_limit_reached`, `resets_at` far future) must become
  QuotaResetError. The suggested heuristic (Retriable==true && RetryAfter<5m
  stays RateLimitError) is a starting point; if tests show a cleaner split
  (e.g. code-based), take it and document the deviation.
- internal/llm's full test suite takes 80-90s. Always scope runs with -run.
- Anthropic's `rate_limit_error` on 429 is genuinely ambiguous (per-minute
  throttle vs subscription cap through claude-code OAuth). The design
  decision: classify as QuotaResetError with header-derived reset when
  available, else estimate. The broker block makes the per-minute case
  slightly conservative (blocked until estimate) — acceptable.
