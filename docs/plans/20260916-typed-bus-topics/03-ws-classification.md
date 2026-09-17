# WS Classification via WSClassified Marker - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Add the `WSClassified` marker interface and switch `transformBusEventToWS` from topic-string prefixes to payload-marker classification, keeping the prefix table as a documented legacy fallback.
- **Dependencies:** 02-migrate-scarred-topics.md (COMPLETE - TopicTurnTerminal/TopicAgentQuotaWait exist; the leaf 02 report names the real payload types and the topics-file location)
- **Estimated Context:** 65K
- **Concurrency Group:** C

## Goal

Kill the misclassification bug class at `internal/comm/http/server.go:703-746`. Today classification is a chain of `topic ==` and `strings.HasPrefix(topic, ...)` cases; a new topic either matches a prefix by luck or falls into a default. The fix: payloads that reach the WS relay implement `WSClass() WSClass`; the relay type-asserts the decoded payload and classifies from the marker. The prefix table remains ONLY as fallback for raw-path topics without markers, commented as legacy. A follow-on leaf adds `exhaustive` lint coverage (leaf 04) - this leaf does not add the linter.

## Context

Current classification (verify with terminal cat, line numbers may drift):

- `server.go:703` - `case topic == "chat_message" || topic == "chat.message.received":` -> `chat_message`
- `server.go:711` - `strings.HasPrefix(topic, "chat.")` -> agent_progress (with the documented exception: `chat.response` is NEVER relayed)
- `server.go:717` - `strings.HasPrefix(topic, "agent.quota")` -> agent_progress
- `server.go:727` - `agent.model_escalated` -> agent_progress
- `server.go:733` - `turn.` -> agent_progress
- `server.go:741+` - `metrics.`, `task./step./job./queue.`, `plan.` and a default -> agent_progress

Read the whole `transformBusEventToWS` function AND the surrounding relay loop (`frontendData := transformBusEventToWS(msg)` ~line 567) before changing anything. Note: the relay sees `*models.BusMessage` with `Payload json.RawMessage` - to marker-classify, the relay must decode the payload into the typed payload struct. Topics migrated in leaf 02 have typed wrappers; topics still on the raw path have none.

Key files:

- `internal/comm/http/server.go` - the classification + relay
- `internal/bus/topics.go` or `pkg/models/topics.go` - leaf 02's declarations (master tracking table records the location the orchestrator confirmed)
- Payload structs in their owning packages (internal/agent, others as found)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/comm/wsclass/wsclass.go (new package, no deps beyond stdlib)
package wsclass

type WSClass int

const (
    WSChatMessage WSClass = iota // renders as a chat bubble in Flutter
    WSProgress                   // renders as agent_progress
)

// WSClassified is implemented by bus event payloads that reach the
// WebSocket relay. transformBusEventToWS classifies by this marker
// instead of topic string prefix matching.
type WSClassified interface {
    WSClass() WSClass
}
```

- Marker methods live WITH the payload structs in their owning packages (`func (p TurnTerminalPayload) WSClass() wsclass.WSClass { return wsclass.WSProgress }`) - NOT in comm/http. Verify this does not create an import cycle: wsclass must not import agent; agent imports wsclass. That direction is clean.
- Scope (binding): add `WSClass()` methods ONLY to payload types for topics in the current prefix table's sets that have a NAMED payload struct. Concretely at minimum: turn.terminal (leaf 02's type), agent.quota_wait (leaf 02's type), and any other WS-visible payload struct that already exists as a named type (search the publish sites of the prefix sets). If a topic's payload is map[string]any / anonymous / caller-defined: do NOT invent a struct for it - it stays on the prefix-table fallback. Count and name the fallback topics in your report.
- `transformBusEventToWS` signature may change (it is unexported) but its callers' behavior must not: same `map[string]any` frontend shape, same event type strings.

### What This Leaf Consumes

```go
// From leaf 02 (committed): the typed payload structs for turn.terminal
// and agent.quota_wait, and the topics-file location decision.
// From pkg/models: models.BusMessage (existing).
```

## Tasks

### Task 1: wsclass package + marker methods

**Objective:** Create the marker package; implement `WSClass()` on the named WS-visible payload structs.

**Files:**
- Create: `internal/comm/wsclass/wsclass.go`
- Modify: the payload-struct files in their owning packages (add marker methods)
- Test: `internal/comm/wsclass/wsclass_test.go`

**Step 1: Write failing test**

```go
func TestWSClassConstants(t *testing.T) {
    // Assert iota ordering: WSChatMessage == 0, WSProgress == 1.
    // Assert the WSClassified interface is satisfied by the migrated
    // payload types (compile-time assertions via var _ wsclass.WSClassified = ...).
}
```

**Step 2: Run test to verify failure**

Run: `go test -p 2 -short ./internal/comm/wsclass/ -v`
Expected: FAIL - package does not exist / types not implemented.

**Step 3: Write implementation**

Package + constants + interface per contract. Then add marker methods to payload structs: turn.terminal and agent.quota_wait types get `WSProgress`. For each OTHER named payload struct serving a prefix-table topic, add the marker matching its current prefix-table classification (chat_message for the chat message payloads; agent_progress for everything else). Report the full list of marked vs fallback topics.

**Step 4: Run test to verify pass**

`go build ./... && go test -p 2 -short ./internal/comm/wsclass/` green; owning packages still compile and their tests pass.

### Task 2: Switch transformBusEventToWS to marker-first classification

**Objective:** Marker assertion first; prefix table retained as fallback with legacy comment.

**Files:**
- Modify: `internal/comm/http/server.go` (transformBusEventToWS + wherever the relay decodes payloads)
- Test: extend or create `internal/comm/http/server_wsclass_test.go`

**Step 1: Write failing test**

Table-driven test over `transformBusEventToWS` (it is unexported - test in-package):

- Case: BusMessage with topic `turn.terminal` and a JSON payload of the turn-terminal type -> `agent_progress` (via MARKER - assert by publishing through the typed path or by constructing the BusMessage with that payload).
- Case: BusMessage with topic `agent.quota_wait` -> `agent_progress` via marker.
- Case: a chat-message payload type -> `chat_message` via marker.
- Case: UNKNOWN topic with raw `map[string]any` payload that matches NO prefix -> `agent_progress` (default, unchanged).
- Case: topic `some.brand.new.thing` with an untyped payload -> still `agent_progress` via FALLBACK (not marker) - and a case for a legacy prefix like `metrics.cpu` -> `agent_progress` via fallback.
- Case: `chat.response` topic -> must not appear as chat_message (preserve the exclusion; check how the relay excludes it today and keep that behavior).

**Step 2: Run test to verify failure**

Run: `go test -p 2 -short ./internal/comm/http/ -run TestWSClass -v`
Expected: FAIL (marker path not implemented).

**Step 3: Write implementation**

- In the relay path (or inside transformBusEventToWS if decoding there is cleaner - choose the spot where a decode error can be handled without changing the frontend contract), attempt `json.Unmarshal(msg.Payload, &candidate)` for typed delivery. Prefer: if the relay already holds the decoded value from leaf 02's SubscribeT callback path, use it; if the relay still consumes raw BusMessages from its own subscription, decode here. Record the mechanics in your report.
- Classification order in transformBusEventToWS: (1) `chat.response` exclusion (unchanged, keep existing code), (2) type-assert the decoded payload against `wsclass.WSClassified` -> use marker, (3) existing prefix table as fallback, wrapped in a comment block: `// legacy fallback - remove when all WS-visible topics are typed`, (4) default `agent_progress`.
- Chat-message payloads marked WSChatMessage must produce type `chat_message` exactly as today.

