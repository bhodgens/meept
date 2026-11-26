# Fix wave 2026-11-26 — 2026-11-25 bughunt + 2026-11-24 carryovers

Repo: /Users/caimlas/git/meept, branch main (all work landed directly on main;
no feature branch to merge). Commits: 19. Final gates: see bottom.

## Findings -> commits (this wave)

| Commit | Findings | Concern |
|---|---|---|
| 7b6011d7 | H1/A1-1, H2/A1-2, A1-11 | Ring.Tail newest-n; per-query plan interest; HasMethod probe |
| 25380735 | H3/A1-3 | webhook rate-limit wrap (slack/voice/SMS) |
| c984b1e6 | H4/A1-4 | gate-capture out-param threading + bounded askArmed |
| dd207b8b | A1-12 | debug.Stack() in three recovers |
| 31f73f0a | A1-7 | OTLP sampling rate + step-span TraceID |
| 5a56efb6 | A1-8 | trace-sink circuit re-arm |
| 6619a0a2 | A3-8, A3-9 | engine unknown-action deny; override-error deny |
| 011ae339 | A3-10 | pii SwapStore lock + expiry sweep |
| 6212e3b9 | A3-11 | json5 duration exemption tripwire |
| 10af9146 | H6/A2-1 (D1), A2-6, A3-4, D1-feature | union fs_roots fence at 3 sites; symlink-safe checks; groups.assign fs_roots amend |
| e73b0a0f | H5/A1-5 (D2) | effects key session+agent |
| 888f8f2a | H8/A6-1, A6-4, A6-5, A6-13, A6-10 | goal authority per-ctx; driver inflight/transient/leak; gate resume nested decode |
| f509d198 | H7/A3-1 (D3), A3-2, A3-5, A3-6, A3-12 | compress row-IDs; demoted skip; flush drain wired (tests); scope fail-closed; lease stamps |
| 6beb4499 | H9/A6-7, A2-2, A2-5, A6-2, A6-9, A6-14, F8 | central file_path fence key; executor races; office symlink+atomic; SSE partial drop |
| 038d04dd | A4-1..A4-5, A7-1..A7-5 (D4) | voice teardown off C1; writeMu; monotonic barge clock; resampler tail; never-drop split; outage visibility |
| ba2a3978 | M1/A6-3, A6-6, A6-11, A5-1, A5-2, A5-3, A5-4 | placeholder prompt guard; exit_plan_mode ctx responder (incl. loop stamp, parent edit approved 2026-11-26); classifyDecision words; outcome timeout; goal park release; route gate; dedupe window |
| 4b038f7d | A7-6, A7-8, A7-9, A7-10, A5-7 | eval escape-rate; golden-regen wired; empty-golden fail-closed; chokepoint pending; ClaimNextByID predicates |

Siblings landed these in parallel (same campaign, verified at HEAD, not
double-fixed): 25684380 (bus.poll since_seq pin), 8b90b1cb + 975d3eae
(quickplan e2e + docs), plus the earlier M/H fix batch.

## Disposition vs the 2026-11-25 report

- HIGH 1-9: all closed EXCEPT the chat-lane half of H6 (blocked, below).
- MEDIUM M1-M35: fixed as tabled; report-only remainder: M10 (A1-10 groups
  boot-snapshot enforcement — documented caveat in agent-groups.md, hot-apply
  deliberately not built per ruling), M21/A5-1 fully fixed, M22/M23/M24
  fixed in ba2a3978.
- LOW: fixed incidentally: A3-11, A7-8, A5-7, A6-11. Not touched: A1-13..18,
  A2-7..11, A3-7, A4-6/7, A6-8/12, A7-7, A7-11/12, 11-25 L-items (report-only
  tier), P4.

## Blocked / held (files carrying sibling uncommitted work at commit time)

1. H6 chat lane: handler.go wireSessionLoop needs the composeLoopFence swap
   (patch in sa-2 report; task-2 blocked[]). Same file holds the H9 (11-24)
   thread-id pairing half (sa-7 blocked[]).
2. ~~A2-3/A2-4: loop.go turnCounter race + triggerTitleRefresh wg~~ —
   LANDED as 8e028f36 (loop.go freed by ba2a3978; pins + non-vacuity in
   turn_counter_race_test.go).
3. A5-5: loop_park.go turn-scoped HasPendingForTurn (patch in sa-8) — still
   held, loop_park.go sibling-dirty.
