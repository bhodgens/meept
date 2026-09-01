# Session Interactivity Signal - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** IsInteractive helper (recent user message + foreground
  flag), Foreground session field, queue.interactive_window config.
- **Dependencies:** none
- **Estimated Context:** 50K
- **Concurrency Group:** A
- **Decision references:** D11, Q1

## Goal

One pure function answers "is this session interactive right now?":

```go
IsInteractive(s *Session, now time.Time, window time.Duration) bool
```

- TRUE when the session's LAST USER MESSAGE is within `window` of now.
  AUDIT FACT (2026-09-01): `Session.LastActivity` is the WRONG source —
  its only writers are client-attach (session.go:413 Attach),
  resume/set-project/rename store paths (store_sqlite.go:1403-1457), and
  debug-manager touches; NO chat/message path writes it. Reading it would
  mark a session interactive because a user RESUMED it or re-pointed its
  project. Use instead, in this order of preference:
  1. a NEW dedicated last-user-message field on Session, written from the
     chat path (Task 1's escape hatch — now the LIKELY path), or
  2. `thread.LastActivityAt` (thread.go:57 Touch(), called from
     internal/agent/thread_router.go:93 on message routing) — message-
     proximate but thread-scoped (per-thread, not per-session; a session
     with N threads needs a max over its threads), or
  3. `internal/session/activity_tracker.go:63` `HasRecentActivity(
     sessionID, window)` — ALREADY implements D11's windowed query, but
     audit WHAT feeds it first (ActivityTracker.Update, activity_tracker.go:41)
     before trusting it as a user-message proxy.
  Task 1 evaluates all three against the write-path evidence and picks
  ONE; document the choice. Do NOT ship IsInteractive reading
  Session.LastActivity.
- TRUE when s.Foreground is set (client-declared foreground session —
  TUI/GUI set it when the user has the session open and focused).
- FALSE otherwise. nil session → false.

Plus: `Foreground bool` field on Session (json omitempty, default
false), settable via the session API the clients already use for other
session fields, and the `queue.interactive_window` config knob.

## Context

Key files:
- `internal/session/session.go:65-103` — Session struct; LastActivity
  (line 71), AttachedClients (72). AUDIT FACT: LastActivity's writers
  are Attach (session.go:413), store mutations (store_sqlite.go:1403-1457),
  and internal/debug touches — NOT user messages. See the corrected Goal
  sources list; Task 1's investigation starts from these write paths.
- `internal/session/thread.go:40-60` — Thread.LastActivityAt + Touch()
  (thread_router.go:93 calls it on message routing): the message-
  proximate timestamp that exists today.
- `internal/session/activity_tracker.go` — ActivityTracker with
  HasRecentActivity(sessionID, window) (:63) — the windowed query D11
  describes, already built; audit its feed before trusting it.
- `internal/session/store.go` — persistence pattern for new session
  fields (check how Archived/NoFence persist — column? JSON blob?).
  Follow the SAME mechanism for Foreground.
- `internal/config/schema.go` — queue config block (grep
  `QueueConfig` / queue-related structs) for interactive_window
  (duration string; default "5m" per Q1).
- Session HTTP/TUI surface: find where clients set session fields
  (rename/archive endpoints) — Foreground needs one setter route or RPC
  closure. Reuse the closest existing pattern (archive toggle is the
  model).

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 1:

```go
// internal/session/interactive.go
func IsInteractive(s *Session, now time.Time, window time.Duration) bool
// Session (modify):
Foreground bool `json:"foreground,omitempty"`
// Config (modify):
// QueueConfig.InteractiveWindow (duration; default 5m) — name the field
// InteractiveWindow with json "interactive_window" / toml "interactive_window"
```

### What This Leaf Consumes

Nothing new — existing Session, config.

## Tasks

### Task 1: LastActivity semantics investigation + IsInteractive

**Objective:** The helper, with an evidence-based activity source.

**Files:**
- Create: `internal/session/interactive.go`
- Test: `internal/session/interactive_test.go`

**Step 1:** Grep where LastActivity is written. Table tests (injected
now): within window → true; outside → false; window<=0 → only
Foreground counts; nil session → false; Foreground forces true
regardless of age. **Step 2:** FAIL. **Step 3:** Implement the pure
function. **Step 4:** PASS. Record the LastActivity finding in a
comment: "user messages update LastActivity via X; other writes via Y"
— if non-user writes dominate, ADD a lastUserMessageAt field instead
(persisted like Archived) and use THAT; document either way.

### Task 2: Foreground field + persistence + setter

**Objective:** Clients can flag a session; it persists.

**Files:**
- Modify: `internal/session/session.go` (+ store persistence if
  column-based)
- Modify: the session mutation surface (HTTP route or RPC closure —
  investigate archive/rename for the pattern)
- Test: `internal/session/store_test.go` extension (persist + load
  round-trip); surface test per the pattern found

**Step 1:** Failing round-trip test: set Foreground → store → load →
still true; default false. **Step 2:** FAIL. **Step 3:** Implement.
**Step 4:** PASS + full `go test ./internal/session/... -count=1`.

### Task 3: Config knob

**Objective:** queue.interactive_window with 5m default.

**Files:**
- Modify: `internal/config/schema.go` + defaults site
- Modify: `docs/configuration/` queue/config page
- Test: `internal/config/schema_test.go` (default + override parse)

**Verify:** default "5m"; JSON5 round-trip; docs name the knob.

## Self-Verification Checklist

- [ ] IsInteractive pure, clock-injected, nil-safe (tested)
- [ ] Activity-source decision recorded in code comment (LastActivity vs new field)
- [ ] Foreground persists; default false; setter follows an existing route pattern
- [ ] Config default 5m + docs
- [ ] Multi-user: no OwnerID filtering changes
- [ ] gofmt/vet clean; package suites green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract matches master Contract 1 exactly
- [ ] No behavior change when nothing sets Foreground
- [ ] Docs updated

Output: APPROVED or specific gaps with file + line references.

## Notes

- WS presence was explicitly NOT ratified (D11) — do not add connection
  tracking.
- The setter surface: if session mutation goes through an RPC
  register-handler closure (epistemic/memory pattern) rather than HTTP,
  use that; whichever path the archive toggle uses is correct.
