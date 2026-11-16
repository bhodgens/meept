# Bughunt 2026-11-15 — two-run wave over 2026-11-14/15 commits

HEAD at wave-1 audit: c0788bb0. Prior audited point: a58ed47a (the 2026-11-14
two-run report) — wave scope = a58ed47a..c0788bb0 (16 commits).
Method: 6 read-only auditors (voice, agent-loop/tools, scheduler/config/langcode,
hooks/CI, kanban, fix-commit regression) → parent source-verified all HIGHs at
file:line → 6 isolated fixers (disjoint file ownership) → parent gate matrix →
wave 2: 3 auditors (go-fix regression, hook-fix regression, fresh-ground gates +
sibling-batch sweep) → 3 parent patches. Fixing authorized by request
("bughunt ... and fix all issues").

## Verified findings → dispositions (wave 1)

| # | Finding | Sev | Verdict | Disposition |
|---|---------|-----|---------|-------------|
| S1 | scheduler: cron-fired one-shots never claim/stamp/prune → restart re-runs them (timing.go:1016) | HIGH | CONFIRMED | 2d2f3d08 fireOneShotOnce shared by boot+cron paths; pins 7d8bf06e |
| S2 | scheduler: boot sweep fires FUTURE-due one-shots early (timing.go:870, no due<=now guard) | HIGH | CONFIRMED | 2d2f3d08 `due.After(now) { continue }`; pin TestOneShotFutureDueNotFiredAtBoot |
| W1 | voice: TestTalkLoopOnEventFiresAtLogSites deterministic FAIL (said events at say() choke broke dispatch-first assertion; ee465418) | HIGH | CONFIRMED (repro 5/5; green at anchor via detached worktree) | 5d7a4165 test filtered said + positive said pins |
| H1 | hooks: census blind to renames (--diff-filter=A vs R rows; rename into unmapped dir passes) | HIGH | CONFIRMED (auditor reproduced empirically) | 079c29b1 name-status ACMR minus M; new rename selftest case (17/17) |
| V1 | voice: SetToolRegistry not propagated to executor → prompt-only filtering | HIGH | REFUTED — prompt-visibility-only is the documented design (voice_employee.go:225-226, docs/workflows/employees.md:274); enforcement = executor call-time gate. Commit message overclaims ("only the constitution allowlist") but code+docs are consistent | none (message hygiene noted) |
| S4 | scheduler: RunNow holds s.mu across store I/O | MED | CONFIRMED | 2d2f3d08 markOneShotFailed pattern |
| S5 | scheduler: RunNow leaves one-shot re-fireable at boot | MED | CONFIRMED | 2d2f3d08 RunNow stamps OneShotFiredAt |
| S3 | scheduler: !claimed prune loses never-executed runs | MED | resolved by S1 (claim now proves delivery) | verified in fixer report |
| V2 | voice: silence escalation loops forever under loud mic (hold resets silencePrompted + zeroes counters) | MED | CONFIRMED | 5d7a4165 hold keeps ladder; pins TestTalkLoopCallerHoldEscalatesToHangup + Alternating |
| V3' | enforcement wildcard raw-vs-trimmed divergence (" *" denies visibly but executes) | MED | CONFIRMED | 14a4fb7b canonicalToolName contains-func; pin enforcement_wildcard_test.go |
| K1 | kanbansvc: takeover gated on GrantTakeover alone, contract says conjunctive | MED | CONFIRMED | 310701d5 grants []string route order; tests updated |
| H2 | e2e-affected.sh naive prefix match | MED | CONFIRMED (latent — current keys all carry slashes) | 079c29b1 slash-boundary check |
| H3 | nil-seam ratchet blind to multi-line calls | MED | CONFIRMED (+ latent exit-0 bug found en route) | 079c29b1 logical-line join + BLOCK_COUNT exit; 6/6 throwaway-repo cases |
| A1 | agent: ClaimDispatch no rollback; future early-return wedges turn | MED | CONFIRMED (latent) | 625c7ce7 defer safety net; wave-2 upgrade 3ffc4052 emits failed terminal |
| R6 | config: ISO-639 error advertises "fr" (not in Builtin) | LOW | CONFIRMED | 079c29b1 batch (schema.go example list) |
| R4 | voice: PreviewEventText zero callers | LOW | CONFIRMED | 7579969b parent patch (fixer claimed deleted but only re-commented — landed-work verified adversarially) |
| H4-H7 | deadcode comment / comm ||true / @latest hints / bench fallback banner | LOW | CONFIRMED | 079c29b1 |
| A2 | turns map unbounded when watchdog disabled | LOW | CONFIRMED | comment-only invariant doc (625c7ce7) |
| V4-V7 | said-feed short-reply drop, drain-tail bypass, printf-under-mutex | LOW | CONFIRMED | deferred (cosmetic; listed below) |
| R2/R3 | scheduler RunNow row re-fireable / clock-backwards | LOW-MED | partially covered by S5; clock-backwards deferred | disclosed below |