4. H7 lane (11-24), M1-M4 llm items, A3-7 semantic cache: internal/llm/* —
   sibling's uncommitted transient_retries revert still in the tree.
5. tiers compress flush production call-site: components.go one-liner
   (BuildTierPolicyEngine optional working.Store arg; patch in sa-3).
6. A6-12 contact ctx threading: contact_wiring.go (patch in sa-7).

## Behavior users will see after this wave

- activity.recent shows NEW events (was frozen at oldest 50 after 1000).
- Webhook floods now 429; unauthenticated slack/voice/SMS endpoints throttled.
- Workspace-preset shell ASK commands record + resolve permission_table_ask.
- A second session re-running the same tool call no longer false-flags the
  effects ledger or double-executes in flight.
- Managed agent groups: fs_roots actually bounds writes (additive), and
  groups.assign can amend fs_roots.
- memory.tiers compress distills the selected rows instead of silently
  deleting them; evicted working-ring turns now drain to the episodic sink.
- A cron/webhook round can no longer borrow another employee's goal
  authority; goal create/edit/pause/resume need a real human turn (or the
  driver's own round ctx).
- file_path-keyed tools (lsp_*/ast_*/lsp_navigate) are fenced; git_* working
  dirs are fenced; office tools resolve symlinks and write atomically.
- Voice calls survive end-frame teardown without blocking other calls; long
  supervised replies arrive whole; provider outages log per-dial.
- TUI never binds a project literally named "(no project detected)".

## Final gates

Full sweep at HEAD 4b038f7d (`go build ./...` exit 0; `go test -p 2
-count=1 ./...`): 133 ok, 1 FAIL package internal/metrics, 1 FAIL package
internal/llm [build failed].
- internal/llm: the sibling's uncommitted transient_retries revert vs the
  committed test file (unchanged attribution since 2026-11-24; committed
  tree is green).
- internal/metrics: TestVR_BREAKDOWN_PLAN_1_WindowedBreakdownUsesIndex —
  passes isolated AND package-wide with -p 1; fails only under the -p 2
  sweep (window-boundary timing flake, pre-existing, same one the fixer
  proved at 01:00).
All 19 commits passed the 22-gate pre-commit chain; no --no-verify used.

## Ledger / disclosures

- Repair edits by the parent during commit chain: goal_tools no-op param,
  two unused helpers + two mutexio nolints in voice pins, socket.go deadline
  error check, responder stamp in loop/speak/turn_conversation (user
  approved "merge to main" scope; the earlier scripted form was denied and
  NOT retried — done via patch tool instead).
- A first attempt at the activity commit hit a sibling index race; reflog
  verified no sweep, files were restored and re-committed cleanly.
- One execute_code cell timed out mid-batch; all four affected commits were
  individually verified landed (6619a0a2, 5a56efb6, 31f73f0a, dd207b8b,
  c984b1e6).
- internal/voicechild/pipeline.go: fixer reported NO edit needed; file remains
  sibling-dirty; untouched.
- e2e manifest entries for new e2e pins (golden_scenarios registry, jev
  chokepoint pin) live in sibling-dirty e2e/harness/daemon.go and
  e2e/manifest.json — NOT committed; harness files golden_regen.go,
  golden_scenarios.go, capture_test.go, trajectory_golden_test.go WERE
  committed with 4b038f7d. If manifest wiring is missing, the pre-push e2e
  gate is the catcher.

## Addendum — tree-resolution phase (2026-11-26 afternoon)

The "lot of uncommitted work" was a bulk partial revert of PR #171.
Triage (4 read-only subagents) + parent execution resolved it:
- RESTORED to HEAD (collateral, contradicts committed fixes): handler.go
  T4c removal, loop_park const, topic_detector tie-break, envelope_prose
  TrimSpace, chat_nav/chat hydration, voicechild SetWriteBuffer, skills
  watcher, kanban drain, e2e suite reverts. Restoring re-activates M12
  (badge misjoin) as an OPEN finding — targeted fix, not feature deletion.
- COMMITTED (intentional/forward), split surgically from the mixed diff:
  e85696d0 transient_retries lane revert (operator ruling; fixes the
  long-red internal/llm test build; moots 11-24 H7/M1-M4);
  fed4a8fe A1-4 production half + SecurityNotify trio + MCP pin cluster
  (#166/#167; doctor mcp-pin-updates lane added by parent when the
  deadcode gate caught CheckUpdates unwired); 215e578b steer-arming docs;
  6a72b597 audit reports; 7222a70c H6 chat-lane + injectSessionFence
  removal + the _arm filename build-constraint discovery; 482576ec A5-5;
  c3662088 11-24 H9; 60d0a1c6 A6-12; 19ec8d27 A3-5 + nil-bus stop guard;
  7433ee5a A3-7; b4c40dcd + 676f8d60 e2e fixture honesty updates (both
  pinned the pre-fix double-selection / race shapes).
- PUSH: rejected twice (upstream divergence + pre-push e2e gate); resolved
  by rebase on upstream 0a2762f8 (595ceb17 supersedes c5d54c9e's proxy.go
  legs — took upstream, kept the chat_async seq-cursor leg), gate failures
  fixed, third push in flight.
- Held list: now EMPTY. All 12 patches landed. New issue: bhodgens/meept#182
  (agents.create never registers a GoalLoop at runtime — the goals-plans rig
  root cause; product fix pending). Disk note: go-build cache filled the
  volume mid-phase (100%); go clean -cache; df before heavy waves.
