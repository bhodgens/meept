# Restore Lane + Config + Wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.
> This leaf DEPENDS on 01-detector-store.md being REVIEWED (pii package
> exists). IMPORTANT: components.go may have been edited by the parallel
> branch - re-read it fresh before patching.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Restore-on-output hook at the ScanOutput choke point
  (session-bounded), secret `#XXXX#` restore (dead Deobfuscate lane fix),
  `[security.pii]` config, and components.go wiring with typed-nil guards.
- **Dependencies:** 01-detector-store.md (REVIEWED), R0 decisions D3-D5
- **Estimated Context:** 90K
- **Concurrency Group:** C
- **Audit references:** proposal 8 (master.md R5-R9, R10)

## Goal

Close the loop so no `[PII:...]` token or `#XXXX#` secret placeholder ever
reaches a user reply, conversation history, LoRA/trace capture, or audit
row: restore happens at the ScanOutput choke point
(internal/agent/loop.go:3213 neighborhood), BEFORE any persistence or
surface, and only for tokens originating in the SAME session.

## Context

- internal/agent/loop.go:3213 - `scannedText, hasCredentials, warnings :=
  l.securityOrch.ScanOutput(response)` - THE choke point: restore must run
  on the response before/around this so every downstream consumer
  (reply-guard, history append, trace capture at loop.go:5353+) sees
  restored text.
- internal/security/secrets.go:250 - Deobfuscate (zero external callers -
  this leaf gives it its first real one). Deobfuscate is read-locked and
  replaces all known placeholders; safe to call per-session since the
  obfuscator is process-wide (its maps are not session-scoped - restore
  #XXXX# only when the session's own TransformContext pass obfuscated;
  gate on config).
- internal/security/secrets.go:188/:284 - Obfuscate/DeobfuscateMessages.
- internal/agent/security_hooks.go:160-200 - SecretObfuscationHook shape
  to mirror for the new restore hook.
- internal/daemon/components.go:1976-1984 - secret-obfuscation
  registration; pii-restore registers immediately after it.
- internal/config/schema.go:2332 - SecurityConfig (add Pii field; both
  json+toml tags; default zero-value false = opt-in).
- pii package from leaf 01: Swapper.Mask/Restore, NewSwapStore(dir).
- Leakage classes (master C3): user replies, history, trace capture
  (loop.go:5353+), audit rows, logs.

## Verified Anchors

- internal/agent/loop.go:3213 - ScanOutput call (choke point)
- internal/agent/loop.go:5353-5365 - trace capture records tool output
- internal/security/secrets.go:250-260 - Deobfuscate
- internal/agent/security_hooks.go:180-200 - TransformContext signature
- internal/daemon/components.go:1976-1984 - secret-obfuscation registration
- internal/config/schema.go:2332-2394 - SecurityConfig struct

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C3-B: restore at the choke point - hook-driven, pre-persistence
// C4-B: config
type PiiConfig struct {
    Enabled bool `json:"enabled" toml:"enabled"`                    // false default
    Classes []string `json:"classes" toml:"classes"`                // defaults per R0 D1
    RestoreWindow time.Duration `json:"restore_window" toml:"restore_window"` // 0 = session lifetime
}
// internal/agent/security_hooks.go (or new internal/agent/pii_restore_hook.go):
func NewPiiRestoreHook(swapper *pii.Swapper, obfuscator *intsecurity.SecretObfuscator,
    cfg config.PiiConfig, logger *slog.Logger) *PiiRestoreHook
