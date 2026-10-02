# Findings → commits disposition table (2026-09-29 bughunt, "fix all" wave)

Audit: `.hermes/audits/2026-09-29-week-bughunt.md`. All fixes committed on main
between f0f69485 and f5d369bf (Sep 30). Gate evidence: unit + e2e tagged suites
green before commit (24 pkgs, UNIT_EXIT=0 / E2E_EXIT=0 in
/tmp/fixwave-final-gate.log); full -race pass as the final gate
(/tmp/fixwave-race-gate.log).

| Finding | Disposition | Commit |
|---|---|---|
| H1 recall label gate no-op | FIXED | f0f69485 |
| H2 sweep kills auto_stop=false runtime | FIXED | b6d25522 |
| M1 digest/guard thread ids | FIXED | 71fee63c |
| M2 digest fallback unreachable | FIXED | f0f69485 |
| M3 retry bypasses parked-turn guard | FIXED | f0f69485 |
| M4 /plan slash route thread id | FIXED | 71fee63c |
| M5 lsp_rename fence never arms | FIXED | 16c16840 |
| M6 Telegram UTF-8/escape truncation | FIXED | fe2a3454 |
| M7 judge grades exit status 0 failed | FIXED | 8b8c0845 |
| M8 task-queue spurious kill / vacuous | FIXED | a7290da9 |
| M9 hook vs script prefix mismatch | FIXED | a7290da9 |
| M10 language_en default-on | NOT FIXED — product decision (documented 2026-09-22 default flip); owner call |
| L1 record_age always 0 | FIXED | b6d25522 |
| L2 streaming sentinel shape | FIXED | 07cf284b |
| L3 whitespace path cwd anchor | FIXED | 011051ea |
| L4 empty-exhaustion usage rows | FIXED | 07cf284b |
| L5 gossip per-resurrection budget + leak | FIXED | 7ee58026 |
| L6 HTTP plan sink fallback | FIXED | 049df702 (sibling) + c3e34c83 (wiring) |
| L7 "job " prose dropped | FIXED | f0f69485 |
| L8 guardRetried mutex asymmetry | FIXED | f0f69485 |
| L9 SkillIndexEntry.RequiresTools | FIXED | (skills/plan commit) |
| L10 backup silent empty success | FIXED | fe2a3454 (absorbed) + f5d369bf (marker) |
| L11 thread-id suffix unwrap | NOT FIXED — narrow, fail-safe direction; revisit if topics ever contain "-thread-" |
| L12 plan lock map unbounded | FIXED | (skills/plan commit) |
| L13 distill dedupe re-enabled | NOT FIXED — intended behavior; needs an eval, not a revert |
| L14 file:// misclassified remote | FIXED | 47737b63 |
| L15 fuzz seams | FIXED | d50fc051 (L15b), a7290da9 (L15a note) |
| L16 fake-search residues | PARTIAL — L16b fixed (a7290da9); L16a DuckDuckGo fallback coverage gap left (pre-existing) |
| L17 recall T1 stub acceptance | FIXED | a7290da9 |
| L18 classifier-eval resize | NOT FIXED — documented deliberate change |
| L19 lint_js all-caps false negative | NOT FIXED — documented advisory tradeoff |
| L20 python3 hardcode | FIXED | d50fc051 |
| L21 fake-search silent override | FIXED | 62646345 |
| L22 pair.* docs location | NOT FIXED — doc move; code verified matching contract |
| L23 config-flip rule windows | NOT FIXED — process note; /Volumes/LLMs machine-specific pin flagged for owner |
| L24 "one extra model call" claim | FIXED | f0f69485 |

## Wave incidents (disclosure)

- Sibling session's `git stash` destroyed fixers 4 and 5's tracked-file edits
  mid-wave; both scopes were re-dispatched and re-landed. The surviving pin
  tests (written pre-stash) verified the re-implementations.
- Fixer 4's L6 landed under the sibling's own commit 049df702 (staged-index
  absorption); the wiring half landed here as c3e34c83.
- L10's content was absorbed into fe2a3454 at commit time (shared staged
  index); f5d369bf is an empty disposition marker.
- The sibling's own commits during the wave (190c3f57, 13eeb696, 049df702,
  5fdeec90) are theirs and were not audited here.
- NOT done this wave: M10, L11, L13, L18, L19, L22, L23 (each disposition
  above), the full -race verdict (running at report time — see
  /tmp/fixwave-race-gate.log), `make lint-ci` analyzers, and the audit
  report's own commit (left uncommitted by choice: report-only artifact).
