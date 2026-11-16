# 05 Data Posture + Sensitivity Gate - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test (report suspected-wrong gate tests to the
> orchestrator instead). Do NOT commit - the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. After writing a file, do NOT
> read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Add a per-model `data_posture` label (Contract C4) and the first filter in the selection chain (posture, registered FIRST so it is evaluated even when a later filter would skip). Scan the outbound prompt with the existing secret scanner; a sensitive turn routes only to `private` models.
- **Dependencies:** 00T gate tests committed (RED); 01-quality-dimension.md COMPLETE (creates `selection.go`)
- **Estimated Context:** 60K
- **Concurrency Group:** A2 (wave 2 - serializes behind 01; both write `providers.go` and `resolver.go`)
- **Source reference:** Sovereign Router `src/sensitivity.py`; registry `lane` (`private-free` / `public-only-free` / `paid` / `premium`) plus the `spillway_sensitive_excluded` flag. Note: Sovereign's `lane` encodes DATA POSTURE and price together; meept separates them - this leaf implements only the posture half, because price already lives in `input_cost`/`output_cost`.

## Goal

meept has **no way to say "this model trains on submitted data, so never send it this prompt."** `grep -rni "posture|no_train|data_collection" internal/` returns no hits in `internal/llm/`. `internal/security/secret_scanner.go` is written and tested, but its only callers are `internal/tools/builtin/filesystem.go:69` and `shell.go:237` - it never sees the outbound prompt.

This is the one gap from the comparison with a compliance consequence rather than a cost consequence: Google's free tier trains on data, several aggregators expose no auditable policy, and an operator currently cannot express that as a routing rule.

### Why posture is registered FIRST

The filter order in Contract C1 is: posture, quality, admission, budget. Posture is a hard compliance boundary, so it must be evaluated even when a later filter would skip the same candidate - otherwise the routing log names the wrong binding constraint. A sensitive turn must never reach a `public_only` model even if that model would also have failed the quality floor.

## Context

- `internal/security/secret_scanner.go:21-60` - `NewSecretScanner()`, `Scan(text string) []string`, `ScanAndReport(text string) string`. Rules are `defaultSecretRules()`: known credential formats (AWS keys, bearer tokens, private-key headers). **Reuse this. Do not write a second detector.**
- `internal/daemon/components.go:8419-8421` - the only construction site today (`readFileTool.SetSecretScanner`). Shadow/LLM wiring does NOT construct one; this leaf adds the LLM-side construction.
- `internal/llm/providers.go:73-110` - `ModelDef`: gains `DataPosture`.
- `internal/llm/models.go:229-300` - `ModelConfig`: gains `DataPosture`.
- `internal/llm/selection.go` - from leaf 01: the `Filter` interface and `registerSelectionFilters`.
- `internal/llm/resolver.go:113` - `NewResolver`, where filters register.
- Sovereign Router's rule, verbatim (`docs/ROUTING-PRINCIPLES.md` rule 7): "Five providers are non-sensitive-only by registry flag... The flag also covers, per-entry, every OpenRouter paid route... their `data_collection=deny` probes passed, but the aggregator exposes no auditable data-policy metadata, so the conservative posture stands until it does."

## Verified Anchors

- `internal/security/secret_scanner.go:21` - `NewSecretScanner`
- `internal/security/secret_scanner.go:42` - `Scan(text) []string`
- `internal/security/secret_scanner.go:54` - `ScanAndReport`
- `internal/daemon/components.go:8419-8421` - the current (tools-only) construction site
- `internal/tools/builtin/filesystem.go:69` - `SetSecretScanner` (existing caller, unchanged)
- `internal/tools/builtin/shell.go:237` - `SetSecretScanner` (existing caller, unchanged)
- `internal/llm/providers.go:73` - `ModelDef`
- `internal/llm/models.go:229` - `ModelConfig`
- `internal/llm/selection.go` - `Filter` interface from leaf 01
- `internal/llm/resolver.go:113` - `NewResolver`
- `e2e/suites/sovereign-parity/posture_test.go` - gate tests T7, T8, T9

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm/posture.go
type Posture string
const (
    PosturePrivate      Posture = "private"        // no-training; cleared for sensitive
    PosturePublicOnly   Posture = "public_only"    // provider may train -> sensitive barred
    PostureNonSensitive Posture = "non_sensitive"  // aggregator, posture unclear -> barred
)

// ParsePosture maps a config string to a Posture. Unknown/empty -> Private.
func ParsePosture(s string) Posture

// SensitivePostures is the set a sensitive turn may NOT route to.
func (p Posture) AllowsSensitive() bool   // true only for PosturePrivate