**Step 4: Run test to verify pass**

`go test -p 2 -short ./internal/comm/http/` full package green. Then `go test -p 2 -short ./internal/...` full internal tree green (the e2e chat contract guards live elsewhere but the TUI/GUI event tests must not regress).

### Task 3: Document the derivation + verify no behavior change

**Objective:** Prove classification parity old-vs-new and leave the code self-explaining.

**Files:**
- Modify: `internal/comm/http/server.go` (comments only beyond Task 2)
- Test: none new

**Step 1: Write test (parity proof, in the same test file)**

A test that iterates EVERY topic string in the old prefix table (enumerate them from the pre-change code: chat_message, chat.message.received, chat.* examples, agent.quota*, agent.model_escalated, turn.*, metrics.*, task.*, step.*, job.*, queue.*, plan.*) with a minimal raw payload and asserts the event type is IDENTICAL to the pre-change classification (agent_progress for all except the chat_message set). This is the regression fence.

**Step 2: Confirm old state / run new test**

Run: `go test -p 2 -short ./internal/comm/http/ -run TestWSClassParity -v`
Expected: PASS (parity holds after Task 2 - if it fails, Task 2 changed behavior; fix Task 2, do not weaken the parity table).

**Step 3: Comments**

- At the prefix table: `// legacy fallback - remove when all WS-visible topics are typed (see internal/comm/wsclass).`
- At each migrated topic's publish site (if not already commented by leaf 02): one line noting the WS class.
- Report: the exact list of topics still on fallback and why (no named struct).

**Step 4: Verify**

`go build ./... && go test -p 2 -short ./internal/comm/... ./internal/agent/...` green. gofmt clean.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] `internal/comm/wsclass` exists with WSClass, the two constants, WSClassified; no imports beyond stdlib
- [ ] Marker methods live in payload-owning packages (grep `func.*WSClass() wsclass.WSClass` shows them outside comm/)
- [ ] transformBusEventToWS: exclusion -> marker -> fallback(prefix) -> default, in that order
- [ ] Parity test passes over every legacy prefix topic
- [ ] No payload struct invented for map[string]any topics; fallback list in report
- [ ] `go build ./...` + `go test -p 2 -short ./internal/...` green; gofmt clean
- [ ] No debug artifacts

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Marker-based classification actually drives the migrated topics (not just tested alongside)
- [ ] Prefix table retained ONLY as fallback with the legacy comment
- [ ] chat.response exclusion preserved exactly
- [ ] Default behavior unchanged for unknown topics
- [ ] Parity test enumerates the full legacy prefix set
- [ ] No import cycle (wsclass imports nothing from internal/agent etc.)
- [ ] Contracts match: package path internal/comm/wsclass, names WSClass/WSChatMessage/WSProgress/WSClassified

Output: APPROVED or specific gaps with file + line references.

## Notes

- The Flutter client creates a bubble for every `chat_message` event - the parity test is the guard that keeps lifecycle events out of that bucket. Treat any parity failure as a REAL bug in the migration, never adjust the expected values.
- leaf 04 (exhaustive lint) builds on the type switch you write here - keep the switch a clean `switch cls := ...` over WSClass values so exhaustive can check it. If you use if/else chains, leaf 04 cannot enforce coverage; use a switch.
- The relay decode adds one json.Unmarshal per event for typed topics - negligible at this scale; note it in the report.
