# NEXT-STEPS — Grok-Bot Blueprint Gap Implementation

Date: 2026-09-06. Source: X article "How to Master Graph and Loop
Engineering using Grok Bot" (@Av1dlive,
https://x.com/i/article/2092573868011745281) + gap analysis from session
2026-09-05. All four plan trees COMPLETE; full `go test ./internal/...`
green (90 packages, exit 0) at cb7d3e36.

## What landed (this program, 16 commits)

Trees 1-3 (gaps #1, #2, #4) + tree 4 (#3, full frontier):

1. **Tamper-evident audit chain** — b19d6ca7, 6224b28c, 271b613c.
   internal/auditlog: canonical JSON, SHA-256 hash chain, VerifyChain,
   SQLite append-only store; audit findings + gate results chain into it
   (sanitized, output_sha256 never raw); AnchorJob JSONL digests;
   `agents.audit.verify` RPC; `meept agents audit --verify`.
2. **External-effect idempotency ledger** — cbe4cce3, 2961ad32, eb50b9a2.
   internal/effects: atomic Claim (INSERT PK conflict), receipts,
   completion markers, ReconcilePending, deterministic EffectKey; push
   notification + backup git push wired; startup + parked-turn-resume
   reconcile (auto-retry only provider-idempotent effects);
   effects.list/effects.reconcile RPC (direct handlers, no bus proxy);
   `meept effects list|reconcile`.
3. **Claim temporal validity** — 08d41590, dd72e8d7, dd78783b, b03af1a0.
   Claim gains ObservedAt/ValidFrom/ValidTo/Rev (metadata-keyed,
   zero-value backward compatible); supersede stamps rev+1 /
   valid_to in place; expired claims hard-excluded from canonical
   selection + detection; ListExpiredClaims; retain_claim params +
   list_expired_claims tool + `meept memory expired`.
4. **Phase frontier parallel dispatch** — 764f8c3c, 3dac0963, 637cdeb9,
   7c6c8a44, cb7d3e36. Pure computePhaseFrontier; startPhase extraction;
   advancePhasesFrontier (CAS single-flight, race-proven); per-phase
   worktree provisioner + PhaseWorktree accessor; parallel-aware budget
   hierarchy (per-phase maps); `plans.parallel_phases` opt-in (default
   FALSE — serial preserved); daemon wiring + worktree consumption;
   wiring test proves flag flips dispatch behavior.

## Notable fixes found during review (landed with the work)

- modernc.org/sqlite ignores `_journal_mode`-style DSN keys — functional
  stores already use `_pragma=`; effects store sets WAL via PRAGMA +
  SetMaxOpenConns(1). **The park store has the same latent issue**
  (candidate follow-up).
- Superseded claims are unreachable via GetByID (pre-existing is_current
  filter) — tests load via GetVersionHistory.
- startPhase re-entrancy guard originally missed started-not-yet-
  scheduled steps (still StepPending) — a stamped ConversationID now
  also counts as started (observed double-start B->C->C->D, fixed).
- Duplicate-send push test raced non-blocking bus fan-out; wait now
  covers both session topics.

## Open follow-ups

1. **memory.listExpired RPC handler** (claim-validity OPEN-QUESTIONS Q4):
   one-file follow-up in internal/rpc/epistemic.go. Until it lands,
   `meept memory expired` round-trips to the daemon and gets
   method-not-found; the in-process tool works today.
2. **Production worktree provisioner**: leaf 04 wires flag + consumption
   only; a real provisioner (git worktree factory per active phase) is
   the remaining piece to make parallel phases write-isolated. Until
   then, run parallel phases only on plans without file-write conflicts.
3. **Park store DSN** (above) + Abandon-reason persistence (effects
   schema has no reason column; documented in code).
4. **Scratch files** (rm denied by shell approval during session — delete
   manually): ~/git/meet (typo'd repo path), meept internal/auditlog_tmp/,
   meept internal/agent/tmpdbg_test.go, meept cmd/skillparse_main.go
   (parallel session's stray; breaks repo-wide `go build ./...`).
5. Pre-existing (not this program): full-repo mutexio failure in
   internal/agent/intent_session_rules_test.go:83; enforcement.go's four
   grandfathered `_ = autoPause(...)` sites.

## How to use

- Audit chain: automatic; verify with `meept agents audit --verify`.
- Effects: automatic for push + backup push; inspect with
  `meept effects list`; stuck non-idempotent effects need an explicit
  `--complete` or `--abandon --reason <r>`.
- Claim validity: set valid_to when retaining claims; review with
  `meept memory expired` (needs follow-up #1) or the agent tool.
- Parallel phases: set `plans.parallel_phases: true` in meept.json5;
  only meaningful for plans whose phases declare Produces/Consumes.
  See docs/workflows/agent-orchestration.md.
