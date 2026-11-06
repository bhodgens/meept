# Next Steps

State as of 2026-11-06. Main is stable and green; the active campaign is the
parked **dsh-convergence** tree (`docs/plans/20261102-dsh-convergence/`,
branch `plan/dsh-convergence`, live worktree `.hermes/worktrees/dsh-conv2`).
The branch carries 15 commits ahead of main: session-storage branch 03 is
COMPLETE (S1 format versioning, S2 projection registry, S3 cross-session
bounded reference, S4 storage record-change events - all gates green), branch
06 office skills is mostly landed (`office_docx` over godocx v0.1.5,
`office_workbook` over excelize v2.11.0, plus skills and library picks; pptx,
docs leaf, and C11.5 tool registration remain), and branch 04 has two leaves
in (per-tool `TimeoutMs` contract B1, TUI message-quotes). All six branch
test gates are parked RED as `.staged` files (20 files on the branch ref),
per the tests-first prerequisite (user decision D7). Branches 01-delegation,
02-goals-plans, and 05-schedule implementation have not started. The worktree
at `dsh-conv2` is NOT clean: it holds in-flight uncommitted work that
un-parks and implements 04 (skill hot-reload watcher, `/name`
skill_invocation, messagequote/executor gate tests activated) and 05
(RFC5545 `timing.go`, delivery `receipts.go`, their tests), plus a
started `office_pptx.go`. GitHub issues 11/12 are filed (#111, #112).

## Decision brief: dsh-convergence (2026-11-06)

### Recommendation: CONTINUE

The branch iteration is not superseded anywhere and the hardest,
highest-value branch (03 session-storage) is already complete and green;
merging the partial 15 commits would strand half-activated contracts, and
archiving would throw away ~10.1k added lines of reviewed, gate-tested work.

Evidence:

1. **03 COMPLETE + gates GREEN** - S1-S4 landed on the branch ref with
   activated e2e/unit gates (`internal/storage`, session schema versioning,
   projection registry, sessionref), suite in e2e/manifest.json.
2. **Main supersedes nothing** - `git diff main plan/dsh-convergence` on the
   branch's headline files shows branch-side insertions only: main has no
   `office_docx`/`office_workbook`, no `internal/storage/`, no sessionref,
   no `SessionFormatVersion`, no `TimeoutMs`. ARCHIVE is ruled out.
3. **In-flight work is mid-activation, not abandoned** - the dsh-conv2
   worktree is dirty with 04/05 implementations that un-park parked gates
   (scheduler timing+receipts, skills watcher, skill_invocation, office
   pptx); merging the branch now (MERGE-NOW) would ship 15 commits whose
   gate tests are still RED `.staged` files and whose 01/02 spine is absent.

Options ranked:

| Option | Assessment | Effort |
|--------|-----------|--------|
| **CONTINUE** (recommended) | Finish in-flight 04/05 in the worktree, then 01-delegation (the spine), 02-goals (depends on 01), finish 06 (pptx + docs + C11.5 registration), un-park remaining gates, run Phase 3 integration review. Contracts C1-C11 are already spec'd; gates exist parked. | ~3-5 focused sessions: 04/05 mostly done in-flight, 06 partial; 01 is the largest remaining branch (6 leaves), 02 depends on it (4 leaves) |
| MERGE-NOW | The 15 commits are individually coherent and the tree builds, but all six branch gates are RED `.staged`, tool registration (C11.5) is deferred, and 01/02 (the delegation spine the other branches consume) is absent. Would require gate activation + stub policies on main. | ~1-2 sessions to make merge coherent, but leaves the tree mid-contract |
| KEEP-PARKED | Safe but wasteful: the worktree's uncommitted 04/05 work decays and diverges from a moving main. | 0 now, growing rebase cost later |
| ARCHIVE | Ruled out: main has superseded none of it (see evidence 2). | n/a |

If CONTINUE: resume in `.hermes/worktrees/dsh-conv2` (do not reset its dirty
state - it IS the 04/05 implementation), keep D7 tests-first, and run the
master Phase 3 integration pass (`go build ./...`, `make test`, `make
e2e-fast`, `make graphs` + `make graphs-check`, `scripts/config-drift-check.py`)
before any merge to main.

## Resolved since last write

- Token-efficiency Q4 (skill ranking at scale): RESOLVED as option 3 -
  "always rank". Embedding-rank ordering of the no-cap skill listing landed
  in 6a4aca81 (`feat(skills): token-efficiency 04/02`). Branch 04 of that
  plan is COMPLETE; the token-efficiency master plan is closed.
- 20261031 forest reconciliation landed and pushed (6fca9297..b3c03f80):
  post-crash reconciliation commits promoted hitl-notifications,
  llm-serving-tiers (incl. leaf 04 TTFT at 0827f72c), jev-command-gate
  closeout, wiring-followups-final, and agent-activity-audit-views
  (7/7 leaves) to COMPLETE with cited SHAs.

## Constraints to keep

- GOTOOLCHAIN=go1.26.5 for build/test.
- Never `git add -A`. Stage exact paths; commit `--no-verify` only when the
  failure is a known unrelated gate (e.g. the racy shared-index or an
  in-flight sibling file), never to skip a real failure.
- `go test -p 2` (port-exhaustion invariant on this host).
- macOS full sweeps must bound package parallelism (-p 2) - see AGENTS.md.
