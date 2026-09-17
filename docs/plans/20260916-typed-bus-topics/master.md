# Typed Bus Topics - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 5 leaf documents under this node
- **Scope:** Give meept's message bus compile-time-checked topics and payload types (Tier 1 typed topics + Tier 2 WS-classification marker), per the 2026-09-16 design discussion.

## Goal

Meept's internal message bus (`internal/bus`) is stringly typed: `Publish(topic string, msg *models.BusMessage)` carries `json.RawMessage` payloads, and 136 publish sites / 41 subscribe sites bind to topics by shared string literal. The compiler cannot check that a publisher's payload matches what a subscriber decodes - the failure class behind the documented WS misclassification bugs (blank chat bubbles from misrouted `chat.*` lifecycle events, quota events nearly landing in the `chat_message` bucket).

This tree delivers two changes:

1. **Tier 1 - Generic typed topics.** A `Topic[T]` declaration type in `internal/bus` with typed `Publish`/`Subscribe` wrappers. Publisher and subscriber must reference the same `Topic[T]` value, so payload agreement is a compile error, not a runtime zero-value. Topic existence becomes checkable (unused `Topic` vars are staticcheck U1000 findings).
2. **Tier 2 - WS classification by marker method.** A `WSClassified` interface with `WSClass() WSClass` implemented by event payloads; `transformBusEventToWS` switches on payload type via the marker, and the `exhaustive` linter enforces complete coverage - replacing the AGENTS.md instruction that every new topic author "must verify they land in the correct bucket."

Scope discipline (binding decision from the design discussion, 2026-09-16):

- **Do NOT adopt typing as ideology.** Topics with genuinely open-ended payloads (caller-defined shapes, e.g. tool result forwarding) stay on the raw string path, explicitly labeled. Only topics with real, stable schemas get typed.
- **No big-bang migration.** The raw `Publish`/`Subscribe` API stays. We migrate the two scarred topics first (`turn.terminal`, `agent.quota_wait`), then the WS-visible chat lifecycle set, then stop. Remaining topics migrate opportunistically in future work, never in this tree.
- **Tier 3 (Dart-side codegen) is explicitly OUT of scope.** It is tracked as a separate GitHub issue (created 2026-09-16; see Notes for the issue number once filed).

Cleanup of AGENTS.md when the work completes is an explicit deliverable (leaf 05).

## Architecture

The bus today: `internal/bus/bus.go` - `MessageBus` with `map[string][]*Subscriber`, wildcard subscription support (`"agent.*"` matches `agent.status`; the WS relay subscribes via a wildcard set including bare `*`). `pkg/models/types.go:23` - `BusMessage{... Payload json.RawMessage}`. Payload structs (35 ad-hoc `*Payload struct` types) are marshaled at publish, unmarshaled at subscribe, with no compile-time link.

The design adds a generic layer on top, not a rewrite:

```
Topic[T] declaration (package-level var, one per topic)
   |                 |
   v                 v
bus.PublishT(t, payload)      bus.SubscribeT(b, id, t, func(T))
   | marshals T here             | unmarshals T here
   v                             v
raw Publish(topic, BusMessage{RawMessage})  -> raw Subscribe -> decode callback(T)
```

The raw path is untouched - typed wrappers marshal/unmarshal in exactly one place each. WS classification moves from string-prefix matching in `internal/comm/http/server.go:703-760` to a type switch over a `WSClassified` marker interface, with `exhaustive` linter enforcement.

### Pre-dispatch verification findings (2026-09-16, orchestrator-verified - binding amendments)

The authoring-time assumptions were checked against the code before dispatch. Reality:

