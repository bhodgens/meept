# Generic Topic[T] Declarations - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Add `Topic[T]` declaration type plus generic `PublishT`/`SubscribeT` wrappers to `internal/bus`, with round-trip and raw-interop tests.
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

Introduce the generic typed-topic layer in `internal/bus/topic.go`. This leaf adds ONLY the mechanism - no call sites change, no topics migrate. The existing raw API (`b.Publish`, `b.Subscribe`) is untouched so all 136 publish sites and 41 subscribe sites keep compiling unchanged.

## Context

Meept's message bus is `internal/bus/bus.go`: `MessageBus` holds `map[string][]*Subscriber`; `Publish(topic string, msg *models.BusMessage) int` fans out by topic name; `Subscribe(id, topic string) *Subscriber` supports wildcards (`"agent.*"`). Messages are `pkg/models.BusMessage{... Payload json.RawMessage}` created via `NewBusMessage(msgType, source, payload any)` which marshals `payload` with `json.Marshal`.

Publishers currently do: `msg, _ := models.NewBusMessage(models.MessageTypeEvent, "agent", somePayloadStruct); b.Publish("turn.terminal", msg)`. Subscribers read the channel and `json.Unmarshal(msg.Payload, &target)`. Nothing binds the topic string to the payload type - that is the hole this leaf closes mechanically.

Key files to understand before implementing:

- `internal/bus/bus.go` - MessageBus, Publish, PublishBlocking, PublishExternalOnly, Subscribe, Subscriber, wildcard matching
- `internal/bus/bus_test.go` - existing test style to match
- `pkg/models/types.go` - BusMessage, MessageType constants, NewBusMessage

Check the module name in `go.mod` first (expected `meept`) and use the correct import path in tests.

## Interface Contracts (From Parent)

### What This Leaf Exposes

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

// PublishT marshals payload as T and publishes to t's topic.
func PublishT[T any](b *MessageBus, t Topic[T], source string, payload T) int

