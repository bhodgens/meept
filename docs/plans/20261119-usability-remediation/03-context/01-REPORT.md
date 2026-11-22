# 01-session-history -- Completion Report

Leaf: docs/plans/20261119-usability-remediation/03-context/01-session-history.md
Worktree: .hermes/worktrees/usability-remediation. NOT committed, NOT git-added.
Gate tests (committed 89e996e2) untouched: chat_submit_validate_test.go (T1),
handler_history_test.go (T2), messages_toolcalls_test.go (T3).
Finisher pass: prior child (9 writes) died on upstream 401 after landing the
T1 validation + the T2 ensurer seam; this pass completed T2's wiring and all
of T3, then verified every gate.

## 1. Files touched

T1 (prior child, verified GREEN by this pass):
- internal/rpc/chat_submit.go -- submit-time identity validation: unknown
  session_id / conversation_id -> accepted:false + actionable note (never a
  phantom session); thread-scoped ids unwrapped via
  session.ResolveThreadConversationID before validation; both-ids pair
  membership-checked and corrected to the owning conversation. Nil-store
  path keeps the legacy resolution/mint shape byte-for-byte.
- internal/agent/handler.go -- resolution twin of the same contract.

T2 (sessions-row upsert, FK 787 fix):
- internal/session/store_upsert.go (new file, prior child) -- added
  ensureSessionRowInTx(tx, sessionID): probe + INSERT ... ON CONFLICT DO
  NOTHING inside the CALLER's open transaction. Existing rows untouched
  (never widens a project binding); new rows UNBOUND, keyed by the id the
  conversation-only submit persists messages under.
- internal/session/store_sqlite.go -- SaveMessages and
  SaveMessagesWithToolCalls each call ensureSessionRowInTx right after
  Begin(), under s.mu: the race-safe guarantee lands at the store level
  (s.mu serializes writers on one sqlite connection; concurrent same-conv
  submits produce exactly ONE sessions row).
- internal/agent/handler.go persistExchange -- wires the prior child's
  declared-but-unreferenced sessionRowEnsurer capability:
  EnsureSessionRow(sessionID) runs BEFORE any message write (the C4 "upsert
  before persistExchange" order), best-effort Debug-log on failure; savers
  without the capability keep the legacy path.

T3 (include_tool_calls read surface):
- internal/session/store.go -- Message gains additive
  ToolCalls []MessageToolCall `json:"tool_calls,omitempty"` (nil by default
  -> legacy bytes unchanged) + new MessageToolCall wire type
  {id, name, args_summary, result_summary, at} exactly per C4.
- internal/session/session.go handleGetMessages -- parses
  include_tool_calls (default false); when true, joinToolCalls batch-reads
  GetToolCallsForMessages for the page's message ids and attaches each row
  its own calls (user rows without calls keep no tool_calls key). Summaries
  bounded 200 runes (truncateRunes, rune-safe; same discipline as
  dispatchInputSummaryLimit). "at" is the owning message's RFC3339
  timestamp -- session_tool_calls carries no own time column and the
  additive-surface scope kept the schema untouched. A join error logs and
  degrades to the rows WITHOUT the surface; it never fails a read that
  worked. No other handler touched: absent param and explicit false take
  the identical legacy path.

Test fixture updated (NOT a gate; pre-existing green test made stale by T1):
- internal/daemon/http_only_submit_test.go (F17 pin) -- its Part-2 live-ack
  probe submitted a fabricated "sess-g1", which C4 validation now correctly
  rejects. The fixture now creates a REAL session in the daemon's own store
  and submits that id. The finding-17 pin (HTTP-only submitter seam serves
  an ack, never 503) is preserved; the test's intent never included
  accepting phantom ids.

## 2. Verify output (all run in this session)

- go test ./internal/session/ -count=1                 -> ok (full package)
- go test ./internal/session/ -run TestIncludeToolCalls -count=1 -v
  -> PASS both arms: include_tool_calls=true joins 2 rows by message id
  (name/args_summary/result_summary/id/at pins) AND
  default-false byte-identical legacy (absent == explicit false, key set
  {messages,total}, no tool_calls key on any row)
- go test -overlay /tmp/ctx01-overlay/overlay.json ./internal/agent/
  -run TestConvOnly -count=1 -> ok (TestConvOnlyHistoryRoundtrip all 3 arms
  incl. concurrent race-safety, TestConvOnlyHistoryFK787Reproduction).
  Also ran GREEN without the overlay earlier in-session; the overlay masks
  ONLY sibling leaves' in-flight files (loop_park.go references
  llm.DefaultFailurePolicyConfig which is mid-edit in internal/llm's owner's
  working copy; ralph_loop.go MaxReplanExtensions similar).
