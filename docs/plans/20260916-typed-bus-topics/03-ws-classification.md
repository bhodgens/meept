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
- **Scope:** Add the `WSClassified` marker interface (six classes) and switch `transformBusEventToWS` from topic-string prefixes to payload-marker classification, keeping the prefix table as a documented legacy fallback.
- **Dependencies:** 02-migrate-scarred-topics.md (COMPLETE - TopicTurnTerminal declared on agent.TurnTerminalEvent; quota_wait labeled RAW)
- **Estimated Context:** 65K
- **Concurrency Group:** C

## AMENDMENT (orchestrator, post-verification - overrides the original task shape)

Verified facts (line numbers approximate - re-confirm with grep):

1. **Six event types exist, not two**: `chat_message`, `agent_progress`, `metrics_update`, `job_update`, `plan_update`, and the default `event` (server.go:685-758). The WSClass enum carries all six. The old default branch sets `"event"` - preserve that exactly.
2. **The relay decodes payloads to `map[string]any` once** at the top of transformBusEventToWS (server.go ~693). For marker classification you need the TYPED value for migrated payloads - but most WS-visible topics still publish raw maps and stay on the fallback. Design below.
3. **agent.quota_wait is RAW-labeled (leaf 02)** - its three payload shapes do NOT get markers; it classifies via the prefix fallback exactly as today. This is intentional.
4. **The relay subscribes via wildcard patterns** (server.go ~530-545: "*", "agent.*", "chat.*", etc.) and receives raw `*models.BusMessage` - it does NOT use SubscribeT. You will not change the subscription mechanics.

## Goal

Classification order in transformBusEventToWS becomes: (0) `chat.response` exclusion unchanged, (1) marker assertion on the decoded payload when the payload was decoded into a typed value, (2) existing prefix table as legacy fallback, (3) default `event`. TurnTerminalEvent (the one typed WS-visible payload from leaf 02) classifies via its marker; everything else classifies via the unchanged fallback. The `exhaustive` linter (leaf 04) guards switch coverage.

## Context

Key files:

- `internal/comm/http/server.go` - transformBusEventToWS (~685-760), the relay loop (~520-580), and the chat_message payload normalization after the switch (~760+, which reads session_id/content keys from the map - UNTOUCHED)
- `internal/agent/topics.go` - TopicTurnTerminal (leaf 02)
- `internal/agent/handler.go` - TurnTerminalEvent (the frozen struct; marker method goes in this package, near the struct)

How marker classification works with a map-decoding relay: when the relay decodes msg.Payload, it currently gets map[string]any. For the typed path, after the map decode attempt, ALSO attempt a decode into the typed payload for topics that have a declared Topic[T]: concretely, check `msg.Topic == agent.TopicTurnTerminal.Name` (or, better, a small package-level lookup table mapping topic name -> decode function that returns a WSClassified) and decode into the typed struct; on success, classify via the marker; on decode failure, fall through to the prefix fallback. This table (in comm/http or wsclass) is the ONLY place that knows about specific payload types - it must not import more than wsclass + the packages owning typed payloads (agent). Keep it to the migrated set: exactly one entry today (turn.terminal).

Do NOT touch: the chat_message normalization block (~760+), the subscription topic list, handleWSEvent's broadcast logic, the `employee.*` classification comment.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/comm/wsclass/wsclass.go (new package; imports stdlib only)
package wsclass

type WSClass int

const (
    WSChatMessage  WSClass = iota // chat_message
    WSProgress                    // agent_progress
    WSMetricsUpdate               // metrics_update
    WSJobUpdate                   // job_update
    WSPlanUpdate                  // plan_update
    WSEvent                       // generic "event" (the old default)
)

