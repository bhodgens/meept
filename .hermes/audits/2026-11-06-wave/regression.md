# Adversarial regression review — meept 0a446a21..HEAD

- Scope: regression review of fix/feature commits in `git log 0a446a21..HEAD` (288 commits) + e2e harness assertion-integrity
- HEAD SHA at audit start: `6b42213b` (fix(security,agent): restore voice_call permission rows; make rewake eviction test deterministic)
- Timestamp: 2026-11-06T18:42:40Z
- Excluded (already closed): prior wave `.hermes/audits/2026-10-30-bughunt-wave.md`, PB-1/PB-2 (fixed in 6b42213b)
- Method: read-only; findings appended as confirmed. Commit-message-claim vs diff, mutation-read of test assertions, over-narrow/over-broad conditions.

## Findings

## F-1 [MEDIUM] e84be514 deleted `githubsvc.RegisterGitHubTools` but AGENTS.md still documents it as THE registration entry point
- `AGENTS.md:325` (root): "`githubsvc.RegisterGitHubTools(reg, opts)` consults the package-level `githubsvc.ToolFactories` factory…" — also the Critical-Invariants section header "The github_* tool factory seam is init-populated" describes the removed constructor path.
- `internal/githubsvc/register.go:83` (HEAD): tombstone comment "RegisterGitHubTools was the registration entry point; it was removed as…" — the symbol is gone; provider path is the only route.
- Evidence: `git show e84be514 --stat` = 6 files, none is `AGENTS.md`. `grep -n RegisterGitHubTools AGENTS.md` at HEAD `6b42213b` still hits line 325.
- Why a regression: root AGENTS.md's own maintenance rule ("Every commit MUST review this file… If a commit removes… update this file in the same commit") and its warning "Stale agent guidance causes more bugs than no guidance at all." Any agent following the invariant writes code against a deleted API; the file is loaded into every session's context at this very path.
- Fix shape: rewrite the AGENTS.md invariant section + package-table row to the toolprov provider path (as `docs/workflows/toolprov.md` and `internal/toolprov/provider.go` describe), in one commit.

## F-2 [MEDIUM] 7e4748d4 fakellm recall arm contradicts its own comment — "SetPostToolText still wins" is not implemented; over-broad arm
- `e2e/harness/fakellm.go:659-663` (HEAD): the new arm returns `f.chatText` whenever `f.chatText != ""` and the prompt contains `Memory (standing)` or `<memory-context>`, evaluated BEFORE the `f.postToolText` default return.
- The comment at :654-656 claims "Explicit SetPostToolText still wins for suites that script the post-tool reply directly: those calls set postToolText AFTER the default…" — but `SetPostToolText` (:398-400) is a bare field assignment with no explicitness flag; there is no code path by which it "wins" when both a pinned `chatText` and memory markers are present. The claim in the diff is not shipped by the diff.
- Why a regression (assertion-integrity): 8+ suites use BOTH `SetPostToolText` and a pinned chat text (`refusal`, `dispatcher`, `memory-turn-capture`, `llm-hop-tracing`, `goal-completion`, `breakers`, `acp-persona-route`, `continuity-regressions` — grep of committed HEAD). Combined with 5da890a1 flipping memory subsystems default-ON, any such suite whose executor turn carries an injected-memory marker now silently gets the chat-pinned reply instead of the scripted post-tool reply — assertions that pass today can flip meaning without any production change. The harness is the measurement instrument; its documented precedence is wrong.
- Fix shape: add `postToolTextExplicit bool` set by `SetPostToolText`, and gate the recall arm on `!f.postToolTextExplicit` — or scope the arm to the recall suite via an explicit opt-in setter instead of marker-sniffing every prompt.

## F-3 [LOW-MEDIUM] 5a2f050e AGENT-10: claim of "callers cancel and drain the builder" not shipped — `BuildIndexWithCancel`/`Stop` have ZERO callers; the only production build path still calls `BuildIndex` directly
- `internal/agent/intent_index.go:47/160` (HEAD): `BuildIndexWithCancel` and `Stop` added; commit message: "Adds BuildIndexWithCancel (cancellable ctx + WaitGroup) and Stop() so callers cancel and drain the builder instead of leaking it."
- Evidence: `grep -rn BuildIndexWithCancel internal/ cmd/` → only the definition + its own internal call of `idx.BuildIndex`. Production builder is `internal/agent/dispatcher.go:686` (`d.semanticIndex.BuildIndex(d.indexCtx)`) — untouched by the commit. The leak the finding described stays exactly as before; the seam is dead code that reads like a fix.
- Secondary defects in the new code: (1) the background goroutine's `buildErr` is swallowed in an empty `if` body (ignored error — AGENTS.md "no ignored errors" rule; note commit was made with `--no-verify`); (2) `idx.cancel` is written in `BuildIndexWithCancel` and read in `Stop` with no mutex, while every other field of `SemanticIndex` is `idx.mu`-guarded — a latent data race the moment anyone wires it (the repo's own typed-nil/lock conventions would flag it); (3) the named return `err` is always nil even when the goroutine later fails.
- Mutation-read of pins: no test accompanies the seam (test diff in 5a2f050e is only the `SetLinters` switch in orchestrator_reflection_scope_test.go), so reverting the whole AGENT-10 hunk breaks nothing — a fake fix by the "no test pins it" criterion.
- Fix shape: either wire `dispatcher.go` through `BuildIndexWithCancel` + call `Stop()` on dispatcher shutdown (with a pin test), or drop the seam and re-file AGENT-10. Guard `idx.cancel` under `idx.mu`.