type postureFilter struct{ scanner *security.SecretScanner }
func (postureFilter) Name() string { return "posture" }
func (f postureFilter) Filter(c *Candidate, fc *FilterCtx) Verdict
func (postureFilter) Reason() string   // "sensitive_data_posture"
```

### What This Leaf Consumes

- `security.NewSecretScanner()` from `internal/security`. **`internal/llm` importing `internal/security` must be checked for an import cycle before the first edit** - run `go list -deps ./internal/security | grep internal/llm` and confirm no cycle. If a cycle exists, STOP and report to the orchestrator; the fallback is a narrow `SecretScanner` interface declared in `internal/llm` and satisfied by `*security.SecretScanner` (structural typing), wired from the daemon.
- The outbound prompt text. **Survey first**: find where the client assembles the final message list (`internal/llm/client.go`) and scan the concatenation of user + system text. Do not scan tool results - they are already scanned at the tool boundary.

### Frozen semantics

```
posture absent / unknown / empty   -> PosturePrivate   (backward compatible - Q3)
sensitive turn + private model     -> ALLOW
sensitive turn + public_only       -> SKIP  ("sensitive_data_posture")
sensitive turn + non_sensitive     -> SKIP  ("sensitive_data_posture")
non-sensitive turn                 -> ALLOW for every posture
scanner error / nil scanner        -> treat as NOT sensitive (fail open),
                                      never fail a turn on a scanner fault
```

The fail-open rule on scanner error is deliberate and must be commented: a broken detector must not become a denial of service. This mirrors the repo's "a warmup failure NEVER hard-fails a turn" posture (`internal/agent/AGENTS.md`, classifier boot readiness).

**Log discipline:** the matched string is NEVER logged - only the count and the rule name. Sovereign Router: "the match itself is never logged." The e2e gate T9 asserts this.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Import-cycle check: `go list -deps ./internal/security` contains no `internal/llm`; record the result in the leaf report | - | `go list -deps ./internal/security \| grep -c internal/llm` | n/a | prints 0 | Given the repo; When listed; Then 0 (no cycle) |
| T2 | Add `Posture`, the three constants, `ParsePosture`, `AllowsSensitive` | Create: `internal/llm/posture.go` | `go test -p 2 ./internal/llm/ -run TestParsePosture` | RED (undefined) | GREEN | Given "", "private", "public_only", "non_sensitive", "garbage"; When parsed; Then Private, Private, PublicOnly, NonSensitive, Private |
| T3 | Add `DataPosture Posture` (`json:"data_posture"`) to `ModelDef` and map it to `ModelConfig` at the construction site | Modify: `internal/llm/providers.go:73-110`, `:737-740`; `internal/llm/models.go:229-300` | `go build ./internal/llm/` | RED (compile) | GREEN | Given a json5 block with `"data_posture": "public_only"`; When loaded; Then `ModelConfig.DataPosture == PosturePublicOnly` |
| T4 | Implement `postureFilter.Filter`: scan, set `fc.Sensitive`, skip non-private models on a sensitive turn | Modify: `internal/llm/posture.go` | `go test -p 2 ./internal/llm/ -run 'TestPostureFilter_'` | RED | GREEN | Given a public_only candidate and a sensitive prompt; When filtered; Then `(VerdictSkip, "sensitive_data_posture")` |
| T5 | Register the filter FIRST in the chain | Modify: `internal/llm/resolver.go:113` (`registerSelectionFilters`) | `go test -p 2 ./internal/llm/ -run TestSelection_Order` | RED | GREEN | Given the registered order; When iterated; Then posture is index 0 |
| T6 | Fail-open: a nil scanner or a scanner error treats the turn as non-sensitive and never errors | Modify: `internal/llm/posture.go` | `-run 'TestPostureFilter_FailOpen'` | RED | GREEN | Given a nil scanner; When filtered; Then ALLOW, no error |
| T7 | E2E: a sensitive prompt never routes to public_only / non_sensitive; a private model serves | - | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestPosture_'` | RED | GREEN | Given a public_only model first in the alias; When a prompt contains an AWS-shaped key; Then the private model serves |
| T8 | E2E inertness: with no `data_posture` key anywhere, routing is byte-identical to `main` | - | `-run TestPosture_AbsentIsPrivate'` | RED | GREEN | Given no posture config; When a normal turn; Then the routing decision is unchanged |
| T9 | E2E log hygiene: the matched secret appears in NO log line and NO DB row | - | `-run TestPosture_NoMatchInLogs'` | RED | GREEN | Given a sensitive prompt; When the turn runs; Then grep of logs and `routing_decisions` finds no match |
| T10 | Document the key in `config/models.json5` with a comment naming the three values and the backward-compatible default | Modify: `config/models.json5` | config-drift-check | n/a | documented, no model set | Given the template; When read; Then the key is documented and unset |

## Self-Verification Checklist

- [ ] T1-T10 complete; gate tests T7, T8, T9 GREEN.
- [ ] Import cycle verified absent (T1), or the interface fallback was used and reported.
- [ ] `secret_scanner.go` is UNCHANGED; this leaf only calls it.
- [ ] Existing tools-side scanner wiring (`components.go:8419-8421`) is untouched.
- [ ] Posture is registered FIRST; the order is commented.
- [ ] Absent posture = `private`; fail-open on scanner fault.
- [ ] The matched string never reaches a log or a DB row (T9).
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] Frozen table matches master.md C4 (default private, fail-open).
- [ ] Posture filter is first in the chain and the reason string is exactly `sensitive_data_posture`.
- [ ] No second secret detector was written - `security.SecretScanner` is reused.
- [ ] Only user/system text is scanned, not tool results.
- [ ] No model in `config/models.json5` was silently given a posture (Q3: backward compatible).
- [ ] Comment cites the compliance reason, not just the mechanism.

## Do NOT commit

The orchestrator commits this leaf after review.