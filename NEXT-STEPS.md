# NEXT-STEPS — bughunt wave 2026-09-04 (fix wave: prior findings + new delta)

Full report: `.hermes/audits/meept-week-2026-09-04.md`
Fix wave landed as commits 0088ab38 (H1-H11 + M7/M13) and d5714492 (H7), plus
c6c07f0b (C1) and 39884594 (C2 leg 1) from parallel sessions — every fix
parent-verified against the tree.

Round 2 (auditor reports → same-session follow-ups, UNCOMMITTED working tree):
- **D-C3** cycle-detection panic (`firstArgs[:8]` on "empty") — FIXED, tested.
- **D-H2** unsynchronized reasoningOverride read (RPC race) — FIXED.
- **G** legacy-memory hardening: GetByID keeps `is_current=''` rows visible;
  version MAX now CASTs to INTEGER — FIXED (data loss stopped).
- **A-HIGH** codex SSE was buffered (streaming cosmetic + keep-alive stall) —
  FIXED: resp.Body now consumed incrementally; all 17 SSE tests green.

## Headline

The 2026-09-03 report-only wave is now CLOSED OUT: both CRITICALs and all 11
HIGHs are fixed and tested. Chat throttle-parking works end-to-end (parker
started, parker inherited by per-session loops), parked turns no longer fake
success, tier-2 employee episodes survive parking, and paused employees no
longer execute parked episodes.

## Fixed this wave (C1, C2, H1-H11, M7, M13 — all verified)

1. **C1** worker requeue no longer returns holding w.mu (+ regression test).
2. **C2** throttleParker.Start(ctx) + ConfigSnapshot parker propagation.
3. **H1** TTSR third-call parked check (no double execution).
4. **H2** parked turns skip reflection/learning/history/worker-completed.
5. **H3** resume no longer duplicates the user message (WithResumedTurn).
6. **H4** employee episode dedup (employee:phase:trigger key; ~96x → 1).
7. **H5** tier-2 assess resume re-enters decideTier2 (plans actually created).
8. **H6** resume honors the pause gate (re-parks at 15m while paused).
9. **H7** ftstore migration uses INTEGER for version/is_current (no stall at 9).
10. **H8** intent-type filter via IsValidIntentType (agent-ID pollution gone).
11. **H9** allowed_urls config field; hooks auto-allow their own URL (feature live).
12. **H10** unblock_at canonical on park events + dual-read in TUI and Flutter.
13. **H11** park events keep raw session_id (WS filter no longer drops them).
14. **M7** PublishBlocking no-subscriber logs at WARN again (drop is visible).
15. **M13** NormalizeRank now rises with match strength (min_relevance works).

## Found, NOT fixed (decisions or clean llm-zone window needed)

- **D-H1** duplicate-search rollback can spin at iteration 1 (needs per-turn rollback cap + tests)
- **D-H3** guard state never resets between turns (Reset() exists, zero callers — wiring + tests)
- **I-M8 root cause** neither TUI nor Flutter parses `reason`; Flutter copyWith is null-preserving so stale badges stick — needs reason plumbed into both payloads + copyWith nil-override (own PR)
- **A-M** deltas duplicate across provider rotation (consumer contract needed); codex 429s lack RateLimitError typing
- **M3/M4/M5** (llm rotation ignores NonRetryable; streaming auth-arm; RetryAt off-by-one): needs a quiet llm window + a tested PR
- **M6** AGENTS.md chat.response invariant doc is stale (doc-only fix)
- **M9** HH:MM rendered in three timezones — needs an RFC3339-with-tz convention
- **M11** json5 duration rewrite breaks compact JSON5 — deliberate line-based design post-aa7eecfe; proper fix is a tokenizer pass
- **M14** pacing double-claim window (pacing default-off; needs reservation design)
- **G-L** vote-store FD leak on Close; accesses=0 in eviction scoring; FK delete-order hazard; consolidation.go:307 nil-check ordering (latent)
- LOW 1-16 from the prior report: dispositions unchanged.

## Product decisions for you

- retry_count: 0 still means 3 (doc the -1 opt-out at the config surface?).
- M8/M9 surface parity calls above.
- H9 semantics: hooks now auto-allow their own URL when allowed_urls is unset —
  tighter alternative (deny by default) would keep the feature dead; say the word
  if you want a config validation error instead.

## Verification debt (disclosed)

- -short mode everywhere; -race and the full suite still not run.
- flutter analyze ran on the touched file only.
- One sibling test edit (internal/agent/registry_test.go, chat-dispatch-ux leaf)
  was mid-flight and not compiling at handoff — theirs, untouched, likely
  transient within their session.
- Gate matrix at handoff: build green; employee/memory/bus/tui/pkg/sqlite/
  config/comm/worker/daemon/llm/acp/skills tests green -count=1; gofmt/vet/
  predid/mutexio clean.
