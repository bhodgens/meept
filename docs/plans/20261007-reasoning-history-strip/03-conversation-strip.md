# Leaf 03: conversation choke-point reasoning strip

## Meta

- **Role:** Leaf (Wave 2, SERIAL — depends on Leaf 01's `llm.StripThinking` landed. Start only after Leaf 01 is committed; its commit SHA will be recorded in the tracking table in master.md.)
- **Parent:** master.md in this directory
- **Owns:** `internal/agent/conversation.go` (add-path hunks only), `internal/agent/conversation_strip_test.go` (new), optionally one hermetic e2e suite under `e2e/suites/` + `e2e/manifest.json` entry if a conversation-level behavior needs the harness (decide inside the leaf; unit tests may suffice).
- **Does NOT touch:** `internal/llm/` (Leaf 01's exports are consumed, never edited), `internal/agent/loop.go` except where the tests demand (per master: no loop.go refactors).

## Goal (from master, frozen)

Every assistant message entering a `Conversation` passes through
`llm.StripThinking` on its Content field before append, so inline
`<think>` reasoning (deepseek-style models on inline-tag wire forms) never
persists in multi-turn history. Tool call arguments are NOT stripped.

## Current state (drift-audited 2026-10-07)

- `internal/agent/conversation.go:471` — `AddAssistantMessage(content string)`
  builds a `ChatMessage{Role: llm.RoleAssistant, Content: content}`.
- `conversation.go:480` — `AddAssistantMessageWithToolCalls(content string,
  toolCalls []llm.ToolCall)` same shape plus calls.
- `conversation.go:168-219` — Conversation keeps parallel `messageTypes`
  classification (MessageReasoningStep etc.) — classification runs on the
  content at add time via `classifyMessageClassification`
  (conversation.go:311); check whether classification happens in the same
  add path and strip BEFORE classify so heuristic misclassification of
  reasoning-shaped prose is reduced.
- `internal/agent/loop.go` call sites (3073, 3250, 3469/3474, 4421, 4959)
  add assistant content verbatim today — the choke-point change covers them
  without edits.
- Restore paths: grep `Restore|reload|FromThread|hydrate` in
  internal/agent + internal/session to find where history is rebuilt into a
  Conversation after restart; route through the same add methods OR strip
  once at restore. Pick one, note in report.
- Leaf 01 exports `llm.StripThinking(content string) string` in
  `internal/llm/reasoning_strip.go` — verify the landed signature with
  `go doc ./internal/llm StripThinking` before writing the call; trust the
  source over this doc.

## Tasks

1. TDD: `internal/agent/conversation_strip_test.go` FIRST. Cases:
   - `AddAssistantMessage("<think>r</think>Final answer.")` → stored
     Content == "Final answer.".
   - Leading unclosed block `"<think>partial` → stored Content == "".
   - Leading reasoning_content fragment stripped.
   - `AddAssistantMessageWithToolCalls("<think>r</think>Calling.",
     calls)` → Content stripped, ToolCalls byte-identical (arguments JSON
     untouched — assert a JSON-argument string survives).
   - Plain prose passes byte-identical.
   - History-build check: the message slice returned for the LLM
     (`c.Messages()` or the package's accessor — grep the real name) shows
     only stripped content.
   RED evidence (undefined behavior / failing assertions) before the edit.
2. Apply the strip inside the two add methods (and any third assistant-add
   path found by grep `RoleAssistant` in conversation.go) — content =
   llm.StripThinking(content) before append and before classification.
3. Empty-after-strip stays an empty-string message (do NOT drop the
   message — it may carry ToolCalls).
4. Restore-path decision (see current state) and implement/justify.
5. E2E decision: if the unit tests cover the contract, add a small hermetic
   e2e assertion in an EXISTING chat-path suite only if cheap; otherwise
   justify skipping (AGENTS.md: new FEATURE tests go in e2e tier — this is
   a defense-in-depth internal invariant; a reasoned skip is acceptable,
   note it).

## Guardrails

- Do not modify `internal/llm/` — consume the export; if the signature
  differs from this doc, adapt and note the drift.
- Do not touch loop.go nudge/reply-guard logic; the strip happens under it.
- `go test -p 2 ./internal/agent/` must stay green — the loop_* tests pin
  history shapes (loop_restore_test.go asserts message roles).
- Commit explicit paths only; siblings work in this worktree.

## Acceptance

- New tests green: `go test -p 2 ./internal/agent/ -run Strip -v`.
- Package green: `go test -p 2 ./internal/agent/`.
- Hand-verified trace in the report: constructing a Conversation, adding
  `"<think>reasoning</think>Final answer."`, and printing the message list
  showing only "Final answer.".
- `go build ./...` green.

## Report back

RED/GREEN evidence, restore-path decision + reason, e2e decision + reason,
files changed, deviations (with reason).
