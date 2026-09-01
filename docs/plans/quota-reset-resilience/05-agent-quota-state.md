# Agent quota states + episode tracker - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** New agent states `quota_wait` and `blocked`, the per-provider-
  key episode tracker that owns transitions, the 12h/20h/24h soft-stop
  escalation ladder, and the `agent.quota_wait` bus events.
- **Dependencies:** 03 (broker blocks / ActiveQuotaBlocks)
- **Estimated Context:** 70K
- **Concurrency Group:** C

## Goal

Make quota waits a first-class agent state. Users see "quota_wait, resets in
3h" in the agent list instead of a failure. Escalation is soft: three
notifications before the 24h boundary flips the agent to `blocked` (human
action needed). Everything transitions automatically when the quota lifts.

## Context

Agent state lives in `internal/agent` (loop/orchestrator + the state the
TUI/GUI read). Find the existing state enum/constants and where state
transitions are published to the bus with search_files — the tracker must
reuse the existing transition/broadcast machinery, not invent a parallel
one. States today include running/idle/paused-style values (verify actual
names). The daemon has one tracker instance (provider-key scoped), wired
where other components get wired.

Key files to understand before implementing:

- `internal/agent/` — agent state constants, state transition helpers,
  agent status struct read by TUI/GUI.
- `internal/bus/` — publish conventions (string topics, `map[string]any`
  payloads, two-value type asserts).
- `internal/llm/broker.go` — `ActiveQuotaBlocks()` from leaf 03 (read-only
  consumer).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/agent/<state file chosen after exploration>

// New states alongside existing constants:
//   AgentStateQuotaWait = "quota_wait"
//   AgentStateBlocked   = "blocked"

// QuotaEpisodeTracker (one per daemon):
type QuotaEpisodeTracker struct { /* deps: bus, state store, clock */ }

func NewQuotaEpisodeTracker(...) *QuotaEpisodeTracker

// Enter registers/extends an episode: agent transitions running ->
// quota_wait, records providerKey + unblockAt, starts escalation timers
// (12h, 20h after episode start; 24h boundary -> blocked + final event).
// Re-Enter with a later unblockAt extends the episode; escalation timers
// recompute WITHOUT re-firing already-fired tiers.
func (t *QuotaEpisodeTracker) Enter(agentID, providerKey, modelID string, unblockAt time.Time, taskID string)

// Clear ends the episode: quota_wait -> running, cancels timers,
// emits transition event with to="running", reason="quota_cleared".
func (t *QuotaEpisodeTracker) Clear(agentID, providerKey string)

// BlockedByEscalation is called by the 24h timer: quota_wait -> blocked
// (terminal until user action; Clear also works from blocked if the user
// fixes config and the provider recovers).
func (t *QuotaEpisodeTracker) BlockedByEscalation(agentID, providerKey string)

// Wire-up for tests: allow injecting clock + escalation thresholds
// (12h/20h/24h defaults; tests use seconds).

// Bus events (topic "agent.quota_wait"), payload keys all string-typed:
//   agent_id, task_id, from, to, reason ("quota_blocked"|"quota_cleared"|
//   "warn"|"action_recommended"|"escalation_24h"),
//   provider_id, credential_key, model_id,
//   unblock_at (RFC3339), escalation ("" | "warn" | "action_recommended" |
//   "blocked"), fallback_model (reserved; no producer sets it today —
//   see master.md remaining gaps)
//
// DEVIATION from the sketch below: the wire escalation vocabulary is the
// semantic ladder above, not "12h"/"20h"/"24h" strings. The 12h/20h/24h
// timings gate WHICH tier fires (see QuotaEpisodeTracker.tiers); only the
// wire strings differ. Tier events fire with to == "" (a refresh, not a
// transition); only the initial entry and the 24h tier set to.
```

### What This Leaf Consumes

```
// internal/agent state transition/publish machinery (existing)
// internal/bus Publisher interface (existing)
// leaf 03's QuotaBlockStatus shapes (types only, for doc alignment)
```

## Tasks

### Task 1: State constants + transitions

**Objective:** quota_wait and blocked exist and transition legally.

**Files:**
- Modify: the internal/agent file holding agent state constants
- Test: `internal/agent/quota_state_test.go`

**Step 1: Write failing test**

Constants exist with exact string values; transition helper (or tracker,
once built) accepts running->quota_wait, quota_wait->running,
quota_wait->blocked; illegal transitions (running->blocked directly,
blocked->running without Clear) are rejected or no-op'd per the existing
state machine's convention — VERIFY the existing convention and follow it.

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestQuotaAgentState -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Follow existing state patterns exactly.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestQuotaAgentState -v`
Expected: PASS

