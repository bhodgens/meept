# PII Unmasking Proxy + Restore Lane (Proposal 8) - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Your job: (1) dispatch leaf agents, (2) review their work in-session,
> (3) re-dispatch if incomplete, (4) track completion. Do NOT implement
> code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 leaves (R0-research, 01-detector-store, 02-restore-lane)
- **Scope:** Build a PII unmasking proxy (detect -> swap to tokens
  pre-LLM -> restore on output, session-bounded) and fix the dead secret
  Deobfuscate lane, behind `[security.pii]` (opt-in, default false).

## Goal

Two gaps:

1. **No PII detection exists** - only credential patterns
   (internal/security/sanitizer.go:270+, OutputMonitor
   internal/security/orchestrator.go:205-237) and auditlog output
   redaction to SHA-256 (internal/auditlog/redact.go:13-16/:28). Personal
   data flows to LLM providers in the clear.
2. **The secret obfuscator's restore lane is dead.** SecretObfuscator
   (internal/security/secrets.go:41-64) swaps secrets for `#XXXX#`
   placeholders (16-bit xxhash, :182-187; Obfuscate :188; modes
   obfuscate|replace) pre-LLM via SecretObfuscationHook
   (internal/agent/security_hooks.go:160-200, registered
   components.go:1976-1984). But `Deobfuscate`/`DeobfuscateMessages`
   (:250/:284) have ZERO callers outside internal/security (verified by
   grep): obfuscated tokens leak to users as literal `#XXXX#` today. This
   is a latent bug to fix in the same stroke.

This branch delivers: a regex-first, NER-pluggable PII detector in NEW
package internal/security/pii; a UUID-swap store with DURABLE
session-scoped storage; a restore-on-output hook at the ScanOutput choke
point (internal/agent/loop.go:3213 neighborhood) bounded to the ORIGINATING
session (cross-session restore = info leak, refused); secret-placeholder
restore; config `[security.pii]` default false; hook ordering pinned by
tests.

## Architecture

Mirror of the obfuscator's pre-LLM swap, generalized:

1. **Detector + store** (01): `internal/security/pii` - Detector (regex
   first: emails, phones, SSN-like, credit-card-like with Luhn, addresses
   kept minimal; NER behind an interface for later), SwapStore (crypto/rand
   UUID token per PII span, durable per-session storage via the existing
   session store pattern, in-memory plaintext map following
   Broker.resolve's in-process-only discipline).
2. **Restore lane** (02): a TransformContext-adjacent output hook at the
   ScanOutput choke point restoring tokens BEFORE reply-guard, history,
   LoRA/trace capture (loop.go:5353+), and audit surfaces; session-ID
   scoped; also restores `#XXXX#` secret placeholders when the originating
   session obfuscated them. Config plumbing `[security.pii]` in
   internal/config/schema.go + registration in components.go after
   secret-obfuscation.

R0 research gates the detector layering and restore-window scoping.

## Interface Contracts

### C1-B: PII package surface

```
// internal/security/pii (NEW package)
type Detector struct{...}
func NewDetector(cfg Config) *Detector                  // regex-first; nil-safe
func (d *Detector) Detect(text string) []Span           // ordered, non-overlapping
type Span struct{ Start, End int; Class string; Value string } // Value stays in-process ONLY
type Swapper struct{...}
func NewSwapper(store *SwapStore) *Swapper
func (s *Swapper) Mask(text string, sessionID string) (masked string, count int)
func (s *Swapper) Restore(text string, sessionID string) (restored string, count int)
// Restore refuses tokens not originating from sessionID (cross-session = leak).
// Owner: 01-detector-store.md; Consumers: 02-restore-lane.md, 03-e2e
```

### C2-B: Swap token + durable store

```
// Token format: [PII:<uuid-v4>] (crypto/rand; predid analyzer clean).
// SwapStore: durable session-scoped rows (session store sqlite pattern),
// in-memory plaintext map for the process lifetime; plaintext NEVER to
// logs/env/audit/trace (master contract C2). API:
func NewSwapStore(dir string) (*SwapStore, error)       // dir under MEEPT_HOME
func (st *SwapStore) Put(sessionID, token, value, class string) error
func (st *SwapStore) Resolve(sessionID, token string) (value string, ok bool)
func (st *SwapStore) ResolveForRestore(sessionID, text string) (string, int)
// Owner: 01-detector-store.md; Consumers: 02-restore-lane.md, e2e
```

### C3-B: Restore choke point + secret lane

```
// internal/agent/loop.go:3213 neighborhood (ScanOutput call): BEFORE the
// result is used further (reply-guard, history append, trace capture at
// loop.go:5353+), run:
//   finalResponse = piiRestore(finalResponse, sessionID)  // hook-driven
// Secret placeholders (#XXXX#) restored by the SAME hook via
// SecretObfuscator.Deobfuscate - the dead lane gets its caller.
// Owner: 02-restore-lane.md; Consumers: 03-e2e
```

### C4-B: Config

```
// internal/config/schema.go, SecurityConfig:
Pii PiiConfig `json:"pii" toml:"pii"`
type PiiConfig struct {
    Enabled bool   `json:"enabled" toml:"enabled"`     // default FALSE (opt-in)
    Classes []string `json:"classes" toml:"classes"`  // default: email,phone,ssn,credit_card
    RestoreWindow time.Duration `json:"restore_window" toml:"restore_window"` // 0 = session lifetime
}
// Owner: 02-restore-lane.md; Consumers: components.go wiring, e2e
```

### C5-B: Hook order (instance of master C1)

```
TransformContext (critical): security-transform -> secret-obfuscation ->
pii-restore (registered LAST of the three; restore is output-side but its
registration ride-along test pins input-side order too).
// Owner: 01-detector-store.md (order test), 02-restore-lane.md (registration)
// Consumers: e2e
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| R0 | R0-research.md | leaf (research) | none | 25K | A |
| 01 | 01-detector-store.md | leaf | R0 | 75K | B |
| 02 | 02-restore-lane.md | leaf | 01 | 90K | C |

R0 first; 01 after findings digest; 02 after 01 REVIEWED (sequential:
store API consumed by restore lane).

## Dispatch Protocol

For each concurrency group, in dependency order.

### Phase 0: Research (group A)

Dispatch R0-research.md via `delegate_task` (web research + findings file,
no production code, Do NOT commit). Review findings in-session; convert
decision IDs into dated addenda for 01/02 dispatch contexts.

### Phase 1: Dispatch 01-detector-store.md (group B)

Context = full leaf text + contracts C1-B/C2-B + secrets.go/Broker
patterns INLINED + "pure-mechanism leaf in a new package with unit tests;
e2e coverage lands in 03-e2e. Do NOT commit. Do NOT git add."

### Phase 2: Review and commit each child

In-session review (main model): leaf spec, contracts, Review Checklist;
`go test -p 2 ./internal/security/...`; grep debug artifacts + line-number
corruption; verify NO swap value reaches logs (grep for logger calls near
value handling). Pass: commit leaf's exact files, REVIEWED. Gaps: re-dispatch
(max 3), then escalate to ../master.md.

### Phase 3: Dispatch 02-restore-lane.md (group C)

Context = full leaf text + contracts C3-B/C4-B/C5-B + 01's exposed
signatures + components.go / loop.go choke-point neighborhoods INLINED +
no-commit rules. NOTE: components.go may have been edited by branch A -
re-read fresh before patching.

### Phase 4: Integration review

After 02 REVIEWED: `go build ./...`; `go test -p 2 ./internal/security/...
./internal/agent/... ./internal/daemon/... ./internal/config/...`;
sanitizer C0-first pinned test green; ordering test green. Report COMPLETE
to ../master.md.

## Review Checklist

The branch orchestrator (main model) verifies each child in-session:

- [ ] All tasks implemented; tests passing (`go test -p 2`)
- [ ] Contracts C1-B..C5-B satisfied exactly
- [ ] Swap values/token resolution never logged, env-set, audit-rowed, or
      traced (grep the new code for logger/emitter/setenv near value paths)
- [ ] Restore refuses cross-session tokens (test present)
- [ ] [security.pii] default false; disabled path byte-identical behavior
- [ ] Secret `#XXXX#` restore works and does not regress obfuscation tests
- [ ] Sanitizer C0-first order untouched; hook order pinned by tests
- [ ] No debug artifacts; no line-number corruption; leaf made no commits

Output: APPROVED or specific gaps.

## Coding Conventions

- Go, gofmt-clean; exported minimal; errors wrapped with %w.
- crypto/rand for tokens (predid analyzer); no math/rand/time-based IDs.
- No mutex across I/O (mutexio); plaintext swap values in-process only.
- Table-driven tests; `go test -p 2`. ASCII only; no heredocs.
- Config fields carry BOTH json and toml tags (schema.go style).
- Leaf agents never git commit/add.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| RB1 | "PII unmasking proxy with restore lane (implement fully)" | 01 T1-T3, 02 T1-T2 | e2e boundary-hardening-03 |
| RB2 | "regex-first, NER-pluggable" | contract C1-B; 01 T1 | detector interface test |
| RB3 | "UUID-swap map with DURABLE session-scoped storage" | contract C2-B; 01 T2 | store persistence test |
| RB4 | "restore bounded to the ORIGINATING session" | 02 T2 | cross-session refusal test |
| RB5 | "fix the existing dead Deobfuscate lane" | contract C3-B; 02 T4 | e2e boundary-hardening-04 |
| RB6 | "config [security.pii] opt-in default false" | contract C4-B; 02 T3 | config default test |
| RB7 | "hook ordering ... pin the pass order with tests (sanitizer-ordering class)" | contracts C5-B + master C1; 01 T4 | ordering test |
| RB8 | "leakage classes named and closed (logs, audit, trace capture)" | master C2/C3; 02 T1/T2 | e2e boundary-hardening-03 |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| R0-research.md | COMPLETE | 1 | landed c5ef67e9 (findings doc, reviewed) |
| 01-detector-store.md | COMPLETE | 1 | landed 46661bbe (issue 84) |
| 02-restore-lane.md | COMPLETE | 1 | landed 1e4912cc (issue 84) |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE |
BLOCKED

## Integration Test Plan

1. `go test -p 2 ./internal/security/pii/...` - detector classes, swap
   round-trip, durable store, cross-session refusal, token format.
2. `go test -p 2 ./internal/security/...` - existing obfuscator tests
   green + new ordering test green; sanitizer_marker_order_test.go green.
3. `go test -p 2 ./internal/agent/... ./internal/daemon/...` - restore hook
   at ScanOutput choke point; components.go wiring present with typed-nil
   guards; disabled path byte-identical.
4. e2e (after 03-e2e): boundary-hardening-03 (PII roundtrip + leak-class
   closure), boundary-hardening-04 (secret #XXXX# restored).

## Test Gate

Tests-first gate: not used (research-gated pattern; inline TDD tables).
Coverage matrix:

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| FB1 | PII detector classes + NER seam | 01 T1 | TestDetectorDetects*/TestDetectorNERSeam | boundary-hardening-03 | VALIDATED |
| FB2 | UUID swapper + durable session store | 01 T2-T3 | TestSwapperRoundTrip/TestSwapStoreDurability | boundary-hardening-03 | VALIDATED |
| FB3 | hook order pinned (critical peers) | 01 T4 | TestPiiHookOrderPinned | boundary-hardening-03 | VALIDATED |
| FB4 | restore at ScanOutput, session-bounded | 02 T1-T2 | TestRestoreAtScanOutput/TestRestoreCrossSessionRefused | boundary-hardening-03 | VALIDATED |
| FB5 | secret #XXXX# restore (dead lane fixed) | 02 T4 | TestSecretPlaceholderRestore | boundary-hardening-04 | VALIDATED |
| FB6 | config [security.pii] default false + disabled passthrough | 02 T3 | TestPiiConfigDefaults/TestPiiDisabledByteIdentical | boundary-hardening-03 | VALIDATED |
| FB7 | leakage classes closed (logs/audit/trace) | master C2/C3 | in-package assertions | boundary-hardening-03 | VALIDATED |

Statuses: PENDING | DELTA | VALIDATED. Delta log: [none yet]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda.

## Notes

- Verified anchors (grep-confirmed 2026-10-31): secrets.go:41-64 struct,
  :182-187 placeholder (xxhash 16-bit), :188 Obfuscate, :250 Deobfuscate,
  :271/:284 ObfuscateMessages/DeobfuscateMessages; security_hooks.go:160-200
  SecretObfuscationHook; components.go:1976-1984 registration;
  loop.go:3213 ScanOutput call; loop.go:5353+ trace capture records tool
  output; orchestrator.go:205-237 ScanOutput; secrets broker
  internal/secrets/broker.go MEEPT_SECRET:<name> (:25).
- MEEPT_SECRET broker placeholders are a TWIN mechanism - out of scope
  except: the restore lane must NOT mangle them; note any interaction in
  the report (scripts/audit-secrets-value-callers.py governs Broker.resolve
  discipline).
- Durable store location: under MEEPT_HOME via config.MeeptPath - never
  the daemon CWD.