// SubscribeT subscribes to t and invokes fn with each decoded payload.
// Decode failures are logged (topic + type name) and dropped.
func SubscribeT[T any](b *MessageBus, id string, t Topic[T], fn func(T)) *Subscriber
```

- Package-level generic FUNCTIONS taking `*MessageBus` as first arg - NOT methods (Go methods cannot add type parameters). Do not attempt `func (b *MessageBus) PublishT[T any](...)`.
- The decision from this leaf's Task 3 (error-returning variant vs panic-on-marshal-failure) becomes part of the exported contract; document the final choice in the doc comment of `PublishT`.

### What This Leaf Consumes

```go
// From internal/bus/bus.go (existing, unchanged):
func (b *MessageBus) Publish(topic string, msg *models.BusMessage) int
func (b *MessageBus) Subscribe(id, topic string) *Subscriber
// From pkg/models (existing):
models.NewBusMessage(msgType MessageType, source string, payload any) (*BusMessage, error)
```

## Tasks

### Task 1: Topic declaration type + NewTopic

**Objective:** Add `Topic[T]` and `NewTopic[T]` with doc comments.

**Files:**
- Create: `internal/bus/topic.go`
- Test: `internal/bus/topic_test.go`

**Step 1: Write failing test**

```go
func TestNewTopic_DeclaresName(t *testing.T) {
    tp := NewTopic[TurnTerminalTestPayload]("turn.terminal")
    if tp.Name != "turn.terminal" {
        t.Fatalf("Topic.Name = %q, want %q", tp.Name, "turn.terminal")
    }
}
```

(Define `TurnTerminalTestPayload` in the test file - a small struct with one or two fields, json-tagged.)

**Step 2: Run test to verify failure**

Run: `go test -p 2 -short ./internal/bus/ -run TestNewTopic_DeclaresName -v`
Expected: FAIL - `undefined: NewTopic`

**Step 3: Write minimal implementation**

`internal/bus/topic.go` with `Topic[T]` struct and `NewTopic` exactly per the contract.

**Step 4: Run test to verify pass**

Same command. Expected: PASS.

### Task 2: SubscribeT wrapper

**Objective:** Generic subscribe that decodes payloads into T and invokes fn.

**Files:**
- Modify: `internal/bus/topic.go`
- Test: `internal/bus/topic_test.go`

**Step 1: Write failing test**

```go
func TestSubscribeT_DecodesPayload(t *testing.T) {
    b := NewMessageBus(...) // match however bus_test.go constructs one
    defer b.Close()          // match existing teardown in bus_test.go

    got := make(chan TurnTerminalTestPayload, 1)
    sub := SubscribeT(b, "test-sub", NewTopic[TurnTerminalTestPayload]("turn.terminal"), func(p TurnTerminalTestPayload) { got <- p })
    defer b.Unsubscribe(sub)

    PublishT(b, NewTopic[TurnTerminalTestPayload]("turn.terminal"), "test", TurnTerminalTestPayload{TurnID: "t1", Status: "ok"})

    select {
    case p := <-got:
        if p.TurnID != "t1" || p.Status != "ok" {
            t.Fatalf("decoded payload = %+v, want TurnID=t1 Status=ok", p)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("timed out waiting for typed delivery")
    }
}

func TestSubscribeT_DropsUndecodablePayload(t *testing.T) {
    // Publish a RAW BusMessage to the same topic name whose Payload is
    // invalid for T (e.g. json of the wrong shape). Subscriber fn must
    // NOT be invoked; nothing panics; test completes.
}
```

**Step 2: Run test to verify failure**

Run: `go test -p 2 -short ./internal/bus/ -run TestSubscribeT -v`
Expected: FAIL - `undefined: SubscribeT`

**Step 3: Write minimal implementation**

`SubscribeT[T]`:
- `sub := b.Subscribe(id, t.Name)`
- If `sub` is the closed-subscriber sentinel bus.Subscribe can return (check bus.go for the closed-bus branch), return it immediately.
- Start exactly ONE goroutine that reads `sub.Channel` and for each `*models.BusMessage` does `json.Unmarshal(m.Payload, &payload)`; on success calls `fn(payload)`; on failure logs at warn/error with topic and type name and continues. How to log: match the repo's existing logging idiom - search `internal/` for how bus-adjacent packages log (there may be a `log` package or structured logger; do NOT introduce a new logging dependency; if the bus package itself has no logger today, emit via the pattern bus.go already uses, else use the standard `log` package and say so in your report).
- Document in the doc comment: this goroutine runs until the subscriber is unsubscribed/closed; callers must Unsubscribe.

**Step 4: Run test to verify pass**

Same command. Expected: PASS. Also run the whole package: `go test -p 2 -short ./internal/bus/` - existing bus tests must stay green.

### Task 3: PublishT wrapper + marshal-failure decision

**Objective:** Generic publish; make and document the marshal-failure behavior decision.

**Files:**
- Modify: `internal/bus/topic.go`
- Test: `internal/bus/topic_test.go`

**Decision to make (record in code + report):** `json.Marshal` of T fails only for channels/funcs/cycles - programmer error. Two options:
- (a) return `int` like raw Publish, and internally `panic` on marshal error (loud, matches "programmer error" semantics, keeps call sites one-line);
- (b) return `(int, error)` and force every call site to handle it.

Choose (a) - panic on marshal failure with a wrapped, descriptive message - UNLESS you find existing repo precedent that panics in bus-adjacent code are prohibited. Search `internal/` for panic usage precedent and honor what you find. Document the choice in the `PublishT` doc comment either way.

**Step 1: Write failing test**

```go
func TestPublishT_RoundTrip(t *testing.T) {
    // PublishT then SubscribeT delivery yields a deep-equal payload.
    // Use reflect.DeepEqual on the test payload struct.
}

func TestPublishT_InteropsWithRawPublish(t *testing.T) {
    // Typed SUBSCRIBER receives a RAW publish: construct
    // models.NewBusMessage(models.MessageTypeEvent, "raw", testPayload),
    // b.Publish(topicName, msg), assert SubscribeT fn receives decoded T.
    // And the reverse: typed publish received by a RAW subscriber whose
    // test code unmarshals Sub.Channel message Payload into T.
}

func TestPublishT_PanicsOnUnmarshalablePayload(t *testing.T) {
    // Payload struct containing a chan field. defer recover() asserts
    // panic occurs (if decision (a)) or error returned (if (b)).
}
```

**Step 2: Run test to verify failure**

Run: `go test -p 2 -short ./internal/bus/ -run TestPublishT -v`
Expected: FAIL - `undefined: PublishT`

**Step 3: Write minimal implementation**

`PublishT[T]`: marshal payload; on error behave per the decision above; on success `models.NewBusMessage(models.MessageTypeEvent, source, payload)` (or construct the BusMessage literal directly with the marshaled bytes - either is fine; match what avoids double-marshal) then `return b.Publish(t.Name, msg)`.

Avoid double-marshal: `NewBusMessage` marshals `payload any` internally. If you pass the typed payload to NewBusMessage, T is marshaled once by NewBusMessage and your error handling wraps that call - fine. Do NOT marshal twice.

**Step 4: Run test to verify pass**

Same command. Expected: PASS. Full package green: `go test -p 2 -short ./internal/bus/`.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing: `go test -p 2 -short ./internal/bus/` green
- [ ] `go build ./internal/...` compiles - no existing call sites broken
- [ ] Interface contracts satisfied exactly: Topic[T], NewTopic, PublishT, SubscribeT at internal/bus/topic.go with package-level generic functions
- [ ] Raw bus API untouched: `git diff --stat` shows only internal/bus/topic.go + internal/bus/topic_test.go (new files)
- [ ] No scope creep - no topics migrated, no call sites changed
- [ ] gofmt clean on both new files
- [ ] Marshal-failure decision documented in PublishT doc comment and reported

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Contracts match exactly: package-level generic functions, first param *MessageBus, Topic[T] struct with Name string
- [ ] Raw Publish/Subscribe path unchanged (zero diffs in bus.go, types.go)
- [ ] No debug artifacts, no TODOs, no placeholder values
- [ ] Round-trip and raw-interop tests actually assert payload equality (not just delivery)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The `SubscribeT` reader goroutine leaks if callers never Unsubscribe - same lifetime discipline as raw Subscribe channels. Document it; do not build a finalizer.
- Wildcards: SubscribeT takes a Topic[T] with an exact name - wildcard typing is explicitly out of scope (see master Architecture). Do not add pattern support.
- Go version: generics require 1.18+; check go.mod.