// WSClassified is implemented by bus event payloads that reach the
// WebSocket relay. Classification prefers the marker over the legacy
// topic-prefix table.
type WSClassified interface {
    WSClass() WSClass
}
```

```go
// In the payload's owning package (internal/agent/handler.go, next to the struct):
// WSClass implements wsclass.WSClassified: turn lifecycle events render
// as agent_progress - never chat_message (blank-bubble invariant).
func (TurnTerminalEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }
```

- wsclass imports NOTHING from internal/ - direction is agent -> wsclass only.
- Named WS-visible payloads beyond TurnTerminalEvent: leaf 03 MAY add markers to other named structs ONLY if their publish site already marshals that exact named struct (verify with grep at the publish site). If a topic publishes map[string]any or anonymous structs (quota_wait's three shapes, employee.*, most task/step/queue publishers), it stays on fallback - do NOT invent structs. Record: marked topics vs fallback topics, in the report.
- transformBusEventToWS returns the same map shape; event-type strings unchanged; the six-way classification table is behavior-identical (parity test proves it).

### What This Leaf Consumes

```go
// From leaf 02 (committed): agent.TopicTurnTerminal, agent.TurnTerminalEvent.
// From pkg/models: models.BusMessage.
```

## Tasks

### Task 1: wsclass package + TurnTerminalEvent marker

**Objective:** Create the marker package and put the first marker method beside its struct.

**Files:**
- Create: `internal/comm/wsclass/wsclass.go`
- Modify: `internal/agent/handler.go` (marker method only - one 3-line func near TurnTerminalEvent)
- Test: `internal/comm/wsclass/wsclass_test.go`

**Step 1: Write failing test**

```go
func TestWSClassConstants(t *testing.T) {
    // iota ordering: WSChatMessage==0 .. WSEvent==5.
}
func TestTurnTerminalEventImplementsWSClassified(t *testing.T) {
    // var _ wsclass.WSClassified = agent.TurnTerminalEvent{}
    // and agent.TurnTerminalEvent{}.WSClass() == wsclass.WSProgress
    // (wsclass_test.go cannot import agent without... check the direction:
    // wsclass is imported BY agent, so this test lives in agent's package
    // instead - put it in internal/agent/handler_turnterminal_typed_test.go
    // or a new internal/agent/wsclass_test.go. wsclass_test.go keeps only
    // the constants test.)
}
```

**Step 2: Run test to verify failure**

`go test -p 2 -short ./internal/comm/wsclass/ ./internal/agent/ -run 'TestWSClass' -v` - FAIL.

**Step 3: Write implementation**

Package + constants + interface + the one marker method. Nothing more.

**Step 4: Run test to verify pass**

Both packages green.

### Task 2: Marker-first classification in transformBusEventToWS

**Objective:** Marker assertion before the prefix table; typed decode table for migrated topics; parity preserved.

**Files:**
- Modify: `internal/comm/http/server.go`
- Test: `internal/comm/http/server_wsclass_test.go` (new)

**Step 1: Write failing test**

Table-driven, in-package (transformBusEventToWS is unexported):

- turn.terminal with a marshaled TurnTerminalEvent payload -> `agent_progress` (this must pass BOTH pre- and post-change - it is a parity case; the marker case is distinguished by an additional assertion below).
- turn.terminal with CORRUPTED payload bytes (invalid for the struct) -> still `agent_progress` (decode fails, fallback catches it).
- quota_wait topic with a ParkTurnEvent-shaped map -> `agent_progress` via FALLBACK (no marker - assert classification result only).
- `chat_message` topic with a chat-shaped map -> `chat_message` (fallback path, unchanged).
- `metrics.cpu.sample` -> `metrics_update`; `task.completed` -> `job_update`; `plan.approved` -> `plan_update`; `employee.notify` -> `event` (default branch - verify what employee.* actually hits today by reading the switch: employee.* matches NO prefix case, so it lands in default `event`; assert that).
- `chat.response` -> the function must never return chat_message for it (assert whatever today's behavior is - likely falls to default `event`; PRESERVE whatever you measure, and note it).
- Marker-proof case: a BusMessage on an UNKNOWN topic name (no prefix match, e.g. "future.topic") whose payload decodes as TurnTerminalEvent -> `agent_progress` VIA MARKER. This is the case that FAILS before your change (pre-change it hits default `event`) and passes after. This is the RED assertion that proves the marker path works.

**Step 2: Run test to verify failure**

`go test -p 2 -short ./internal/comm/http/ -run TestWSClass -v` - the marker-proof case FAILS, all parity cases PASS. If a parity case fails pre-change, your expected value is wrong - fix the test's expectation to measured behavior FIRST (parity means matching today, not matching hopes).

**Step 3: Write implementation**

- Small decode table (package-level in server.go or a new server_wsclass.go in comm/http):
  ```go
  // typedPayloadDecoders maps topic names to decoders for payloads with
  // WSClass markers. Entries exist only for topics migrated to Topic[T]
  // (currently: turn.terminal). Legacy fallback still classifies any
  // topic missing here.
  var typedPayloadDecoders = map[string]func(json.RawMessage) (wsclass.WSClassified, error){
      agent.TopicTurnTerminal.Name: decodeTurnTerminal, // unmarshal into agent.TurnTerminalEvent
  }
  ```
- In transformBusEventToWS after the existing map decode: if a decoder exists for msg.Topic, try it; success -> `eventType = typed.WSClass().wsString()` (add a small method mapping WSClass to its wire string: chat_message/agent_progress/metrics_update/job_update/plan_update/event); failure -> continue to the prefix switch.
- Add the legacy-fallback comment above the existing switch: `// legacy fallback - remove when all WS-visible topics are typed (see internal/comm/wsclass).`
- Use a `switch` statement keyed on WSClass for the wire-string mapping (leaf 04's exhaustive lint must be able to check it). NEVER if/else chains.

**Step 4: Run test to verify pass**

All cases green. Full package: `go test -p 2 -short ./internal/comm/http/`. Then `go test -p 2 -short ./internal/comm/... ./internal/agent/... ./internal/tui/...`.

### Task 3: Parity fence + report

**Objective:** Enumerate every prefix-table topic with a raw map payload and assert classification is byte-identical to the pre-change table.

**Files:**
- Test: same test file as Task 2

**Step 1: Write the parity test**

Iterate representative topics for EVERY prefix case in the pre-change switch: chat_message, chat.message.received, chat.progress (chat.* case), agent.quota_wait (agent.quota case), agent.model_escalated, turn.terminal (turn. case), metrics.cpu (metrics. case), task.completed / step.started / job.done / queue.updated (job_update case), plan.approved (plan. case), employee.notify + unknown.topic (default event case). Each with a minimal `{"k":"v"}` map payload. Assert exact event-type strings. This table is the regression fence - derive expected values from the PRE-CHANGE switch (measure first if unsure; the values above are from the verified switch but confirm employee.* and any case you are unsure about by checking out the pre-change function via git show HEAD:internal/comm/http/server.go if needed).

**Step 2: Run to verify pass**

`go test -p 2 -short ./internal/comm/http/ -run TestWSClassParity -v` - PASS. A failure here means Task 2 changed behavior: fix Task 2, never the table.

**Step 3: Comments + report**

- Legacy-fallback comment present above the switch (from Task 2).
- Report lists: topics classified via marker today (turn.terminal), topics on fallback and why (map-shaped payloads: quota_wait trio, chat lifecycle, metrics, task/step/queue/job, plan, employee, everything unprefixed).

**Step 4: Full verify**

`go build ./... && go test -p 2 -short ./internal/...` green. gofmt clean.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] wsclass package: six constants + interface; imports stdlib only
- [ ] Marker method beside TurnTerminalEvent in internal/agent; agent imports wsclass (one direction)
- [ ] Classification order: chat.response exclusion -> marker (typed decode table) -> prefix fallback -> default event
- [ ] Wire-string mapping is a switch over WSClass (exhaustive-checkable)
- [ ] Parity test covers every prefix case + default; passes
- [ ] Marker-proof case (unknown topic, typed payload) passes - the RED-first proof
- [ ] No structs invented for map-shaped topics; fallback list in report
- [ ] chat_message normalization block untouched (diff confirms)
- [ ] `go build ./...` + `go test -p 2 -short ./internal/...` green; gofmt clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Marker path demonstrably drives turn.terminal classification (marker-proof test)
- [ ] Prefix table retained as fallback with legacy comment
- [ ] All six event-type strings produced correctly; default is `event`
- [ ] typedPayloadDecoders table has exactly one entry (turn.terminal)
- [ ] No import cycle: wsclass <- agent only; comm/http imports agent (check this is already the case or acceptable - server.go is comm/http; if comm/http importing agent is NEW, flag it in the report for orchestrator review; the decode table alternatively lives behind a registration seam if the import is unacceptable)
- [ ] Parity table enumerates the full prefix set

Output: APPROVED or specific gaps with file + line references.

## Notes

- The Flutter client creates a bubble for every `chat_message` event - the parity fence is what keeps lifecycle events out of that bucket. Any parity failure is a REAL bug in your migration; never adjust expected values to match broken output.
- The comm/http importing internal/agent question: server.go currently imports internal/bus and pkg/models. Adding an agent import for one decoder entry may be undesirable layering. Acceptable alternative: define the decoder registration as a seam - wsclass or comm/http exposes `RegisterTypedDecoder(topic string, fn ...)` and the agent package (or daemon wiring) registers the turn.terminal decoder at startup. Prefer the direct map if a comm/http->agent import already exists or is clearly harmless; prefer the registration seam if not. Report which you chose and why.
- leaf 04 builds on the switch you write here - keep it a `switch` over WSClass values.
