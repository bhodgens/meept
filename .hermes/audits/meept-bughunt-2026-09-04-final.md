# Bughunt wave: 2026-09-03/09-04 commits + agent loop (double pass) — FINAL

Session: 2026-09-04 evening → 2026-09-05. Request: "bughunt-audit-and-fix
this repo, fixing issues as found. focus on commits made in the past 2
days, and the agent loop. do it twice."

Wave scope: ~60 commits (2026-09-03 → 09-04) plus the then-uncommitted
sibling diff (session-continuity leaf 01, now 2a433d8e). Round 1: 5
read-only auditors. Round 2: 5 fresh auditors (adversarial re-review of
round-1 fix commits + new scopes). A sibling session was ACTIVE on the
same tree throughout (session-continuity, classifier-observability,
grok-bot blueprint trees); contested-zone rules held throughout: disjoint
files only, git log re-checked before every patch, re-verification of
each stale-prone auditor claim against HEAD.

## Round 1 — 19 commits

Parent pre-read fixes (before auditor reports):
- 32376cdf HIGH: HTTP hook retries reused one http.Request — retries
  after a 5xx sent Content-Length=N with a drained body (transport
  kills every body-carrying retry). Request rebuilt per attempt.
- c044e858 HIGH: codex streaming 429s never read the error body →
  quota windows degraded to RateLimitError and got short-retried.
  Error path drains on both modes (anthropic.go parity).
- 812fc4c3 HIGH: daemon TEST package unbuildable — fda25177 referenced
  test hooks in a file that was never committed.
