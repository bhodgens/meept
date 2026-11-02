# Employee phone binding - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit. Do NOT use read_file
> on existing source files - explore with search_files or terminal cat.
> Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** Call session store (persistent phone session per number+employee), goal-round parking for call lifetime, progress events per prompt/say, day-brief injection into the employee turn, CallTranscriptSource implementation for leaf 04
- **Dependencies:** leaf 04 (DayBrief), leaf 05 (Manager release hook)
- **Estimated Context:** 85K
- **Concurrency Group:** C
- **Files this leaf owns:** internal/voice/binding.go, internal/voice/callstore.go, internal/daemon/voice_employee.go

## Goal

Make a call an employee conversation with memory. One meept session per
(number, employee), bound via sessions.employee_id, so inbound and
outbound calls append to the same session and a call carries the prior
call. The employee goal round parks for the call's life and unparks on ANY
terminal outcome. Every final prompt and every allowed say publishes a
progress event (observability + turn liveness for attached paths).

## Context

Call session store (internal/voice/callstore.go, SQLite at
$MEEPT_HOME/voice/calls.db): rows = call_id, number, employee_id,
direction, session_id, state, started_at, ended_at, outcome, transcript
(JSON array of {role,text,at}). Migration idiom: guarded ALTER like the
other stores. This store implements leaf 04's CallTranscriptSource
(TodayTranscripts = rows for number+local-day, Summary = first user line +
outcome, capped).
Meept session binding: session.Store SetEmployeeID exists
(sessions.employee_id column, agents.employees.bind_session precedent -
search internal/session for SetEmployeeID). Create-or-resolve the session
per (number, employee): lookup by a stable key (session name
"voice:<employee>:<number>" or a new column - prefer lookup-by-name via
existing store queries; do NOT add a session column unless name lookup is
impossible; document the choice).
Employee turn path: NOT ExecuteBot (spec non-goal: it mints bot-exec-* ids
and drops context). Use the AgentLoop with the resolved session's
conversation id so messages persist to the phone session:
loop.RunOnce(ctx, userMessage, conversationID) with the session store
wired (ChatHandler.persistExchange precedent - search internal/agent for
how RunOnce results persist; if RunOnce does not persist, use the same
SessionMessageSaver seam). System-prompt injection of DayBrief.PromptBlock()
via the loop's AddSection or a per-call prompt prefix (choose the seam
that does NOT mutate shared loop state - the loop is cloned per session
via Manager.GetOrCreateWired; verify before writing).
Goal parking: employee GoalLoop park machinery (internal/employee/park.go -
Steer/TriggerEvent Source shapes). The call holds the invoke mutex for its
life (Manager.triggerViaGoalLoop precedent: acquire before Decide). For
voice: the phone employee's call IS the round - parking means the round
does not return until release. Implement as: round entry acquires, call
terminal outcome (release hook from leaf 05) releases. A no-answer dial
releases within timeout_secs (leaf 11 timer).
Progress events: bus topic voice.call.progress (typed bus.Topic[T],
payload {call_id, employee_id, kind: prompt|say|interrupt, at}). Published
per final prompt and per allowed say. WS class: "event" (never
chat_message); no GUI relay subscription in this tree.

## Verified Anchors