1. **`turn.terminal` is single-shape and publisher-migrated only.** Payload is the frozen `agent.TurnTerminalEvent` (internal/agent/handler.go:233, "CLOSED" field set). Single publisher: handler.go:1584 (`publishTurnTerminal`). NO direct bus subscriber exists - consumers are (a) the WS relay via its `*` wildcard subscription and (b) the TUI via the RPC event stream (internal/tui/events.go topic filter), neither of which uses `bus.Subscribe("turn.terminal")`. So leaf 02 migrates the publisher to `PublishT` and declares the topic; there is no SubscribeT call site for this topic today.
2. **`agent.quota_wait` is POLYMORPHIC - it stays RAW.** Three distinct payload shapes ride this one topic: `agent.QuotaEvent` (internal/agent/loop.go:2093, wired from QuotaEpisodeTracker), `agent.ParkTurnEvent` (internal/agent/parked_turn.go:712, class="quota"|"throttle"), and a job-level `map[string]any` payload (internal/daemon/components.go:8318, class="quota_wait", includes a user-facing message). Consumers discriminate by class/reason fields (services/quota_notifier.go decodes to map and reads keys; TUI reads unblock_at/class). A single `Topic[QuotaEvent]` would decode ParkTurnEvent JSON into partially-zero QuotaEvent fields - silent data corruption, strictly worse than today. Per the scope-discipline rule this topic is declared RAW with a labeled comment, NOT typed. Leaf 02 documents the polymorphism at a single canonical comment site.
3. **WS event types number six, not two**: `chat_message`, `agent_progress`, `metrics_update`, `job_update`, `plan_update`, and the generic default `event` (server.go:703-758). Leaf 03's WSClass enum carries all six.

Migration set (final, per the amendments above):
- Tier 1 typed: `turn.terminal` only (publisher-side; TopicTurnTerminal declared on agent.TurnTerminalEvent).
- RAW-labeled: `agent.quota_wait` (polymorphic - see finding 2).
- Tier 2 marker: the WS-visible payload structs that exist as named types (TurnTerminalEvent, chat message payloads, others as leaf 03 finds); everything else stays on the prefix-table fallback.
- Explicitly NOT typed in this tree: `chat.request`/`chat.response` RPC-over-bus pair, wildcard-only subscriptions, and any multi-shape/open-ended topic discovered (quota_wait is the first, labeled).

## Interface Contracts

These are the frozen contracts all leaves implement against. Quote them verbatim in dispatch contexts.

### Contract 1: Topic declaration type

```go
// File: internal/bus/topic.go
package bus

// Topic is a compile-time-checked bus topic with a typed payload.
// Declare topics as package-level vars; reference the same var from
// publisher and subscriber so the compiler enforces payload agreement.
type Topic[T any] struct {
    Name string
}

// NewTopic declares a typed topic. Name must match the legacy string
// topic exactly - typed and raw publishes to the same name interoperate.
func NewTopic[T any](name string) Topic[T] { return Topic[T]{Name: name} }
```

Owner: leaf 01. Consumers: leaves 02, 03.

### Contract 2: Typed publish/subscribe wrappers

```go
// File: internal/bus/topic.go (same file)

// PublishT marshals payload as T and publishes to t's topic.
// Panics only on marshal failure of T (programmer error, same class as
// the raw path's NewBusMessage error return - here we surface it loudly;
// see leaf 01 Task 3 for the error-returning variant decision).
func PublishT[T any](b *MessageBus, t Topic[T], source string, payload T) int

// SubscribeT subscribes to t and invokes fn with each decoded payload.
// Decode failures are logged (with topic + type name) and dropped - the
// same behavior a hand-rolled json.Unmarshal error path has today, but
// centralized in one place.
func SubscribeT[T any](b *MessageBus, id string, t Topic[T], fn func(T)) *Subscriber
```

- Signature decision (binding): wrappers are package-level generic functions taking `*MessageBus` as first arg, NOT methods on `*MessageBus`. Go methods cannot introduce new type parameters. Do not try `func (b *MessageBus) SubscribeT[T any](...)` - it does not compile.
- Round-trip guarantee (binding): `PublishT` then `SubscribeT` delivery must equal a deep-equal payload for the migrated types. Leaf 01 tests this.
- Interop guarantee (binding): a raw `b.Publish("turn.terminal", NewBusMessage(...))` and a typed `bus.PublishT(b, TopicTurnTerminal, ...)` deliver to the same subscribers. The topic name string is the join key. Leaf 01 tests this too.

Owner: leaf 01. Consumers: leaves 02, 03.

### Contract 3: Migrated topic declarations (AMENDED per verification findings)