func (h *PiiRestoreHook) RestoreOutput(text, sessionID string) string
// Exposes also the components.go wiring: hookRegistry registration
// "pii-restore" directly after "secret-obfuscation" (C5-B order), typed-nil
// guarded; nil swapper/obfuscator -> hook nil -> not registered.
```

### What This Leaf Consumes

- C1-B/C2-B pii.Swapper/Store - from 01-detector-store.md
- pii.Swapper.Restore(text, sessionID) (string, int)
- intsecurity.SecretObfuscator.Deobfuscate(text) string

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | PiiRestoreHook: RestoreOutput runs pii.Swapper.Restore (session-bounded) then, when cfg gates it, SecretObfuscator.Deobfuscate for `#XXXX#`; never logs values; typed-nil guards (nil swapper -> constructor nil) | Create: internal/agent/pii_restore_hook.go; Test: internal/agent/pii_restore_hook_test.go | `go test -p 2 ./internal/agent/ -run TestPiiRestore -v` | missing -> compile RED | tokens restored only for the matching session; obfuscated secrets restored; disabled config -> text returned byte-identical | Given masked text + same session; When RestoreOutput; Then PII restored. Given other-session token; Then token survives verbatim. Given disabled config; Then byte-identical output |
| T2 | Wire RestoreOutput at the ScanOutput choke point: in loop.go where ScanOutput runs (:3213), apply the hook to the response BEFORE reply-guard/history/trace persistence; hook held on the loop via a LoopOption set from components.go; sessionID = the loop's session | Modify: internal/agent/loop.go (~:3213 + option), internal/agent/options.go (or wherever LoopOptions live); Test: internal/agent/loop_pii_restore_test.go (new) | `go test -p 2 ./internal/agent/ -run TestLoopPiiRestore -v` | no restore in loop -> RED (masked token reaches the recorded response) | recorded/final response has PII restored; trace-capture path (loop.go:5353+ neighborhood) sees restored text; no-hook loop unchanged | Given a loop with the hook and a response containing `[PII:u]`; When the turn finalizes; Then response + captured output contain the original value; Given a loop without the hook; Then behavior byte-identical to today |
| T3 | Config: add Pii PiiConfig to SecurityConfig (schema.go) with zero-value defaults (Enabled false); template comment in config/meept.json5 [security] section documenting opt-in; wire construction in components.go: build Swapper+store under config.MeeptPath("pii_swap") when enabled, construct hook, register "pii-restore" TransformContext AFTER secret-obfuscation (:1976-1984); typed-nil guards | Modify: internal/config/schema.go, internal/daemon/components.go, config/meept.json5; Test: internal/config/pii_config_test.go, internal/daemon/pii_wiring_test.go | `go test -p 2 ./internal/config/ -run TestPiiConfig && go test -p 2 ./internal/daemon/ -run TestPiiWiring -v` | no config field/wiring -> compile RED | defaults test: zero-value config = disabled; wiring test: enabled -> registry contains pii-restore ordered after secret-obfuscation; disabled -> absent; removing registration FAILS the wiring test | Given zero PiiConfig; When defaults applied; Then Enabled==false and daemon behavior unchanged; Given enabled config; When hooks build; Then pii-restore present after secret-obfuscation |
| T4 | Secret Deobfuscate lane fix proven: an end-to-end unit through hook construction showing a secret that entered the LLM context obfuscated comes back restored at output (the previously dead lane); obfuscation side still covered by existing tests (must stay green) | Test: internal/agent/pii_restore_hook_test.go (extend) | `go test -p 2 ./internal/agent/ -run TestSecretPlaceholderRestore -v && go test -p 2 ./internal/security/ -run TestSecretObfuscator -v` | dead lane -> RED (placeholder survives RestoreOutput) | `#XXXX#` -> original secret at output within the same process; existing obfuscator tests untouched and green | Given Obfuscate(API_KEY_VALUE) -> #AB12# in a response; When RestoreOutput with gating on; Then API_KEY_VALUE returns; obfuscator maps unchanged in behavior |
| T5 | Leakage-class closure assertions: grep-style tests (or build-tagged audit test) asserting no new code path logs swap values: hook logs only counts/classes; verify trace capture + auditlog paths receive RESTORED text (i.e., no token rows); document residual classes in the report | Test: internal/agent/pii_restore_hook_test.go (extend); docs note in report | `go test -p 2 ./internal/agent/ -run TestPiiLeakageClasses -v` | unrestored text reaching capture -> RED | capture paths see restored text; hook emits counts only | Given a finalized turn with PII; When trace capture + audit write run; Then stored output has no `[PII:` tokens; hook logs contain counts only |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
  (`go test -p 2 ./internal/agent/... ./internal/security/... ./internal/config/... ./internal/daemon/...`)
- [ ] Contracts C3-B/C4-B/C5-B satisfied exactly
- [ ] [security.pii] defaults false; disabled path byte-identical
- [ ] Cross-session restore refused (leaf 01 store test + hook test)
- [ ] Sanitizer C0-first pinned test green; hook order pinned (leaf 01 T4 test green with real registration)
- [ ] No plaintext in logs/env/audit; typed-nil guards at every seam
- [ ] gofmt clean; no debug artifacts; no line-number corruption
- [ ] No scope creep (no taint-lane work - that is branch A)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines per verify run]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing; wiring tests fail when
      registration removed (verified, not assumed)
- [ ] Restore strictly session-bounded; `#XXXX#` lane fixed (T4 RED->GREEN)
- [ ] Ordering: pii-restore after secret-obfuscation; C0 sanitizer untouched
- [ ] Disabled config byte-identical; typed-nil guards; no value logging
- [ ] mutexio/predid clean; no debug artifacts; no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- components.go is SHARED with branch A: whichever leaf lands second MUST
  re-read the file fresh (stale read-state) and preserve both
  registrations; master contract C1 defines the final order.
- Deobfuscate is process-wide: gate secret restore on the obfuscation hook
  being active for this daemon instance (it always is today,
  components.go:1976-1984) and record that decision in the report.
- RestoreWindow > 0: enforce in the store's Resolve (row timestamp check);
  default 0 = session lifetime (no expiry logic needed beyond session ID).
- Do NOT touch MEEPT_SECRET broker placeholders (internal/secrets) beyond
  ensuring the PII regex classes cannot match them.