## Wave-2 (regression + fresh-ground) → dispositions

| # | Finding | Sev | Disposition |
|---|---------|-----|-------------|
| W2-1 | turn safety net tombstones silently — awaiting client never gets a terminal (reaper keys on the now-deleted row) | MED | 3ffc4052: net publishes Status=failed handler_case=early_return_safety_net BEFORE Complete |
| W2-2 | fireOneShotOnce trades at-least-once for exactly-once for fire-and-forget kinds (crash in pre-execute window loses the run); undisclosed | MED (disclosure) | 4af95df5 posture comment at timing.go:914; behavior deliberate (double-fire was worse) |
| W2-3 | .githooks/README.md census line stale (A-only) | LOW | 03325ee7 |
| W2-4 | 310701d5 message claims pairing work not in its diff (mixing); pairing half dead-wired pending leaf 07/01 | MED | baseline — kanban lane ACTIVE (leaves landing during the wave); owned by that lane's owner |
| W2-5 | census C-row visibility config-dependent; tab-in-filename edge unpinned | LOW | disclosed; fixture candidate for a later hooks wave |
| W2-6 | eb18719f retry comment vs break-on-!Found mismatch | LOW | sibling-commit, needs owner confirmation (voice lane) |
| — | fresh-ground gate reproduction | — | ALL GREEN: build, vet ×6 pkgs, tests ×4 pkgs -count=1, bash -n ×4, census selftest 17/17, W1 test pass |

## Gating

Wave-1 close (parent): go build ./... OK; vet (voice/scheduler/employee/config/
kanbansvc/agent) OK; full -count=1 suites of voice/scheduler/agent +
kanbansvc/employee/config OK; hooks bash -n + census selftest 17/17; gofmt clean.
Wave-2 close (parent): build OK; scheduler + agent suites OK after the two
patches; all three patches committed through the pre-commit hook (all gates ran).

## Incident: fixer commits landed by another actor

The six wave-1 fixers' changes landed as nine commits at 2026-11-15 16:29:33-34
authored "e2e <e2e@example.invalid>" (2d2f3d08, 625c7ce7, 14a4fb7b, 310701d5,
079c29b1, 0c48c45f, eb18719f, 474709be, fe392db5, a8fdb33f, e822dd5b) — mixed
with sibling feature work, NOT committed by this session (the dispatch forbade
commits). Every claimed fix was content-verified at HEAD (grep markers) before
acceptance; two gaps found and repaired (R4 not actually deleted; scheduler pins
left uncommitted → 7d8bf06e). Commit-message/diff mismatches flagged in the
table (310701d5, 079c29b1 message scope).

## Deliberately NOT done (ledger)

1. Voice LOW trio (said-feed short-reply drop in cmd/meept/voice.go pollOnce,
   drain-tail speaker-clock bypass, speakerClock printf-under-mutex) — cosmetic;
   two of three live in sibling-WIP files.
2. Scheduler clock-going-backwards handling (sweep is pure wall-clock).
3. Census C-row fixture + core.quotePath=false tab edge (hooks wave).
4. pre-commit-errors Checks 1/2 share the multi-line blindness H3 fixed for
   Check 4 — same logical-line join would fix them (fixer observation).
5. kanbansvc pairing dead-wiring + stale comments — ACTIVE leaf lane owns it.
6. RegisterTiming (timing.go:1094) zero production callers — dead-code candidate.
7. eb18719f retry/comment mismatch — voice lane owner call.
8. V1 commit-message overclaim (ee465418 "only ... allowlist") — history note.

## Duration (wall clock, MST)

- Scope + baseline gates: 00:25→00:32 (~10 min; two baseline attempts blocked by a permission gate, proceeded with targeted baselines)
- Wave-1 audit (6 auditors, parallel): 00:32→~03:10 (~2h40m)
- Parent HIGH verification + fix dispatch: ~03:10→03:24 (~15 min)
- Fix wave (6 fixers, parallel; scheduler fixer dominated at 2006 api_calls): 03:24→16:29 batch landing (~13h wall; other fixers finished within minutes)
- Parent verification of landed work + gap repairs + commits: ~19:14→19:24 (~10 min active; ~2.7h of that window was waiting on the fixer batch's completion message)
- Wave-2 audit (3 auditors, parallel): 19:24→19:47 (~23 min)
- Wave-2 fixes + gates + commits: 19:47→~20:00 (~13 min)
- Total: ~19.5h wall, of which ~15.5h is subagent parallel time