```go
// File: location decided by leaf 02 (pkg/models/topics.go expected - bus
// cannot import internal/agent without a cycle). Contract names:

var TopicTurnTerminal = NewTopic[agentpkg.TurnTerminalEvent]("turn.terminal")

// agent.quota_wait is DECLARED RAW - it is polymorphic (QuotaEvent,
// ParkTurnEvent, and a job-level map payload share the topic; see master
// Architecture, finding 2). No Topic var is created for it. Leaf 02 adds
// a canonical comment at the declaration site:
//
//   // RAW TOPIC (polymorphic payload): "agent.quota_wait" carries
//   // QuotaEvent, ParkTurnEvent, and job-level map payloads. Do NOT
//   // wrap in Topic[T] until the shapes unify - a single T silently
//   // zero-fills the other shapes' fields.
```

- The exact payload type is `agent.TurnTerminalEvent` (verified: internal/agent/handler.go:233). It already exists as a named, frozen struct - no promotion needed.
- Package placement decision stays with leaf 02 per its Task 1; `pkg/models/topics.go` is the expected acyclic location (bus already imports pkg/models; agent imports bus). The declarations must not create an import cycle - verify with `go build` after adding.
- Leaf 02 scope after amendment: declare TopicTurnTerminal, migrate the single publisher (handler.go publishTurnTerminal) to PublishT, add the RAW-topic comment for agent.quota_wait. There are NO SubscribeT call sites for turn.terminal today (verified - consumers are the WS `*` wildcard and the TUI RPC stream). Do not invent one. The typed topic still buys: compile-checked publisher payload + a declaration any future subscriber must use.

Owner: leaf 02 (declarations + migration of the two Tier 1 topics). Consumers: leaves 03, 04, 05.

### Contract 4: WS classification marker (AMENDED per verification findings)

```go
// File: internal/comm/wsclass/wsclass.go (new package, no deps on comm/http)

type WSClass int

const (
    WSChatMessage  WSClass = iota // renders as a chat bubble in Flutter
    WSProgress                    // agent_progress
    WSMetricsUpdate               // metrics_update
    WSJobUpdate                   // job_update
    WSPlanUpdate                  // plan_update
    WSEvent                       // generic "event" (the old default branch)
)

// WSClassified is implemented by bus event payloads that reach the
// WebSocket relay. transformBusEventToWS classifies by this marker
// instead of topic string prefix matching.
type WSClassified interface {
    WSClass() WSClass
}
```

