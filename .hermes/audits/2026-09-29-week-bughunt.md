# Meept weekly bughunt 2026-09-29 — verified findings (report-only)

Scope: 223 commits, September 22-29, `2d733ee8..8cbb990f`. HEAD at audit start
8cbb990f; two sibling commits landed mid-wave past the range end and are
UNAUDITED here: bf6150c2 (rotatingWriter pre-lock fast-path race fix — follow-up
to 47ed44f4) and 12418ca8 (docs). Five read-only auditor reports (scopes A-E,
delegation deleg_5334d32b); parent source-verified every HIGH and the top
MEDIUMs at final HEAD 12418ca8.

Baseline (parent, verified): full Go suite `-p 2 -count=1` green at 8cbb990f
(/tmp/meept-week-0929-go-baseline.log; two packages failed once on macOS
ephemeral-port exhaustion and passed clean on isolated `-p 1` re-run). Both
binaries build. Report-only: no repository fix applied.

## Findings

### HIGH

**H1 — Recall continuity shape-gate is a no-op for every non-recall label.**
`internal/agent/recall_answer.go:37` rejects any intent whose Type is not
`recall`; the week's shape-gated branch (`dispatcher.go:3045`, commits
14dd4145/ee0274be) fires for ANY label but its body calls `RecallAnswer`, which
immediately returns ("", false) for chat/work/platform labels. For exactly the
runs (38-40, 42) the commits say they fix, the deterministic stored-result
answer never fires; the turn degrades to the LLM path. Parent-verified at HEAD.
The pinned tests pass because their scenarios carry the recall label. Fix
shape: drop the label check inside RecallAnswer (callers already gate on
shape). Twin dead branch: `dispatcher.go:2941` (platform → RecallAnswer) can
never answer (LOW on its own; upstream arbitration makes it dead defensive
code, but the branch comment describes behavior that does not exist).

**H2 — Stale-record orphan sweep ignores `auto_stop=false` operator intent.**
`internal/llm/runtime_sweep.go:555-561` (47844b25): `sweepStaleSpawnRecords`
checks only age / alive / ppid==1 / argv match — NOT `rec.AutoStop` (the
`sweepableRecord` contract the boot sweep enforces, F57/F58) and NOT the
`RuntimeHasLiveOwner` veto. `cmd/meept/runtime.go:347` writes AutoStop=false
records for operator-started runtimes into the exact dir the boot sweep scans.
An operator-started runtime left running >6h (ppid re-parented to 1) is reaped
by the next daemon boot. Violates the documented reaping contract in
internal/daemon/AGENTS.md. Parent-verified: neither guard symbol appears in the
stale sweep. Fix shape: apply `sweepableRecord(rec)` and the live-owner veto in
the stale loop.

### MEDIUM

**M1 — Thread-scoped conv id still used by the digest context block and
guard-fallback arm.** d045a450 fixed only the recall branch
(`dispatcher.go:3048` uses sessionConversationID). `dispatcher.go:3023`
(buildContextMessage) and `dispatcher.go:3087-3092` (SetGuardFallback digest)
still query the thread-resolved id; `GetTasksForSession` JOINs on the
session-level id, so any threaded conversation loses the executing-agent digest
block and the guard fallback — the same A5 failure class at two sibling sites.