## Audited-clean

Areas examined and found sound (within time-box; no finding filed):

1. **Completion-dedup / attempt-freshness forwarding (H2 class)** — verified the forwarding is complete for every production queue type. Only two `Queue` implementations carry `Complete` (grep of `internal/` + `pkg/` at HEAD: `internal/queue/queue.go:473` PersistentQueue, `internal/queue/cluster_queue.go:218` ClusterQueue); both forward `CompletionIsFresh` (queue.go:539 → store; cluster_queue.go:380 → wrapped Queue with `ok=false` documented fail-open), both expose `CompleteAttempt`/`FailAttempt`. The pin `TestQueueExposesAttemptCapabilities` (internal/queue/store_fail_attempt_test.go:34) asserts the CONCRETE types satisfy the mirrored interfaces — a real pin per the H2 lesson (a hand-rolled double cannot catch a missing forwarder). `ClusterQueue.Complete` presents `record.ClaimToken` so the token-exact path is taken in cluster mode, not the state-only fallback. `OnJobCompleted` (internal/agent/tactical.go:1249) layer-1/layer-2 split matches its documented contract.
2. **Hooks-hardening wave e9d20f3f — silent-skip check.** Read `.githooks/pre-commit-consumer-tests` in full: `go list` failure → exit 1 (explicit "not a pass"), module-unresolved → exit 1, dirty-consumer exclusion is a documented loud `SKIP(dirty WIP)` echo, no silent `|| true` in the gate body; the ENVIRON-export bug it names is fixed within the same diff (verified `export STAGED_IMPORTS` at HEAD). Chain wiring in `.githooks/pre-commit`: gate 2 is `|| exit 1` (blocking). The two advisory `|| true` gates (concurrency 14/20, mutation 15/20) are PRE-EXISTING (identical at e9d20f3f^), not introduced by this range. Pre-push `"$remote"` empty-variable arg is pre-existing (context line in the e9d20f3f diff, not a `+`), excluded per scope.
3. **Scratch-bin reaper** — `git log 0a446a21..HEAD -- e2e/harness/daemon.go` returns EMPTY: the reaper was not touched in this range; the AGENTS.md description matches the committed file. Nothing to regress here.
4. **98c85b11 turn-journal queue.db path** — `MeeptHome→StateDir` correction is the right side of 5da890a1's `Queue.DBPath` default-empty flip (daemon.data_dir/queue.db fallback); both read sites updated symmetrically.
5. **d614d0d3 worker queue-swap push** — message documents mutex-guarded handle swap + snapshot-under-RLock of all `w.queue` reads; worker/queue suites claimed green; no contradiction found between claimed files and `--stat`.
6. **5a2f050e AGENT-13/14 hunks** — validation-gate counter cleanup is inside the existing `validationGateMu` (no lock-scope violation); AGENT-14 nil-guard keeps malformed (non-nil) results on the same debug-log path as before (behavior-preserving for the parse-failure case). Only the AGENT-10 seam is defective (filed as F-3).
7. **e8dcb9c2 / d4f1deec / 7bd0998b cluster security mirroring** — `SetRequireDispatchSignatures` mirror call respects the knob (default true via `NormalizeClusterSecurityDefaults` at both load boundaries per 7bd0998b); stale-claim reclaim carries fresh attempt tokens (store_cluster_claim.go:90, H4 contract cited in-code).

## Notes

- Working tree carries sibling WIP in `e2e/` (manifest.json + task-queue test); all greps/reads above against COMMITTED state (`git show <sha>:` / `git log -- <path>`), per instructions.
- Gates exercised: none needed (no build-affecting edits; audit was read-only + findings file). `go build ./e2e/...` not run — no e2e source was modified or claimed broken by a compile-level finding.
- 288 commits in range; ~35 substantive logic commits examined per priority list (async-turn/dedup/reaper/hooks/classifier). Docs/plans commits (~60% of range) were scanned for claim-vs-stat only.
- F-2 was confirmed by reading the committed harness code; no suite failure was executed (would need full e2e, owned by parent).

## Wave closeout (parent, 2026-11-06)
- F-1 CLOSED: AGENTS.md Key Components row + invariant section rewritten to
  the toolprov provider path (SetGitHubProvider + toolprov.RegisterAll;
  ToolFactories documented as the mirrored construction seam; the removed
  RegisterGitHubTools is now named as removed, matching the register.go tombstone).
- F-2 CLOSED: postToolTextExplicit flag implements the claimed precedence (SetPostToolText wins over the recall arm; cleared in Reset). Suites green: continuity-regressions, refusal, memory-defaults, memory-scoping.
- F-3 CLOSED: removed the dead SemanticIndex.BuildIndexWithCancel+Stop pair (zero callers, unshipped "fix"). The real drain already lives on Dispatcher.indexCtx/indexWG, cancelled+waited in Dispatcher.Stop (called from Components.Stop:6902).