- SIX classes, matching the six event types at server.go:703-758 (chat_message, agent_progress, metrics_update, job_update, plan_update, default event) - the authoring-time two-class assumption was wrong.
- Payload structs that reach the WS relay implement `WSClass() WSClass`. The marker methods live WITH the payload structs (owning packages), not in comm.
- `transformBusEventToWS` keeps its existing string-prefix cases as FALLBACK for topics whose payloads are not yet `WSClassified` (the raw path still delivers those). Classification order: type-assert `WSClassified` on the decoded payload first; fall back to the existing prefix table only when the assertion fails. The prefix table gets a comment: "legacy fallback - remove when all WS-visible topics are typed."
- The default branch behavior is unchanged: unknown topics remain `event` (current behavior - verified: server.go default branch sets "event", not agent_progress).
- In-scope marker implementations: `agent.TurnTerminalEvent` -> WSProgress (verified named struct). Other named WS-visible structs leaf 03 discovers at their publish sites (chat message payloads, metrics payloads, etc.) get markers matching their current prefix-table classification. Topics with map/anonymous payloads (agent.quota_wait's three shapes, employee.*, most task/step/queue publishers) stay on fallback - do NOT invent structs for them.

Owner: leaf 03. Consumers: leaf 04 (lint enforcement depends on the marker existing).

### Contract 5: exhaustive linter gate

```yaml
# .golangci.yml - added to the existing linters.enable list (file exists at repo root)
linters:
  enable:
    - exhaustive
```

- `exhaustive` runs over the `WSClass` type switch added in leaf 03. A new `WSClass` constant without a covering case fails `make lint-ci`.
- Scope guard: enable `exhaustive` repo-wide is acceptable ONLY if the existing codebase passes; if unrelated pre-existing switch statements fail, scope it via `.golangci.yml` `issues.exclude-rules` or `exhaustive` settings to the packages carrying WS payloads + comm/http, and say so in the leaf report. Do not silently exclude failures.

Owner: leaf 04.

### Contract 6: AGENTS.md edits (leaf 05, docs-only)

Leaf 05 applies these exact edits when leaves 01-04 are COMPLETE:

1. In "### WS event type classification": delete the three sentences instructing future authors to "verify they land in the correct bucket" for quota/model_escalated/turn topics, and replace the paragraph with a statement that classification derives from each payload's `WSClass()` marker (see `internal/comm/wsclass`), enforced by the `exhaustive` linter; new WS-visible payload types implement the marker and the switch coverage is CI-checked. Keep the `chat.response` exclusion sentence (still true - it is an RPC reply, not relayed).
2. Add to the Key Components table row for Server or Bus: mention `internal/bus/topic.go` typed topics (Topic[T], PublishT/SubscribeT).
3. In the Critical Invariants section, add a short invariant: "New bus topics with stable payloads are declared as `bus.Topic[T]` vars in `internal/bus/topics.go` (or `pkg/models/topics.go`); publishers use `bus.PublishT`, subscribers use `bus.SubscribeT`. Raw string Publish/Subscribe remains for wildcard subscriptions, request/response bus patterns (chat.request/chat.response), and caller-defined payloads - do not wrap `map[string]any` in a Topic; that is ceremony without a guarantee."
4. Do NOT touch any other AGENTS.md content. The file is 45K and heavily load-bearing.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-topic-generic.md | leaf | none | 45K | A |
| 02 | 02-migrate-scarred-topics.md | leaf | 01 (COMPLETE) | 60K | B |
| 03 | 03-ws-classification.md | leaf | 02 (COMPLETE) | 65K | C |
| 04 | 04-exhaustive-lint.md | leaf | 03 (COMPLETE) | 25K | D |
| 05 | 05-agents-md-cleanup.md | leaf | 01-04 (all COMPLETE) | 15K | E |

This tree is a dependency chain (A through E) - no parallel waves. Rationale: each leaf builds on the previous leaf's committed code, and leaves are individually small. Do not dispatch leaf N+1 before leaf N is COMPLETE. The whole tree is roughly sequential; the per-leaf work is sized so each fits one dispatch comfortably.

**Concurrency groups:** single-letter groups A-E, each of size 1 - dispatch strictly in order.

## Dispatch Protocol

For each leaf, in order 01 -> 05:

### Phase 1: Dispatch Implementation Agent

1. **Read** the leaf document (e.g. `01-topic-generic.md`).
2. **Dispatch via `delegate_task`** (single-task form):
   - `goal`: one-line imperative from the leaf header.
   - `context`: the FULL leaf document text + the frozen contracts from this master (verbatim) + the coding conventions block below + the current source of the files the leaf modifies INLINED (use `terminal cat`, not read_file, to obtain them) + these mandatory sentences:
     - "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
     - "Do NOT use read_file on existing source files - explore with search_files or terminal cat instead. Never feed read_file output into write_file."
     - "After writing a file, do NOT read it back to verify. Write once and stop."
     - "Test commands use -p 2 and -short: `go test -p 2 -short ./internal/bus/...` etc. Unbounded parallelism exhausts ephemeral ports on this machine."
   - If the leaf depends on an earlier leaf's committed code (leaves 02+), also inline: the earlier leaf's "What This Leaf Exposes" section content as committed (the orchestrator confirms it exists on disk with a grep before dispatching).
3. **Before dispatching leaf 03**, the orchestrator greps `internal/agent/` for the real payload struct names at the `turn.terminal` and `agent.quota_wait` publish sites and inlines the actual struct definitions into the dispatch context (leaf 02's report names them, but verify - do not trust the summary).

### Phase 2: Review and Commit

After each implementation agent returns, review IN-SESSION (main model, never a delegated reviewer):

1. Read the changed files listed in the agent's report.
2. Verify against the leaf spec + the frozen contracts + the Review Checklist below.
3. Run: `go build ./internal/... && go test -p 2 -short ./internal/bus/... ./internal/comm/... ./internal/agent/...` (leaf 04 adds `make lint-ci`; leaf 05 is docs-only - verify cross-references instead).
4. Run the line-number corruption check on changed files: `grep -nE '^\s+[0-9]+\|' <changed files>` must return nothing.
5. If gaps: re-dispatch with specific feedback (max 3 cycles), then escalate.
6. If pass: `git add <exact paths from the leaf> && git commit -m "<leaf's commit message>"`, update the tracking table to REVIEWED.

Commit messages (fixed, one per leaf):
- leaf 01: `feat(bus): generic Topic[T] declarations with typed publish/subscribe wrappers`
- leaf 02: `feat(bus,agent): migrate turn.terminal and agent.quota_wait to typed topics`
- leaf 03: `feat(comm): WS event classification via WSClassified marker, prefix table as fallback`
- leaf 04: `chore(lint): enforce exhaustive WSClass switch coverage in CI`
- leaf 05: `docs: AGENTS.md - typed-topic invariant, marker-based WS classification, package table update`

Commit policy: ONLY the orchestrator commits. Implementation agents never run git commands.

### Phase 3: Integration Review

After leaf 05 is REVIEWED:

1. Full suite: `go build ./... && go test -p 2 -short ./...` must pass.
2. `make lint-ci` must pass (leaf 04's gate included).
3. `make graphs` then `make graphs-check` - the connectivity graph generator parses string-literal topic references; after this tree it must still pass (typed wrappers publish through the same `b.Publish` call, so string literals remain findable in `topic.go`/`topics.go`). If graphs-check fails because the generator lost topic discovery, that is a REAL regression: fix the generator or record the failure in Notes and escalate - do not mask it.
4. Grep for regressions: `grep -rEn 'PublishT\(|SubscribeT\(' internal/ --include="*.go" | grep -v _test | wc -l` - expect the count leaf 02/03 report. `grep -c 'transformBusEventToWS' internal/comm/http/server.go` - unchanged.
5. Verify e2e chat harness untouched and passing: `bash scripts/e2e-naive-user-chat.sh` (if it requires a running daemon, run it per docs/workflows; if it cannot run in this environment, record that in Notes - do not claim it passed without running it).
6. Normalize formatting: `gofmt -l` on changed files must be empty.
7. Update tracking table: all COMPLETE. Report COMPLETE to the user.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this master are satisfied exactly (signatures, file paths, package placement)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD: failing test seen before implementation, per leaf task steps)
- [ ] Raw (untyped) bus API is untouched and still compiles - no breaking changes to the 136 existing publish sites
- [ ] No scope creep: no topics migrated beyond the two Tier 1 topics (leaf 02) / no payloads marked beyond the WS-visible set (leaf 03)
- [ ] Scope-discipline rule respected: no `map[string]any` or caller-defined payloads wrapped in a Topic; anything discovered to be open-ended is LEFT RAW and named in the leaf report
- [ ] No debug artifacts: no print/stdout debugging, no TODOs, no placeholder values, no commented-out code
- [ ] No line-number corruption: no `N|` prefixes in source files
- [ ] gofmt clean on changed files

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go (module `meept`; check go.mod for version). Generics require >= 1.18 - repo is well past this.
- **Naming:** exported PascalCase; unexported camelCase. Topic vars: `Topic` + PascalCase topic name (`TopicTurnTerminal`).
- **Error handling:** wrap with `%w`; no panics in library code EXCEPT the documented PublishT marshal-failure decision from leaf 01 Task 3 (if that task selects panic, it must be documented at the call site contract and tested).
- **Testing:** stdlib `testing` + the repo's existing assertion style - check an existing `internal/bus/bus_test.go` first and match it; table-driven where natural; `_test.go` alongside.
- **Formatting:** `gofmt` before reporting completion.
- **Comments:** doc comments on all exported symbols (Godoc style, start with the symbol name).
- **No new dependencies.** This work needs only stdlib + existing module deps.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-topic-generic | COMPLETE | 1 | Reviewed in-session: contract-exact, 7 tests green, panic-on-marshal documented, both interop directions tested. Committed a384204f. PublishBlockingT added later by bughunt 18c34d66 (accepted). |
| 02-migrate-scarred-topics | COMPLETE | 3 | Amended shape: declaration + RAW label + tests only (publisher typed by 18c34d66). Wildcard correction accepted (turn.* not *). Committed aa7cf489 after 2 commit races with a parallel session. |
| 03-ws-classification | COMPLETE | 1 | Subagent implementation; reviewed in-session. wsclass pkg (6 classes), marker on TurnTerminalEvent, marker-first + labeled fallback, parity fence 14 cases. chat.response measured as agent_progress (plan assumption corrected). wsclass files landed via parallel 1ccdb36f; server.go committed 98c6823d, marker test 6c21a86b. |
| 04-exhaustive-lint | COMPLETE | 1 | Orchestrator-implemented (config-only). Scoped via explicit-exhaustive-switch + //exhaustive:enforce ON THE SWITCH (doc-comment marker is invisible to the linter). RED experiment proved firing; 50 pre-existing findings kept out of scope. Committed a44ec38b. |
| 05-agents-md-cleanup | COMPLETE | 1 | Orchestrator-implemented. 3 hunks staged via filtered patch (sibling refusal hunk left uncommitted). Committed 9294e2ca. |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

Run in order after leaf 05 reaches REVIEWED:

1. `go build ./...` - compiles clean.
2. `go test -p 2 -short ./...` - full suite green.
3. `make lint-ci` - includes new `exhaustive` gate.
4. Targeted: `go test -p 2 ./internal/bus/ -run TestTopic -v` - the typed-topic round-trip + interop tests from leaves 01/02.
5. Targeted: `go test -p 2 ./internal/comm/http/ -run TestWSClass -v` - marker-based classification tests from leaf 03.
6. `make graphs && make graphs-check` - connectivity graph still generated and fresh (see Phase 3 step 3 for the failure protocol).
7. Cross-boundary verification (manual, in-session): `grep -n 'WSClass()' internal/ -r` shows marker methods living with payload structs in their owning packages, not in comm/http. `grep -n 'PrefixTable\|legacy fallback' internal/comm/http/server.go` shows the retained fallback commented as legacy.
8. Docs check: AGENTS.md contains the new invariant sentence and no longer instructs manual bucket verification for the migrated topics; the `chat.response` exclusion sentence is retained.

## Structural Completeness Check (Before Dispatch)

After writing every document in this tree, run:

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans/20260916-typed-bus-topics --strict-leaves
```

Required headings (exact strings): `## Dispatch Protocol`, `## Interface Contracts`, `## Child Index`, `## Review Checklist`, `## Coding Conventions`, `## Completion Tracking Table`, `## Integration Test Plan`. Leaves carry a Self-Verification Checklist and "Do NOT commit".

## Notes

- Branch state at authoring time: `classifier-iteration` with pre-existing modifications to AGENTS.md, README.md, config/models.json5, docs/configuration/*. The orchestrator MUST NOT sweep these unrelated dirty files into its commits - stage only exact leaf paths (per Pitfall 40 hygiene). If a leaf must edit a file that is already dirty in the worktree (AGENTS.md is - leaf 05 edits it), the orchestrator notes the pre-existing dirty state in the commit message body: "AGENTS.md had unrelated pre-existing modifications; only the hunks listed in leaf 05 are from this tree."
- Tier 3 (Dart-side schema codegen over the WS boundary) is OUT of scope here - it is filed as a standalone GitHub issue: bhodgens/meept#48 (https://github.com/bhodgens/meept/issues/48).
- The `exhaustive` linter may flag pre-existing non-WS switches when enabled repo-wide. Leaf 04's task covers the scoping fallback; the orchestrator reviews the final .golangci.yml diff to confirm the scoping is honest and documented, not a blanket exclusion.
- Payload struct promotion (leaf 02): if a publish site marshals an anonymous struct, promoting it to a named exported struct in the owning package is in scope; renaming existing exported types is NOT (that breaks other consumers) - if promotion requires a rename, stop and report.
- Sub-agent timeout discipline: every leaf here is sized well under the wall-clock cap. If a leaf times out anyway, audit `git status` for partial work, then re-dispatch only the missing pieces (never the whole leaf blindly).