**M2 — Digest-aware guard fallback (`l.guardFallback`) is unreachable on the
loop seam.** 24053ced's retry branch always wins when the guard trips
(`guardRetried` is always false at that point), and both retry exits ship the
raw replacement / `applyReplyGuardLogged` — never
`applyReplyGuardWithFallback`. `l.guardFallback` armed by the dispatcher for
exactly this turn is cleared unused (`loop.go:3093`). A retry failure now ships
the generic canned line even though a session digest answer was armed
(ce0cb7e6's whole point). The handler choke point only re-replaces
machine-shaped replies; the loop's canned line passes through.

**M3 — Reply-guard rewrite retry bypasses the parked-turn guard.**
`reasoningCycle` returns ("", nil) on a quota/throttle park; if the RETRY call
parks, `retryErr == nil` → `finalResponse = ""` falls through the entire
post-turn success pipeline the first-cycle `turnParked` guard (loop.go:3020)
exists to skip: empty assistant append (context pollution the resumed turn
inherits), success trajectory, learning trigger, roster gate. Same gap in the
terminate-lane twin (`applyTerminateReplyGuard`). Reachability requires the
second LLM call to park — the exact condition universal parking exists for.

**M4 — `/plan` slash route still links tasks/plans with the thread-scoped id
(90eb0f45 missed a call site).** `dispatcher.go:950` passes the thread-resolved
`conversationID` to `routeToPlan`; the compound/plan detected routes were
fixed, this one was not. Work created via `/plan` inside a non-general thread
is invisible to every digest/recall lookup (no unwrap on the link key), and
`resolveAgent`'s Get() fallback misses thread ids. Partially mitigated
downstream by ResolveThreadConversationID for workdirs only. (Auditors A-5 and
E-7 converged on this independently.)

**M5 — lsp_rename fence check is dead code; the gosec suppression is
unjustified.** `internal/code/tools/lsp_rename.go:269-273` validates write
paths via `t.fenceChecker`, but the daemon never calls `SetFenceChecker` on
LSPRenameTool (`components.go:7139-7145` — only SetPendingChangesRegistry; all
16 SetFenceChecker sites are other tools). `apply=true` renames write files
with a nil checker (validateWritePath nil-safe → returns nil), and the
`//nolint:gosec` at lsp_rename.go:347 cites the check that never arms.
Parent-verified via grep at HEAD.

**M6 — Telegram push truncation splits UTF-8 runes and ignores escape
expansion.** `internal/services/push_channels.go:309-332`: `text[:maxLen]` is a
byte slice — CJK/emoji cut mid-rune → Telegram rejects the send (400); the
4096 limit is applied pre-escape, so dense special chars exceed the limit
post-format.

**M7 — Eval judge grades "exit status 0" as failure; the new e2e suite pins
the wrong shape (spec-vs-fixture).** `internal/eval/judge.go:23-29`
failureMarkers prefix-match "exit status"/"exit code" with no success
discrimination; `e2e/suites/eval-judge/eval-judge_test.go:57,64` asserts
"exit status 0" → Failed as the intended contract. Any trajectory echoing a
zero exit status grades failed — downward bias on every headline pass-rate.

**M8 — task-queue-02 can SIGKILL a daemon whose task already completed
(spurious failure, one-directional).** `task_queue_test.go:387-396` waits for
non-pending AND non-terminal; a task that completes between 200 ms polls spins
to the 30 s deadline, kills a done daemon, then fails on state=completed.
Cannot pass vacuously (honesty check passed); flake risk under CI load is low
but real. task-queue-03's "reclaimed" claim is graded vacuously for the
pending branch (first-poll pending == sweep-ran is indistinguishable from
sweep-never-ran). Plan-write race suite is timing-blind in one direction: the
conjunction is asserted on a snapshot that can precede the clobber (suggest a
settle-delay re-read).

**M9 — pre-commit-e2e NEW-package check contradicts e2e-affected.sh prefix
semantics.** `.githooks/pre-commit-e2e:98-107` requires a path_map key
`pkg == p || p.startswith(pkg+"/")`; behavioral keys are trailing-slash
prefixes, so a NEW SUBPACKAGE of a covered package (e.g. internal/agent/prompts)
is false-flagged. Parent-verified by the auditor via simulation. Manifest
coverage itself is complete at range end (d7dcb62b).

**M10 (product risk, default-on) — `language_en` validator filter now rejects
non-English step output on every default install.** 25d1db94 flipped
`Enabled: true` (`schema.go:2792-2798`); `filter_language.go:189-194` FAILS
(not advisory) at confidence >= 0.5; FilterFail consumes retries then surfaces
`rejected_exhausted`. Documented as a deliberate 2026-09-22 decision — flagged,
not a bug call. Blast radius bounded: only steps with non-empty ToolHint.

### LOW

- L1 (parent pre-read + auditor C-2, deduped) — `internal/daemon/orphan.go:84-95`:
  stale-reap log reads PID-file mtimes AFTER the sweep deleted them; `record_age`
  always logs 0s and the comment claims the opposite. Observability only.
- L2 — streaming empty-completion exhaustion returns a wrapped ClientError, not
  the bare ErrEmptyResponse sentinel (0-indexed loop + 1-indexed guard,
  `client.go:2155` vs `:2263`; sibling loops correct). errors.Is still matches;
  contract-shape drift only. bf6150c2 (post-range) already touches this file —
  re-verify before fixing.
- L3 — list_directory whitespace-only `path` bypasses the workdir fallback and
  anchors at daemon CWD on unfenced deployments (`filesystem.go:849-867`,
  TrimSpace gap; required-arg gate no longer applies after 464b691a).
- L4 — empty-completion retries are invisible in the token ledger (no
  usage rows on the empty-exhaustion path) — budget-burn blind spot.
- L5 — gossip MaxRetryAttempts bound is per-resurrection (drop deletes the
  counter; re-queue restores a fresh budget → unbounded 3-attempt cycles while
  delivery fails) and successful retries leak `attempts` map entries
  (`gossip.go:627-648`). No off-by-one; zero-config fallback correct.
- L6 — HTTP plan service has no sink fallback (`services/plan_service.go:141-156`):
  sink-plan reject/confirm/approve from the GUI fails "plan not found" (clean
  failure; d9688804 fixed only the RPC surface).
- L7 — informativeResultLine drops prose lines starting "job " (`session_digest.go:217-220`).
- L8 — mutex asymmetry on the twin retry flags: terminate lane locked
  (loop.go:9153-9157), loop-seam `guardRetried` unlocked (loop.go:3044).
- L9 — requires-tools: `SkillIndexEntry.RequiresTools` never populated
  (parser.go:113-124, discovery.go:226-237) — metadata/API visibility gap, not
  a gate bypass; gate fails open when `validate_prerequisites: false`
  (executor.go:241 couples two unrelated flags; default true).
- L10 — backup can still report success with no commit when every Add fails
  (`git_ops.go:135-147` — clean-tree return; 043b7c3f fixed only the
  absolute-path leg).
- L11 — thread-id unwrap by `strings.LastIndex(id, "-thread-")` string suffix
  (thread_resolve.go:45-46) — narrow mis-resolve rather than fail.
- L12 — plan markdown lock map never evicts (filelock.go:23-30); locking design
  otherwise sound (9f503122 verified clean).
- L13 — 654448f4 ("chore(graphs)") re-enables cosine distill dedupe in
  internal/memory/distill.go by deleting dead sequencing — real semantic change
  under a chore label.
- L14 — `file://` URLs misclassified as remote → shallow-clone rejection
  (git_checkout.go:56-59); pull side verified fine empirically.
- L15 — fuzz targets: SSE target fuzzes a hand-copied loop, not the production
  scanner (llm/fuzz_test.go:88-135); validator fuzz fence set missing the real
  ```javascript/```typescript markers. 9ca8fddf also smuggles an unrelated
  runtime_logs.go production nolint move into a test commit.
- L16 — Fake SearchProvider: DuckDuckGo fallback has zero e2e coverage;
  error-path test accepts the fake's own error string (grading the fake).
- L17 — continuity recall T1 acceptance admits the forbidden "Task … completed."
  stub (recall_continuity_test.go:50) — weaker than the sibling suite's own
  A1 contract; fixture gate only.
- L18 — classifier-eval remap hardening test weakened from exact-diff count to
  per-line shape (test_hardening.py:214-250) — selection-on-test.
- L19 — lint_js prose false-negative: all-caps narration classified prose,
  never `node --check`ed (advisory; documented tradeoff).
- L20 — lint host-adaptivity: lint_go unconditional; lint_python filter added
  when `python` exists but the filter hardcodes `python3` — guaranteed advisory
  noise on such hosts.
- L21 — `MEEPT_E2E_FAKE_SEARCH` env silently fakes web_search in any process
  carrying it; no boot log when active (suggest one Warn line).
- L22 — pair.* fire-and-forget contract docs live only in
  internal/agent/pair_channel.go:93-105; internal/comm/AGENTS.md (the file
  governing WS relay) never mentions pair.*. Code matches the documented
  behavior; publishError's drop is fully silent (pair_orchestrator.go:503).
- L23 — AGENTS.md config-flip rule violated twice in-range (9d8b61ab, 3e721ccb
  flipped models.json5; test pins landed 71/83 min later in 43895a63/06661e4f).
  Current TestConfigLoads green — but the /Volumes/LLMs absolute pin makes it
  machine-specific.
- L24 — parent pre-read: reply-guard retry is one extra full reasoningCycle
  (fresh iteration budget), not "exactly one extra model call" as the commit
  message says. Bounded, doc-accuracy only.

### INFO

- Statfs overflow comment claims safety the caps don't provide (product bounded
  2^92 > int64 max; unreachable on real hardware) — cmd/meept/doctor.go:341-344.
