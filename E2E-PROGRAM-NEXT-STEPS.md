# NEXT-STEPS — e2e program + CI repair (2026-09-26)

Status: e2e program COMPLETE; CI fully green on both workflows. Two
follow-ups open (below), both documented with evidence.

## What exists now (all committed to upstream/main)

### Hermetic e2e tier
- `e2e/harness/` — scratch-daemon manager (sandboxed MEEPT_HOME, free
  ports, ~20s boot), scripted fake OpenAI-compatible LLM (planner/
  classifier/tool-call routing, SSE), sqlite task-store readers, CLI/RPC/
  HTTP/WS/SSE drivers.
- `e2e/suites/<area>/` — 44 suites / ~145 scenarios per
  `e2e/manifest.json`. Covers transport (RPC/WS/HTTP), agent loop
  (dispatcher/planner/async-turn/task-state/output-filters/refusal),
  tools (filesystem/memory/tasks/docs/arg-validation/SSRF/MCP), skills,
  state (restart-survival, audit chain, effects ledger, quota
  park/resume, daemon lifecycle, TLS/fence, ACP, goal loops).
- Run: `make e2e-fast` (all), `make e2e-fast-area AREA=<dir>`,
  `make e2e-affected` (branch-diff scoped).

### Enforcement
- `.githooks/pre-commit-e2e` — check [18/18]: runs affected suites on
  staged internal/pkg/cmd Go changes; blocks NEW package dirs without an
  e2e suite + manifest entry. `MEEPT_SKIP_E2E=1` is the loud bypass.
- `make e2e-affected` / `scripts/e2e-affected.sh` — path→suite mapping
  via manifest path_map.
- CI: `e2e-fast` job in code-quality.yml (first green run 2026-09-26).
- Policy (AGENTS.md + docs/workflows/e2e-testing.md): new feature tests
  go in the e2e tier; no new unit-test files for features.

### CI repair (was 0/8 green, now 8/8)
Root causes fixed, each verified against real CI logs:
- go-version pinned to go.mod toolchain 1.26 in both workflows
  (setup-go cache tar raced the 1.26.5 toolchain download — 11k
  "Cannot open: File exists", vet missing, 18 pkgs failing silently).
- libasound2-dev installed in every compiling job (cgo oto audio).
- gosec scoped to the repo policy G201,G202 (unfiltered scan has ~1.1k
  pre-existing G115/G123 findings).
- golangci-lint built from source (prebuilt binaries built with go1.24
  refuse go1.26.5 modules) and gated on the branch diff
  (--new-from-rev merge-base; branch diff is 0 findings).
- Linux-only test fixes at root cause: git fixture branch pinned to main
  (cluster + config-syncer bare repos), Chrome --no-sandbox headless +
  launch retry (zygote abort / 20s ws deadline), exec pipe-wait bounded
  (oracle + gate: grandchild `sleep` held the pipe), IPv6 ::1 in ssrf
  allowlists and http-hook tests (httptest binds ::1 on Linux), overlayfs
  ETXTBSY retry on mock-script exec, pty partial-read accumulation,
  coarse-clock episodic boundary skip, evolver actuator wait 2s→10s.

## Open item 1 — evolver bridge CI skip (evolver lane owns)

`TestApprovalWiring_EvolverPlanApprovalTriggersActuator` skips on CI:
the plan.approved bridge pump never observes the event on the 2-core
runner — zero audit lines, no wiring log, deterministic across 5 runs;
passes locally every time (even GOMAXPROCS=1, count=5, full package).
The skip dumps bridge/skillEvolver/msgBus wiring state on every CI run
for the lane owner. All in `internal/daemon/evolver_approval_wiring_test.go`.

## Open item 2 — RESOLVED: alias fallbacks confirmed intentional

Operator confirmed (2026-09-28): fallback follows the same implementation
for every alias — resolver walks the alias's model list in order, first
success wins, failures record backoff and rotate to the next model. No
per-alias special-casing exists or is wanted. Remote members (zai, ollama)
in coder/planner/analyst are intentional production fallbacks. The e2e
sandbox's remote-member filtering and the suites' pinned contract remain
correct as-is. No template changes needed.

## Known product findings (FIXED 2026-09-27, see commits 5edcd799..7d8acd13)

All six findings below are fixed and their pinned suite skips flipped
green. Roster grants landed in the DEFAULT config (coder: file_edit,
tool_view; analyst: pdf_read, spreadsheet_write; skeptic: remember;
librarian: memory_vote) — each maps to a known BuiltinRules action, so
the existing permission layer gates them; no operator action required.

- No roster grants for file_edit/spreadsheet_write/pdf_read/memory_vote/
  remember/curation tools → default roster grants added (see above);
  curation family was already granted to librarian.
- memory_vote lacked a ToolActionMap entry → mapped to memory_write.
- Skill.RequiresTools never populated; WithToolAvailability never wired →
  parser reads `requires-tools:` and the executor consults the live tool
  registry.
- ChatHandler.notificationPublisher never wired → notificationAdapter
  wired at both ChatHandler construction sites.