- internal/session/store_sqlite.go - SetEmployeeID (search before use)
- internal/employee/park.go - park/resume machinery (read before designing the hold)
- internal/employee/manager.go - triggerViaGoalLoop invoke-mutex pattern
- internal/daemon/employee_service_adapter.go:293 - ExecuteBot (the ANTI-pattern; do not use)
- internal/agent/topics.go - typed Topic[T] precedent
- internal/voice/daybrief.go (leaf 04) - CallTranscriptSource interface

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/voice/callstore.go
type CallStore struct{ ... }
func NewCallStore(dbPath string, logger *slog.Logger) (*CallStore, error)
func (s *CallStore) Start(ctx context.Context, rec CallRecord) error
func (s *CallStore) End(ctx context.Context, callID string, outcome string) error
func (s *CallStore) AppendTranscript(ctx context.Context, callID string, line TranscriptLine) error
func (s *CallStore) TodayTranscripts(ctx context.Context, number string) ([]CallTranscript, error) // satisfies leaf 04
func (s *CallStore) SessionIDFor(number, employeeID string) (string, bool)
// internal/voice/binding.go
type Binding struct{ ... } // per-call: session resolve, brief inject, turn runner, progress publish
func NewBinding(deps BindingDeps) *Binding
func (b *Binding) OnCallStart(ctx context.Context, ccid, number, employeeID, direction string, brief DayBrief) (sessionID string, err error)
func (b *Binding) RunTurn(ctx context.Context, ccid, text string) (reply string, err error) // final prompt -> employee turn
func (b *Binding) OnCallEnd(ctx context.Context, ccid, outcome string) error
// internal/daemon/voice_employee.go
func WireVoiceEmployee(deps ...) *voice.Binding  // daemon-side construction; injects session store, loop manager, bus, goal loop hold
```

### What This Leaf Consumes

- C7 DayBrief (leaf 04); C5 release hook (leaf 05); session.Store, agent loop manager, bus (existing)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | CallStore: schema, Start/End/AppendTranscript roundtrip; TodayTranscripts local-day filter (TZ differential) | Create: internal/voice/callstore.go, callstore_test.go | go test -p 2 ./internal/voice/ -run TestCallStore | - | PASS | Given call yesterday + call today; When TodayTranscripts; Then one row (both TZ zones) |
| T2 | Session resolve: create-or-reuse per (number, employee); employee_id bound; same session across inbound then outbound | Create: internal/voice/binding.go, binding_test.go | go test -p 2 ./internal/voice/ -run TestBindingSession | - | PASS | Given prior call session; When new call same number+employee; Then same session_id reused |
| T3 | RunTurn: brief injected once at call start (not per turn), user text -> loop turn with session conversation id, reply persisted to session messages | (same) | go test -p 2 ./internal/voice/ -run TestBindingTurn | - | PASS | Given fake chatter; When RunTurn; Then session_messages gains user+assistant rows |
| T4 | Progress events: voice.call.progress published per final prompt + per allowed say; typed topic; payload shape pinned | (same) | go test -p 2 ./internal/voice/ -run TestBindingProgress | - | PASS | Given one prompt + one say; When call runs; Then exactly 2 progress events |
| T5 | Goal hold/release: round held while call live; release hook (hangup/no_answer/failed) releases exactly once; paused employee mid-call skips post-call reflect (existing pause semantics) | Create: internal/daemon/voice_employee.go, voice_employee_test.go | go test -p 2 ./internal/daemon/ -run TestVoiceEmployeeHold | - | PASS | Given live call; When hangup; Then hold released once, employee idle |
| T6 | Transcript persisted per turn (role/text/at); End writes outcome | Modify: binding.go | go test -p 2 ./internal/voice/ -run TestBindingTranscript | - | PASS | Given 3-turn call; When ended; Then 6 transcript lines + outcome |
| T7 | Format/vet/race | - | gofmt -l internal/voice internal/daemon; go vet ./internal/voice/ ./internal/daemon/; go test -p 2 -race ./internal/voice/ | - | clean | - |

## Self-Verification Checklist

- [ ] ExecuteBot NOT used (grep before reporting)
- [ ] No shared-loop mutation: brief injection goes through a per-call clone/section, verified against GetOrCreateWired semantics
- [ ] Hold released on EVERY terminal outcome including streaming.failed
- [ ] progress topic typed; never chat_message class
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list (session lookup choice documented)

## Review Checklist (For Review Agent)

- [ ] Session reuse proven across directions (inbound then outbound same number)
- [ ] Registry/session-id defect class respected: resolve BOTH session_id and conversation_id keys where lookups occur
- [ ] No deadlock: hold acquire + release hook ordering under -race with concurrent hangup
- [ ] callstore migrations guarded-ALTER idiom

Output: APPROVED or specific gaps.

## Notes

The invoke-mutex hold for a 15-minute call blocks that employee's other
triggers BY DESIGN (one live call per employee). Scheduled assesses
hitting a held employee must skip with DecideSkip, not queue up - verify
the skip path exists (LastDecideSkip precedent) and log it.