- 0cd5ced5 MED: provider-merge overlay models deep-copy ExtraHeaders
  (aliasing hole in 515dba45's own contract).
- 47bf9725, 256b6fd7: lint (errors.AsType, ineffassign, atomic.Int32).

Auditor-report fixes:
- 45a6b154 HIGH: bedrock event-stream header parser violated the AWS
  spec — string lengths read as 1 byte instead of 2-byte BE uint16,
  desyncing EVERY real frame (silently dropped events, empty Response,
  no error); 0x04 consumed 2 bytes not 4; 0x06 was float but spec says
  byte_array; 0x08 timestamp/0x09 uuid missing. Parser + the test
  fixture that encoded the same wrong shape fixed; spec-shape test
  added.
- 777734a1 HIGH: json5 tokenizer PANICKED (slice out of range) on a
  config truncated mid-string with a trailing backslash — probe-verified
  before fixing; both string scanners stop at EOI on a dangling escape.
- 36a3b33d HIGH: duration tokenizer corrupted duration-STRING config
  fields (oauth.refresh_interval, refresh_margin,
  agent.worker_pool.idle_timeout documented as strings parsed with
  time.ParseDuration) — real config shape bricked config load. All
  registered in stringDurationKeys + warning comment.
- 80f1d8b6 MED→HIGH: RecordAliasFailure refused QuotaResetError —
  contract 4 enforced AT THE SOURCE. Quota unwraps to a 429 APIError →
  FailureThrottle verdict → quota advanced alias cooldown, released
  pins, AND armed endpoint blocks via the analyzer/classifier call
  sites. Invariant is now unconditionally safe.
- 16084e9d MED (2 auditors cross-confirmed): shouldRetryHookError's
  unconditional `return true` made the documented non-retryable bail
  dead — retry_count=-1 hammered 401/403/404 forever. Error responses
  now carried as *HTTPError; non-retryable statuses stop the loop.
- 3a731834 MED: Consolidator.Run nil-manager guard completes D-M4
  (421959a9 guarded consolidateEpisodic but Run dereferenced first; its
  own test dodged the site).
- 1a8759ec MED: codex 402 takes the quota lane (client.go:1332 parity;
  was plain APIError → no rotation, quota branch never fired).
- 5f08daef LOW: consolidation error-truncation notice was dead code
  (off-by-one); second site missing its increment.
- 060eaad9 LOW: vector shard DB leaked when createIndex failed
  (shadowed err skipped the deferred close).
- af8a2014 LOW: getCurrentVersion logs DB errors (was silent 0 →
  version numbering restarts).
- 64239c5b docs: pacing Enabled comment tracks fda25177 default-ON.
- 0415a74b LOW→fix: SigV4 now signs extra headers actually present on
  the request (comments claimed it; code didn't; strict AWS endpoints
  can reject). Reserved/transport headers excluded; spec-vector tests
  unchanged, e2e signed-invoke test green.
- a48cfbf5 MED: sticky-caller resolution honors endpoint blocks (D10
  shared fate) — sticky aliases previously bypassed endpoint cooldowns
  entirely, undermining fda25177's timeout-park premise.
- e2c68f3a LOW: RecordDispatch no longer attributes dispatches to a
  stale classifier (last-writer-wins fallback contaminated ByMethod).
- ed7f564c MED: reasonWatchStreakBreach reads/writes take l.mu
  (421959a9 guarded only the reset).
- 01f0ef3a test: SkipsInitialCycle pins HOME (same env-dependence
  5edcead7 fixed in its twin).

## Round 2 — fixes

- f94fd072 HIGH×2 (auditor 2): guard nudges severed tool pairing
  (assistant(tool_calls)→user(nudge)→tool(result) → HTTP 400 on strict
  providers exactly when the ladder fired) — nudges deferred until
  after the tool-result loop; cycle-detector abort and veto termination
  left dangling tool_calls poisoning the NEXT turn — both paths flush
  synthetic "[aborted]" tool results (flushDeferredToolResults).
- 916d32d5 MED (auditor 3): ResolveForAlias sequential quota→endpoint
  passes could hand the cursor back and forth and return a quota-
  blocked model; winner now re-validated against both predicates.
- 6f0a20ba LOW (auditor 3): armed-block multiplier shift capped — at
  TimeoutBlocks==64 the 1<<63 wrap produced a negative duration that
  permanently disabled alias timeout blocks.
- 697a3173 MAJOR-latent (auditor 5): stopword-valued skill names
  ("bench", "agent") emitted bare weight-1.0 keywords; full-name
  emission now stopword-gated + regression test.
- b7c6508d fix(hooks): pre-commit-build's downstream scan used
  `go list -deps=none` (invalid since the -deps flag became boolean) —
  silently fell back to whole-tree build, making every commit fail on
  any untracked scratch main. Verified + fixed.
- 3e82d345 docs: failure-policy test header tracks pacing default-ON.

## Verified clean (highlights)

- Pacer ticket reservation, slot_gate two-lane priority (3:1 starvation
  guard traced), attempt-tagged delta rotation, json5 tokenizer probes
  (CRLF/tab/µs/escapes all correct — only committed-test coverage
  gaps), retry_count *int wiring, ftstore INTEGER migration legs,
  park/resume (nil,nil)-sentinel call sites, reply guard, M9 timezone
  convention + dual-key wire, bus demotion (deliberate), GetMessages
  ordering (int64 PK, no string-sort bug), SaveMessages transaction
  atomicity, persistExchange best-effort contract, iteration budget
  (INFO-9 clean), docs-vs-code: all 16 verifiable claims matched.
- Full-suite -race: zero DATA RACEs across all packages (10-package
  targeted run + full suite).

## Deferred — sibling session owns the files (ledger)

- Restore-key duality (R2 auditor 4 finding 1 + /reset key miss +
  thread-routing restore): the session-continuity tree's master.md
  documents the A5 blocker and plans a session-aware follow-up leaf.
  Cross-cutting fix (restore via GetByConversationID or persist under
  the loop key) is THEIR design call; not patched under them.
- D1/D2/D3 (raw-vs-sanitized mirror text, steer/follow-up queue-string
  reply, LastUserMessage guard coupling) — leaf-01 owner's.
- R2-1: restored conversations lose the validation anchor (leaf-02
  owner; behavior change beyond stated scope).
- R2 auditor 2 MEDIUM-3/4 (truncation pairing awareness, per-turn
  reset for byte-level detectors) + LOW-5..8 — ledgered, agent loop is
  contested; recommend as next-wave items.
- model_parser prose findings (auditor 1 findings 4-5) — sibling
  actively iterating there (b542a0f4 landed mid-wave).
- syncMode one-way global flip + data race (auditor 1 finding 7) —
  wiring design decision; needs owner call.
- Park-event unblock_at vs actual schedule (auditor 1 finding 9) —
  parked_turn.go sibling-owned.

## Observations (unacted, ledger)

- Hermes metadata comma-scalar tolerance gap (hermes_compat.go fields
  stay []string) — consistency follow-up.
- ACP TestSession_PermissionPermissive/Deny keep 5s dial timeouts —
  same flake class 61f04bd3 fixed.
- Five forked stopword lists (shadow/, ralph_loop, review_manager,
  skill_designer) predate the shared StopWordSet.
- Restore ignores BranchID; parked turns double-persist the user
  message in restored history (R2 auditor 4 findings 4/6).
- Stop()/Close() bare wg.Wait with no timeout can block shutdown on a
  hung reflection call (R2 auditor 2 INFO-10).
- Untracked sibling artifacts: cmd/probe-skillkey-tmp/,
  cmd/skillparse_main.go (breaks `go build ./...` while present —
  hook fallback masks it now that the hook is fixed), tmpdbg_test.go
  files, pacing_reservation_test.go (green, should be committed).

## Gates at close

- go build ./internal/... green (cmd/ blocked only by sibling's
  untracked scratch main).
- go test -count=1: llm, agent, daemon, memory, config, session,
  skills(+lifecycle), worker, bus — all ok.
- go test -race -count=1: same set — all ok, zero DATA RACEs, plus a
  full-suite sweep. FOLLOW-UP (2026-09-05): dedicated stress
  (-p 8 -count=6 over scheduler/queue/metrics) REPRODUCED and FIXED
  two real defects the first full-suite run had hinted at —
  metrics.Store.Close racing its never-joined background loops, and
  scheduler writeLastWake's shared temp filename tearing
  last_wake.json under concurrent writers. Fixed in a2231bc7; 13
  consecutive stress runs clean afterward.
- gofmt clean on every touched file; go vet clean on touched packages.
- mutexio + predid analyzers clean.

29 commits total this session (19 round-1 + 10 round-2), all disjoint
from the sibling's concurrent work. Nothing pushed.
