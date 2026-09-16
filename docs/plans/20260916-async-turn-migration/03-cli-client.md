# CLI Client Async Migration - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** `meept chat` (oneshot mode) submits via chat.submit and awaits the real terminal result — a >110s task prints its actual result, never the stub.
- **Dependencies:** 01-async-rpc-mode.md
- **Estimated Context:** ~40K
- **Concurrency Group:** B

## Goal

Today `meept chat "do a long task"` prints "Task X is still running" when
the 110s sync wait fires, and the user never sees the real result in that
invocation. This leaf makes oneshot chat print the genuine final reply by
submitting async and awaiting the terminal event, with a live progress line.
Interactive TUI mode is leaf 04's scope; this leaf is the non-TUI path
(runChat → oneshot_responses and the plain socket client path).

## Context

cmd/meept/chat.go: `runChat` (line 57) dispatches to the interactive TUI or
oneshot mode (`chatWithSession` at line 351 for session chats; the oneshot
responses path around it). The CLI talks to the daemon through the
transport client (internal/transport — the same socket the TUI's
internal/tui/rpc.go wraps with `Call("chat", params)` at rpc.go:290).

The CLI can subscribe to bus topics over RPC: the TUI does exactly this
(internal/tui/events.go `bus.subscribe` + poll — reuse that pattern; the
CLI's transport exposes the same methods).

Key files:
- cmd/meept/chat.go — runChat, chatWithSession, oneshot response printing
- internal/tui/events.go — bus.subscribe + poll pattern to mirror
- internal/transport — the Client interface (Call + bus subscribe methods)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// cmd/meept/chat.go (or a new cmd/meept/chat_async.go helper):
//
// type chatTurnResult struct {
//     Reply      string
//     Status     string // completed|failed|timeout|parked
//     Error      string
//     AckSeconds float64
//     TurnSeconds float64
// }
//
// // submitAndAwait sends chat.submit, awaits turn.terminal on the CLI's
// // bus subscription, renders a single-line progress indicator while
// // waiting (respects --quiet / non-TTY stdout: no spinner, just silent
// // wait), and returns the result. LivenessTimeout default 120s → error
// // "turn stalled (no progress for 120s); task may still complete —
// // check `meept tasks`".
// func submitAndAwait(ctx, client transport.Client, msg, sessionID string, opts chatOpts) (chatTurnResult, error)
//
// Flag: `--await` on newChatCmd: "wait" (default) | "off".
// off = print the ack line ("turn <id> accepted; run `meept chat --session
// <id>` to follow up") and exit 0.
```

### What This Leaf Consumes

```
// Daemon: "chat.submit" RPC (master Contract 1 ack), "turn.terminal" topic
// (Plan 1 payload), "bus.subscribe"/poll (existing CLI transport methods —
// mirror internal/tui/events.go usage).
```

## Tasks

### Task 1: submitAndAwait helper

**Objective:** The submit+await loop with liveness and progress rendering.

**Files:**
- Create: `cmd/meept/chat_async.go`
- Test: `cmd/meept/chat_async_test.go` (the CLI package has tests — follow
  its existing fakes for transport.Client; if none exist for bus subscribe,
  write a minimal fake implementing the used methods)

**Step 1: Failing tests**

- Happy: fake transport returns ack JSON from "chat.submit" and a
  turn.terminal event from poll → result Reply/Status correct, timings >0.
- Wait-for-it ordering: subscribe MUST be established before submit (assert
  call order on the fake) — no event-gap.
- Liveness: no events → error mentions "stalled" and suggests `meept tasks`.
- status=failed → result carries Error; exit-path handles it.
- --await off → no submit-await; prints ack line; returns nil error.
- Non-TTY stdout (test sets a bytes.Buffer writer): no spinner frames in
  output.

**Steps 2-4:** standard cycle. Progress rendering: a single updating line
("\rturn <id> · 12s") when stdout is a TTY (term detection: reuse whatever
the repo already uses; if none, check `os.Stdout.Stat()` mode char pipe —
std-only), nothing when piped or --quiet.

### Task 2: Wire runChat oneshot path

**Objective:** The oneshot chat path calls submitAndAwait; prints Reply.

**Files:**
- Modify: `cmd/meept/chat.go` (runChat oneshot branch + chatWithSession's
  non-TUI call path — whichever path currently prints the reply for a
  single message)
- Modify: `cmd/meept/chat.go` — add the `--await` flag to newChatCmd
- Test: `cmd/meept/chat_test.go` (extend)

**Step 1: Failing tests**

- Oneshot chat with a fake transport scripting a completed turn → stdout
  contains the reply text, exit nil.
- Failed turn → stderr/stdout carries the error text, exit non-zero (match
  the CLI's existing error-exit convention).
- --await off → stdout contains the ack line, no reply wait.

**Steps 2-4:** standard cycle. Keep the TUI branch untouched (leaf 04).

### Task 3: session chat path parity

**Objective:** chatWithSession (used by `--session`) gains the same
submit+await, preserving its session semantics.

**Files:**
- Modify: `cmd/meept/chat.go` (chatWithSession)
- Test: extend chat_test.go

**Steps 1-4:** mirror Task 2 with session_id plumbed into chat.submit
params; conversation_id omitted (daemon generates).

## Self-Verification Checklist

- [ ] All tasks implemented; `go build ./...` clean; gofmt/vet clean
- [ ] Subscribe-before-submit ordering proven by test
- [ ] Liveness error actionable (mentions `meept tasks`)
- [ ] --await off works; ack line exact format documented in code comment
- [ ] A >110s scripted task returns the real reply (test with a fake that
      delays the terminal event 200ms past the OLD 110s semantic — trivially
      exceeded; the point is no 110s ceiling exists in the new path)
- [ ] No spinner output on piped stdout; tests green under -race

**DO NOT COMMIT.** Orchestrator commits after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Tasks + tests present/passing (-race)
- [ ] Contract 4 loop shape honored (subscribe→submit→progress→terminal)
- [ ] Interactive TUI branch untouched (leaf 04's scope)
- [ ] Error/exit conventions match existing CLI behavior
- [ ] No scope creep: no TUI changes, no daemon changes

Output: APPROVED or specific gaps with file+line.

## Notes

- The CLI's transport may not expose bus.subscribe today outside the TUI
  package — if the method lives on a TUI-specific wrapper, extract the
  minimal call (bus.subscribe + bus.poll RPC methods) into the CLI path
  rather than importing internal/tui (import direction: cmd/meept already
  imports internal/transport; prefer transport-level methods; if subscribe
  is missing on transport.Client, ADD it to the transport client following
  its existing RPC plumbing — that is in-scope).
- Keep `meept chat` default behavior user-identical for fast turns; the
  visible change only appears on long tasks (real result instead of stub).
