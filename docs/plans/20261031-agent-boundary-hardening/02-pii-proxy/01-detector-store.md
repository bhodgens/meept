# PII Detector + Swap Store - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.
> This leaf creates the NEW package internal/security/pii ONLY - no
> wiring, no loop.go edits (leaf 02's job).

## Meta

- **Parent:** orchestrator.md
- **Scope:** Regex-first, NER-pluggable PII detector plus a UUID-swap
  Swapper with a DURABLE session-scoped SwapStore in new package
  internal/security/pii.
- **Dependencies:** R0-research.md decisions (D1-D3 cited in addenda)
- **Estimated Context:** 75K
- **Concurrency Group:** B
- **Audit references:** proposal 8 (master.md R4/R5/R8)

## Goal

Give the daemon the detection-and-swap half of the unmasking proxy:
PII spans found in text, replaced with `[PII:<uuid>]` tokens pre-LLM,
with the mapping stored durably per session so the restore lane (leaf 02)
can reverse it within the originating session only.

## Context

- internal/security/secrets.go - SecretObfuscator is the pattern twin:
  entries + maps under sync.RWMutex, placeholder generation, modes.
  Mirror its concurrency discipline (RWMutex; no I/O under lock).
- internal/secrets/broker.go - MEEPT_SECRET:<name> twin + the
  in-process-only value rule enforced by
  scripts/audit-secrets-value-callers.py. The same discipline applies:
  raw PII values and the swap map NEVER reach logs, env, audit rows, or
  traces.
- internal/config/schema.go - PiiConfig will be added by leaf 02; this
  leaf defines pii.Config (package-local) so the config struct maps 1:1.
- Durable storage: follow the session-store sqlite pattern
  (internal/session/store_sqlite.go) for schema style; store rows under a
  dir provided by the caller (leaf 02 passes a MEEPT_HOME path).
- R0 findings: docs/research/pii-unmasking-findings.md (D1 regex set,
  D2 token shape, D3 windowing - honor them where they do not conflict
  with the contracts below).

## Verified Anchors

- internal/security/secrets.go:41-64 - SecretObfuscator struct/mutex shape
- internal/security/secrets.go:188 - Obfuscate (match->replace loop shape)
- internal/secrets/broker.go:25 - PlaceholderPrefix "MEEPT_SECRET:"
- internal/session/store_sqlite.go - durable-store pattern reference

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C1-B / C2-B (exact signatures; orchestrator holds the full definitions)
package pii
type Span struct{ Start, End int; Class string; Value string } // Value in-process only
type Detector struct{...};  func NewDetector(cfg Config) *Detector
func (d *Detector) Detect(text string) []Span
type SwapStore struct{...}; func NewSwapStore(dir string) (*SwapStore, error)
func (st *SwapStore) Put(sessionID, token, value, class string) error
func (st *SwapStore) Resolve(sessionID, token string) (value string, ok bool)
func (st *SwapStore) ResolveForRestore(sessionID, text string) (string, int)
type Swapper struct{...};   func NewSwapper(store *SwapStore) *Swapper
func (s *Swapper) Mask(text string, sessionID string) (masked string, count int)
func (s *Swapper) Restore(text string, sessionID string) (restored string, count int)
// Token: [PII:<uuid-v4>] via crypto/rand. NER seam:
type Recognizer interface{ Detect(text string) []Span }  // pluggable NER later
```

### What This Leaf Consumes

- C-none from siblings (leaf 02 consumes this leaf).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Detector: regex recognizers for email, phone, SSN-like, credit-card-like (with Luhn check to cut false positives); ordered non-overlapping spans; Config with Enabled/Classes; Recognizer interface so a NER recognizer can be appended | Create: internal/security/pii/detector.go; Test: internal/security/pii/detector_test.go | `go test -p 2 ./internal/security/pii/ -run TestDetector -v` | package missing -> compile RED | each class detected in a mixed sentence; overlapping/nested matches resolved (longest-first, non-overlap); disabled config returns no spans; Luhn rejects 16-digit non-card numbers | Given "contact a@b.com or 555-0100"; When Detect; Then spans for email+phone with classes; Given "4111111111111111" and "0000000000000000"; When Detect credit_card; Then first matches, second does not (Luhn) |
| T2 | SwapStore: durable session-scoped rows (sqlite file under caller-provided dir), in-memory map for the process lifetime; Put/Resolve/ResolveForRestore; ResolveForRestore replaces ALL tokens belonging to sessionID in text and returns (restored, count); tokens from OTHER sessions are left untouched | Create: internal/security/pii/store.go; Test: internal/security/pii/store_test.go | `go test -p 2 ./internal/security/pii/ -run TestSwapStore -v` | API missing -> compile RED | persistence across store reopen (durable proof); cross-session token left as literal token (never resolved); no plaintext in error strings | Given Put(sessA,tok,val) + reopen; When Resolve(sessA,tok); Then val. Given tok belonging to sessA in sessB's text; When ResolveForRestore(sessB,...); Then tok untouched, count 0 |
| T3 | Swapper: Mask replaces spans with fresh `[PII:<uuid-v4>]` tokens (crypto/rand), records Put per token; Restore delegates to ResolveForRestore; round-trip property test (Mask then Restore returns original for the same session) | Create: internal/security/pii/swapper.go; Test: internal/security/pii/swapper_test.go | `go test -p 2 ./internal/security/pii/ -run TestSwapper -v` | missing -> compile RED | round-trip equality; token format matches `^\[PII:[0-9a-f-]{36}\]$`; predid-clean randomness (no math/rand) | Given text with PII; When Mask; Then zero PII remains and count>0; When Restore same session; Then original text returns; Restore different session returns masked text unchanged |
| T4 | Ordering pin (sanitizer-ordering class): a test that builds a HookRegistry, registers SecurityTransformContext, SecretObfuscationHook, and a stub pii transform in the prescribed order, runs TransformContext, and asserts the pass order (security-transform -> secret-obfuscation -> pii stub). Asserts registration-order semantics of HookPriorityCritical peers | Test: internal/agent/pii_hook_order_test.go | `go test -p 2 ./internal/agent/ -run TestPiiHookOrderPinned -v` | order unpinned/no pii stub registered -> RED | green; documents that pii-restore registers AFTER secret-obfuscation | Given the three critical hooks registered in order; When a transform pass runs containing a secret and a PII span; Then secret obfuscated before pii stub observes the text (stub records sequence) |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2 ./internal/security/pii/... ./internal/agent/ -run TestPiiHookOrder`)
- [ ] Contracts C1-B/C2-B satisfied exactly (names, signatures, token format)
- [ ] No plaintext PII value in any log/audit/env path (grep the new package
      for slog/Setenv/audit calls touching Value)
- [ ] crypto/rand only (predid clean); no mutex across I/O (mutexio clean)
- [ ] gofmt clean; no debug artifacts; no line-number corruption
- [ ] No scope creep: no loop.go/components.go/config edits (leaf 02)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines per verify run]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing incl. T4 order pin
- [ ] Contracts exact: Span fields, token regex, cross-session refusal in store
- [ ] sqlite durability proven by reopen test, not assumed
- [ ] In-process-only value discipline (audit-secrets-value-callers spirit)
- [ ] No debug artifacts; no line-number corruption; no commits made

Output: APPROVED or specific gaps with file + line references.

## Notes

- Keep Span.Value unexported-visible only inside the package where
  possible; the store never logs it; error values wrap indices, not text.
- MEEPT_SECRET broker tokens must be OUT of scope for the detector's
  regex classes (no class matches `[A-Z_]+:[a-z0-9-]+` shapes); note any
  collision risk in the report.
- The durable dir does not exist in this leaf - NewSwapStore creates it
  (MkdirAll 0700); leaf 02 picks the path under MEEPT_HOME.