- Dispatch step lane did not inject the session working dir → root cause
  was chat.submit minting an orphan conversation id; submit now resolves
  the session's own conversation id, and resolveStepWorkingDirFor also
  consults the step payload's session provenance.
- GitAddCommitPush passed absolute paths to go-git Worktree.Add →
  relToRepo converts to repo-relative; local-only backups (no origin)
  no longer fail the cycle on push.

## Suggested order

1. Evolver lane: root-cause the bridge pump CI silence (skip dumps state).
2. Operator: confirm alias member-list intent (open item 2).
3. Debt paydown: ~1100 golangci findings repo-wide (gated on new code
   meanwhile); gosec G115/G123 classes; the product findings above.
4. ci.yml gosec already scoped to G201/G202 — matches Makefile policy.
5. Formalize `scripts/e2e-naive-user-chat.sh` as a manifest suite.

## What remains (post-CI-repair, 2026-09-26 late)

CI is green on both workflows as of `a4036479` (CI 8/8, Code Quality
8/8). What follows is the outstanding list at that point:

- One documented CI-only skip: `TestApprovalWiring_EvolverPlanApproval
  TriggersActuator` (open item 1 above; diagnostics dump on every run).
- Open item 2 above: operator confirmation of models.json5 alias
  member-list intent.
- Tracked lint debt: ~1100 pre-existing golangci findings repo-wide
  (CI gates on new code via --new-from-rev merge-base) plus the gosec
  G115/G123 classes outside the G201/G202 gate.
- ci.yml gosec already scoped to G201/G202 — matches Makefile policy;
  no action pending there.
- naive-user-chat: formalized (open item 3 resolved — hermetic twin
  green; live tier = `make e2e-chat`).

## Skipped-test decisions — ALL RESOLVED (2026-09-29)

Operator decisions on the three remaining e2e skips (fixes dispatched
to subagents 2026-09-29):

1. breakers-04: the repeat-error breaker threshold becomes a real config
   knob (agent.repeat_error_limit, default 3, <=0 falls back to default).
   Production behavior unchanged; the e2e suite raises it above the tool
   breaker's 5-strike veto so the veto path is testable.
2. tools-filesystem-05: list_directory's path argument becomes optional.
   Omitted path resolves to the session working dir; the no-working-dir
   sentinel fires only when none exists.
3. web-search: fake SearchProvider wired in the e2e harness (canned,
   deterministic, zero network) via the existing SetSearchProvider seam.

All three suites lose their t.Skip; after landing, the tier runs with
zero skips.

## Recommended next steps (ranked)

1. ~~Evolver lane — bridge pump CI silence.~~ RESOLVED 2026-09-28
   (9f503122): the failure was plan.md rewrite races (non-atomic writes,
   shared tmp scratch, lost applied-marker update), not runner slowness.
   Skip removed; deterministic at -count=100 under contention.
2. ~~Operator decision — alias member lists.~~ RESOLVED: fallbacks
   confirmed intentional (open item 2); agnes-2.5-flash is now the coder/
   planner/analyst primary (8e2fa2e5).
3. ~~Formalize naive-user-chat~~ DONE — hermetic twin in
   e2e/suites/naive-user-chat (manifest-registered, green) + live tier
   via `make e2e-chat`.
4. ~~Six product findings~~ RESOLVED 2026-09-28 (7d8acd13, 5edcd799,
   95cc425f, 525b268c, b0342a5f, 043b7c3f); every pinned skip flipped
   green.
5. ~~A5 continuity~~ RESOLVED — PASSED in run 45 (2026-09-26T21:53,
   17/17) after five root-cause fixes; see open item 3 history.
6. **Lint debt paydown.** ~1100 golangci findings + gosec G115/G123,
   package by package; the CI gate already blocks regressions.
7. **TUI e2e phases 1-3** per docs/workflows/tui-e2e-plan.md
   (3305f6c9): parity inventory + goldens first.
8. **Live evolver operator exercise** — issue #60.

## Open item 3 — RESOLVED: naive-user-chat is formalized

Both forms now exist (verified 2026-09-26 late):
- Hermetic twin: `e2e/suites/naive-user-chat/naive_user_chat_test.go`
  (fake-LLM, manifest-registered, `make e2e-fast-area
  AREA=naive-user-chat` green, 2 tests). The scenario script header
  documents the both-places sync rule.
- Live tier: `make e2e-chat` runs `scripts/e2e-naive-user-chat.sh`
  (real provider, local-only) — the recommended `e2e-chat-naive` target
  already exists under this name.
- Session history (2026-09-26, runs 31-45): the harness surfaced and
  drove fixes for A5 continuity (thread-router conversation-id mismatch,
  platform-shortcut ordering, intent-label instability, digest
  envelope-header summaries, interrogative gating, and follow-up
  questions bypassing task creation). 15+ commits through `9c6f46f4`.
  **A5 PASSED in run 45 (2026-09-26T21:53): 17/17 green — continuity
  answered from the stored task result with the full artifact path.**
  Closed.

