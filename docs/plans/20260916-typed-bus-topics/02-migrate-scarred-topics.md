# Migrate turn.terminal to a Typed Topic + Label quota_wait RAW - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Declare `TopicTurnTerminal` on the frozen `agent.TurnTerminalEvent`, migrate its single publisher to `PublishT`, and add the canonical RAW-topic comment for polymorphic `agent.quota_wait`.
- **Dependencies:** 01-topic-generic.md (COMPLETE - `internal/bus/topic.go` exists with Topic[T], PublishT, SubscribeT)
- **Estimated Context:** 50K
- **Concurrency Group:** B

## AMENDMENT (orchestrator, post-verification - this overrides the original task shape)

Pre-dispatch verification changed the picture. Verified facts (trust these; re-confirm only the exact line numbers):

1. **turn.terminal has NO direct bus subscriber.** Consumers are (a) the WS relay via its `*` wildcard subscription (internal/comm/http/server.go ~536, topics list includes bare "*") and (b) the TUI via the RPC event stream (internal/tui/events.go:60 topic filter - RPC polling, not bus.Subscribe). There is no `bus.Subscribe(..., "turn.terminal")` anywhere. You will NOT create a SubscribeT call site. Do not invent one.
2. **The payload is the frozen `TurnTerminalEvent`** (internal/agent/handler.go:233-253, field set CLOSED per its own doc comment). It already exists - no promotion, no changes to the struct.
3. **The single publisher is `ChatHandler.publishTurnTerminal`** (internal/agent/handler.go ~1574-1587). Current body: builds via models.NewBusMessage, sets msg.Topic, calls h.bus.Publish.
4. **agent.quota_wait stays RAW** - it carries three payload shapes (`QuotaEvent` via loop.go ~2093, `ParkTurnEvent` via parked_turn.go ~712, job-level `map[string]any` via daemon/components.go ~8318). This leaf only ADDS the canonical RAW comment; no code changes to any quota_wait site.

## Goal

One typed topic (turn.terminal), publisher migrated; one labeled raw topic (agent.quota_wait); tests proving the migrated publisher delivers the frozen payload through the real bus to a raw wildcard subscriber unchanged.

## Context

Key files:

- `internal/bus/topic.go` - leaf 01's wrappers (consumed, not modified)
- `internal/agent/handler.go` - TurnTerminalEvent definition (~233) + publishTurnTerminal (~1574)
- `pkg/models/` - expected home for the topic declaration (bus imports pkg/models; agent imports bus; a declaration in pkg/models importing agent is ALSO a cycle - see Task 1 decision)

Import-cycle reality to resolve in Task 1: the declaration needs `NewTopic[TurnTerminalEvent]`, and TurnTerminalEvent lives in package agent. `pkg/models` cannot import `internal/agent` (agent imports pkg/models - cycle). `internal/bus` cannot import `internal/agent` either. The clean solution: declare the topic IN package agent (e.g. `internal/agent/topics.go`: `var TopicTurnTerminal = bus.NewTopic[TurnTerminalEvent]("turn.terminal")`) - agent already imports bus. The declaration then lives beside the payload struct it types, which is the pattern going forward. Verify with `go build` and record the decision.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/topics.go (expected; Task 1 confirms placement)
package agent

import "github.com/caimlas/meept/internal/bus"

// TopicTurnTerminal is the typed declaration for the turn.terminal bus
// topic. Publishers MUST use bus.PublishT with this declaration; future
// subscribers MUST use bus.SubscribeT with it. Payload is the frozen
// TurnTerminalEvent - see its doc comment (CLOSED field set).
var TopicTurnTerminal = bus.NewTopic[TurnTerminalEvent]("turn.terminal")