- go test -overlay ... ./internal/rpc/
  -run 'TestUnknownIDRejected|TestBothIDMismatch' -count=1 -> ok (T1,
  re-verified by this pass)
- go test -overlay ... ./internal/daemon/
  -run TestHTTPOnlySubmitConfigServesChatSubmit -count=1 -> ok
- go build ./... -> clean at 10:52 with the whole tree; at report time the
  tree carries sibling in-flight edits (see overlay note) that break
  internal/agent compilation independent of this leaf.
- gofmt -l on every file this leaf touched -> clean; go vet
  ./internal/session/ ./internal/agent/ ./internal/rpc/ -> clean.
- No non-ASCII in any line this leaf added (checked byte-scan over diff +
  new file). No '_ = ' introductions. All new wrapped errors use %w.

## 3. Live-gate evidence

The leaf checklist's live-rig (conv-only turn -> daemon restart -> messages
present; unknown id rejected) is deferred to the orchestrator's rig pass;
the three gate suites pin the same contract end-to-end through the real
SQLite store and the real handler closures (T2 roundtrip arm proves the
exchange persists under a conversation-only id; FK787Reproduction pins the
exact defect text this fix closes; T1 pins unknown-id rejection at the ack
with no publish).

## 4. Deviations / notes

- T4 (resume contract, TestResumeContract) NOT attempted: outside the
  finishing scope (T1/T2/T3 gates). No such test exists yet in
  internal/session -- flagging for the branch orchestrator.
- The C4 upsert is enforced at BOTH levels by design: persistExchange
  pre-calls EnsureSessionRow (explicit C4 ordering), and the store
  transactions re-guarantee the row inside the same tx, so any other
  SaveMessages caller (services, threads) also can't hit 787. The pre-call
  failure path only Debug-logs because the in-tx guarantee is authoritative.
- http_only_submit_test.go was green pre-leaf and is a behavior change the
  T1 contract mandates; the update adapts the fixture, not the assertion
  strength (still requires accepted:true + turn_id on a real session).
- Tree contention incident (durability note): a git stash -u used to
  baseline-test sibling failures swept in live sibling edits; everything
  was restored from the stash (kept as stash@{0}, NOT dropped) and
  re-verified by the gate runs above. Sibling files ralph_loop.go,
  events.go, app.go retain their newer live edits deliberately.
- Attribution for the one green->red flip found in the daemon package:
  TestAgentJobProcessor_ParkedTurnDoesNotFailJob (H1). Cause is the
  errors.Is(err, ErrTurnParked) branch in internal/agent/handler.go --
  gate T8 (01-safety/05) work landed on the SHARED file by a prior/sibling
  session, not by this leaf (this leaf's handler.go edit is only the
  persistExchange ensurer pre-call). That branch makes a parked turn ack
  without the sentinel the daemon AgentJobProcessor test demands; the
  processor-side completion belongs to 01-safety/05, whose owner is
  editing internal/agent/loop_park.go in this worktree RIGHT NOW (file
  mtimes advancing during this session). Deliberately NOT fixed here -
  correcting it would collide with the sibling's in-flight gate.
  Orchestrator: sequence 01-safety/05's daemon seam with this test.