- Manifest scenario inventory counts assertion clusters, not graded tests
  (naive-user-chat 5 scenarios ↔ 2 test functions).
- Three e2e suites landed 2-3 commits before manifest registration (window
  where e2e-affected couldn't map the new packages).
- Observed, pre-range, not acted on: handler.go:1136-1146 empty-success on ctx
  cancel; session_digest completed-preference answers status questions from a
  stale completed task while a newer task runs; prefillStash not cleared by
  resetTurnGuards; skills body-only SKILL.md dropped with a lying warning.

## Parent-verified refutations (auditor candidates that died on read)

- repeat_error_limit config-snapshot drift: refuted — full normalization +
  fallback chain verified (schema → GuardConfig → WithRepeatErrorBudget →
  ConfigSnapshot → breaker; daemon raw path covered by triple fallback).
- Message-pairing integrity in both retry seams: clean — no assistant→assistant
  transition introduced; deferred-result synthesis covers dangling pairs.
- 6e2b449b TrimSpace: correct at both sites; alias-failure contract preserved.
- memory_vote ToolActionMap: registered ∧ granted ∧ mapped; sibling sweep clean.
- rotatingWriter race: all shared-file accesses under one mutex (note post-range
  bf6150c2 tightens pre-lock fast paths — unaudited here).
- ec3854e5: the index loop was re-replaced by the legal two-value
  slices.Backward form; reverse order + bounds correct.
- 7d8acd13 roster grants: all five registered ∧ granted ∧ mapped, pinned by tests.
- Gossip zero-config: `<=0 → 3` matches DefaultConfig; neither infinite nor
  zero-shot.

## Disclosure ledger

- The five auditor reports are self-reports; parent verification covered every
  HIGH and the top MEDIUMs (H1, H2, M1, M2-structure, M4, M5, M7, M9, plus
  L1-L2 spot-checks). M3, M6, M8, M10 and all LOWs below M5 are
  source-verified-by-auditor only, not independently reproduced by the parent.
- Mid-wave: HEAD moved 8cbb990f → 12418ca8 during the audit (bf6150c2 rotating
  writer, 12418ca8 docs). bf6150c2 is UNAUDITED and sits on a file with
  findings L2/L4 — re-verify before fixing those.
- Baseline caveat: the full-suite run overlapped the auditors' own test runs;
  2 packages failed once on ephemeral-port exhaustion (known macOS issue) and
  passed on isolated re-run. No other gate (race, analyzers, flutter) was run
  this wave.
- Working tree carries sibling modifications (internal/llm/runtime_logs.go,
  plan docs, untracked internal/security/testdata/) — untouched, unaudited as
  WIP.
- Fix-order note: H1 and its dead twin branch should land together; M3's fix
  should land with any H1/M2 change on the same seam (reply-guard retry block);
  L2's fix must be re-based on bf6150c2 first.

## Suggested repair order (pending go-ahead; nothing fixed)

1. H2 (operator runtime kill) — small, contract-backed, highest blast radius.
2. H1 + dead platform branch — the week's headline feature is half-dead.
3. M3 (parked retry) + M2 (fallback unreachable) — same seam, one edit each.
4. M4 + M1 (thread-id siblings of 90eb0f45) — one unwrap pass over remaining
   call sites.
5. M5 (lsp_rename fence wiring) — one SetFenceChecker call + test.
6. M7 (eval judge "exit status 0") + M8 (task-queue race/vacuous) — grader
   integrity.
7. M6, M9, M10 decision, then LOWs in any order.