// RAW TOPIC (polymorphic payload): "agent.quota_wait" carries QuotaEvent
// (loop.go quota tracker wiring), ParkTurnEvent (parked_turn.go), and a
// job-level map payload (daemon/components.go publishQuotaWait). Do NOT
// wrap in Topic[T] until the shapes unify - a single T silently
// zero-fills the other shapes' fields. Consumers decode to
// map[string]any and discriminate by class/reason keys.
```

- Publisher migration: `publishTurnTerminal` uses `bus.PublishT(h.bus, TopicTurnTerminal, SourceChatHandler, ev)` - replacing the NewBusMessage + Topic-set + Publish sequence. Behavior preserved: same topic name string, same Source value, MessageType event, external delivery semantics of plain Publish (note: current code uses h.bus.Publish, not PublishExternalOnly - keep plain Publish semantics, which PublishT wraps).
- Error handling: current code logs `h.logger.Error("failed to build turn.terminal event", ...)` on marshal failure. PublishT per leaf 01's decision panics OR returns int-only on marshal failure - if PublishT cannot log-and-drop like the current code, wrap the call: marshal the event yourself first (`json.Marshal(ev)`), log-and-return on error exactly as today, then pass the check passed - or if leaf 01 chose error-returning PublishT variant, use it and keep the log-and-drop. MATCH THE CURRENT log-and-drop behavior - the parker side channels established "a marshal failure is logged and never affects the turn" as the repo posture. Report exactly how you preserved it.
- NO changes to: TurnTerminalEvent struct, any quota_wait call site, the WS relay, the TUI.

### What This Leaf Consumes

```go
// From 01-topic-generic.md (committed):
bus.NewTopic[T](name string) Topic[T]
bus.PublishT[T](b *MessageBus, t Topic[T], source string, payload T) int
```

## Tasks

### Task 1: Declaration + placement decision

**Objective:** Create internal/agent/topics.go with TopicTurnTerminal + the RAW comment for quota_wait.

**Files:**
- Create: `internal/agent/topics.go`

**Step 1: Read current content (exploration)**

- `terminal("grep -rn 'Subscribe(' internal/ --include='*.go' | grep -v _test | grep 'turn.terminal'")` - confirm zero direct subscribers
- Confirm the import graph: `terminal("head -30 internal/agent/handler.go")` shows agent importing bus already.

**Step 2: Confirm old state**

Record: the exact current publishTurnTerminal body (cat handler.go around 1574-1587), confirmation of zero direct subscribers, leaf 01's PublishT signature (cat internal/bus/topic.go).

**Step 3: Write implementation**

Create topics.go per the contract. The RAW comment is part of the deliverable - it is the canonical warning future agents read.

**Step 4: Verify**

`go build ./internal/agent/ ./internal/bus/` - compiles; declaration unused-but-referenced (publisher migration is Task 2, so the var may be temporarily unused - go build does not fail on unused package-level vars; if a lint gate complains in CI that is leaf 04/05 territory, note it).

### Task 2: Migrate the publisher

**Objective:** publishTurnTerminal uses the typed path with preserved log-and-drop marshal semantics.

**Files:**
- Modify: `internal/agent/handler.go` (publishTurnTerminal only)
- Test: `internal/agent/handler_turnterminal_typed_test.go` (new file)

**Step 1: Write failing test**

```go
func TestPublishTurnTerminal_TypedDelivery(t *testing.T) {
    // Real MessageBus (bus.New(nil, nil), deferred Close).
    // Raw wildcard subscriber: sub := b.Subscribe("wildcard", "*") - this
    // is how the WS relay actually consumes the topic today.
    // Call h.publishTurnTerminal(TurnTerminalEvent{...representative fields...}).
    // Assert: sub receives exactly one BusMessage; Topic == "turn.terminal";
    // Type == models.MessageTypeEvent; Source == SourceChatHandler;
    // unmarshal(msg.Payload) deep-equals the published event.
}
```

If ChatHandler is heavy to construct, check how existing handler tests build one (search internal/agent for existing ChatHandler test fixtures) and reuse that pattern.

**Step 2: Run test to verify failure**

The test may PASS against the old code path (it publishes the same shape). To make it genuinely RED-first, add an assertion that fails pre-migration only if you can observe the typed path - e.g. assert via the topics declaration: publish through `bus.PublishT(b, TopicTurnTerminal, ...)` in a second sub-test and confirm identical wire bytes to the handler's output (json.Marshal of the same event). Acceptance: the handler test suite passes AND a compile-level check that the handler references TopicTurnTerminal: `grep -n 'TopicTurnTerminal' internal/agent/handler.go` non-empty is the real gate. State this honestly in your report - the behavior is unchanged by design; the leaf's deliverable is the typed call path.

**Step 3: Write implementation**

Rewrite publishTurnTerminal per the contract (preserve log-and-drop marshal semantics per the error-handling note).

**Step 4: Run test to verify pass**

`go build ./... && go test -p 2 -short ./internal/agent/ -run 'TestPublishTurnTerminal' -v` then the full package: `go test -p 2 -short ./internal/agent/`. Full-tree sanity: `go test -p 2 -short ./internal/comm/... ./internal/tui/...` (the WS + TUI consumers must be untouched and green).

### Task 3: Verify no functional drift anywhere

**Objective:** Prove the migration is wire-identical.

**Files:** none new

**Step 1: Write test (wire-identity check, in the same test file)**

```go
func TestPublishTurnTerminal_WireIdentity(t *testing.T) {
    // Marshal TurnTerminalEvent{...} directly with json.Marshal - this is
    // the pre-migration wire format (NewBusMessage marshaled the same struct
    // with the same tags). Assert the received message's Payload bytes equal
    // that marshal. This pins: field tags, omitempty behavior, timestamp
    // absence (the event struct carries no timestamp field of its own).
}
```

**Step 2: Run to verify pass**

`go test -p 2 -short ./internal/agent/ -run 'WireIdentity' -v` - PASS (if FAIL, your migration changed the wire - fix the migration, never the test).

**Step 3: Grep verification**

- `grep -rn '"turn.terminal"' internal/ --include='*.go' | grep -v _test` - hits allowed ONLY in: agent/topics.go (declaration), agent/handler.go (comment references ok), comm/http/server.go (WS prefix fallback - untouched), tui/ (RPC stream - untouched). NO other functional publish/subscribe sites.
- `grep -rn '"agent.quota_wait"' internal/ --include='*.go' | grep -v _test | grep -v '//'` - all functional sites UNCHANGED (loop.go, parked_turn.go, components.go, quota_notifier.go, tui/app.go).

**Step 4: Full verify**

`go build ./... && go test -p 2 -short ./internal/...` green. gofmt clean on touched files.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] internal/agent/topics.go exists: TopicTurnTerminal declared on TurnTerminalEvent + RAW comment for agent.quota_wait
- [ ] publishTurnTerminal migrated to PublishT (or marshal-check + PublishT), log-and-drop semantics preserved
- [ ] Zero SubscribeT call sites invented; zero changes to quota_wait sites, WS relay, TUI
- [ ] Wire-identity test passes; wildcard subscriber receives identical bytes
- [ ] `go build ./...` + `go test -p 2 -short ./internal/...` green
- [ ] gofmt clean; no debug artifacts

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Exactly one topic typed, exactly one RAW comment added, nothing else
- [ ] TurnTerminalEvent struct untouched (diff shows no changes to lines 233-253)
- [ ] Publisher behavior: topic name, Source, MessageType, wire bytes, marshal-failure logging all preserved
- [ ] topics.go placement creates no import cycle (agent imports bus - one direction only)
- [ ] Grep verification output in report matches expectations

Output: APPROVED or specific gaps with file + line references.

## Notes

- The typed topic's immediate value is narrow (compile-checked publisher + a declaration for future subscribers). Its real value arrives as more topics migrate onto the pattern. Do not oversell or overbuild - no fan-out to other topics.
- If leaf 01's PublishT cannot preserve log-and-drop marshal semantics (e.g. it panics), do NOT change leaf 01's code - wrap at the call site (pre-marshal with json.Marshal, log-and-drop on error, then call PublishT; the double marshal is negligible for a per-turn event, note it).