### Task 2: Episode tracker lifecycle

**Objective:** Enter/Clear/BlockedByEscalation with correct transitions.

**Files:**
- Create: `internal/agent/quota_episode.go`
- Test: same test file

**Step 1: Write failing test**

With injected clock (start at T0) and seconds-scale thresholds:
(a) Enter -> agent state quota_wait, one event with from/to/reason/unblock_at.
(b) Clear -> running, quota_cleared event, timers canceled (no further
    events after Clear even past thresholds).
(c) At +12h equivalent: escalation="12h" event, state unchanged.
(d) At +20h: escalation="20h" event. (e) At +24h: state -> blocked,
    escalation="24h" event. (f) Re-Enter extending unblockAt before 12h
    tier fires -> timers recompute; only later tiers fire. (g) Two agents
    on different provider keys are independent episodes. (h) Same
    provider-key episode is deduplicated (second Enter for same
    agent+key extends rather than duplicates).

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestQuotaEpisode -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Timers via time.Timer or a small ticker loop keyed by episode; guarded by
mutex; no I/O under lock (publish outside).

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestQuotaEpisode -v`
Expected: PASS

### Task 3: Bus event publication

**Objective:** agent.quota_wait events carry the full payload contract.

**Files:**
- Modify: `internal/agent/quota_episode.go`
- Test: same test file

**Step 1: Write failing test**

Capture published bus payloads (existing bus mock/fake — find one in
internal/agent or internal/bus tests): assert every key from the contract
is present and string-typed, unblock_at parses as RFC3339, escalation
values only from the allowed set.

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestQuotaEpisodeEvents -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Publish via existing bus publisher. Two-value asserts on payload construction.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run TestQuotaEpisodeEvents -v`
Expected: PASS

### Task 4: Tracker wiring + broker integration

**Objective:** The daemon constructs the tracker; broker quota blocks on an
agent's model drive Enter/Clear.

**Files:**
- Modify: the daemon wiring site that constructs agent components
  (search for where broker + agent loop are wired; add tracker construction
  and hand it to the component that sees LLM errors — the natural seam is
  where the agent loop currently surfaces LLM errors; leaf 06 refines the
  deferral, this leaf only needs Enter/Clear called correctly)
- Test: `internal/agent/quota_wiring_test.go`

**Step 1: Write failing test**

Integration-style with fakes: LLM error (QuotaResetError with horizon)
reaches the agent layer -> Enter called (state quota_wait). Later, block
cleared + next request succeeds -> Clear called (state running). Use fakes
for the broker; do not require real quota errors through HTTP here (leaf 01
covers parsing; leaf 06 covers dispatch integration).

**Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestQuotaWiring -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Minimal seam: an interface the agent loop already has (or a small notifier
func field) that the tracker subscribes to. Keep it narrow — leaf 06 owns
the dispatcher-side behavior.

**Step 4: Run test to verify pass**

Run: `go test ./internal/agent/ -run 'TestQuota' -count=1 -v`
Expected: PASS
Then: `go test -race ./internal/agent/ -run TestQuota -count=1`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] Escalation tiers fire exactly once per episode (no re-fire on extension)
- [ ] No I/O under mutex; two-value asserts on bus payloads

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (state values, payload keys)
- [ ] Timers cancel cleanly on Clear (no leaks)
- [ ] Extension (Re-Enter) recomputes tiers without refiring fired ones
- [ ] Race-clean

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Escalation thresholds as test-injectable fields with 12h/20h/24h defaults.
  Do not read them from config in this leaf (config has only the four
  contract fields; hardcode thresholds as package constants — the user
  approved the ladder as fixed).
- If the agent layer already has a status/notification helper, reuse it for
  bus publishing. Do not create a second bus client.
- leaf 07 (notifications) subscribes to the same events for push — your
  events are its input. Keep payloads stable.
