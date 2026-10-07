# Leaf 01: llm.StripThinking export + regex home

## Meta

- **Role:** Leaf (Wave 1, parallel with Leaf 02 — disjoint files)
- **Parent:** master.md in this directory
- **Owns:** `internal/llm/reasoning_strip.go` (new), `internal/llm/task_summarizer.go` (regex move only), `internal/llm/task_summarizer_test.go` (call-target touch only), `internal/llm/reasoning_strip_test.go` (new), `internal/llm/reasoning_strip_fuzz_test.go` (optional).
- **Does NOT touch:** anything under `internal/agent/`, `cmd/`, `internal/daemon/`.

## Goal (from master, frozen)

Move the think-strip regexes from the summarizer into a new exported
`internal/llm/reasoning_strip.go` so the agent conversation layer (Leaf 03)
can strip reasoning from history-side too. Zero behavior change to
summarization.

## Current state (drift-audited 2026-10-07)

- `internal/llm/task_summarizer.go:250-274`: `thinkBlockRe`
  (`(?is)<think>.*?</think>`), `unclosedThinkRe` (`(?is)^\s*<think>.*$`),
  `reasoningContentLineRe` (leading `"reasoning_content": "..."` fragment),
  and `stripThinking(content string) string` applying all three then trimming.
- `internal/llm/task_summarizer_test.go:342` `TestStripThinking` pins the
  behavior with table cases including closed blocks, unclosed leading block,
  quoted/unquoted reasoning_content keys, and fragment-only inputs.

## Tasks

1. Create `internal/llm/reasoning_strip.go`:
   - Move the three regex vars verbatim.
   - Export: `func StripThinking(content string) string` with the exact
     signature and doc comment given in master Contract 1.
2. In `task_summarizer.go`: delete the moved regexes and rewrite
   `stripThinking` as a one-line delegate to `StripThinking`. Keep the
   existing call sites in this file untouched (they call `stripThinking`).
3. Tests:
   - `internal/llm/reasoning_strip_test.go`: new tests for the EXPORTED
     symbol — at minimum: (a) multiple closed blocks anywhere in the string,
     (b) leading unclosed block to end-of-string, (c) leading
     reasoning_content fragment, (d) content with no reasoning passes
     byte-identical, (e) result is trimmed. Do not delete or weaken
     `TestStripThinking` in task_summarizer_test.go — it still passes through
     the delegate.
   - If the package has an existing fuzz test covering stripThinking, add the
     exported name to it; if not, skip (optional).
4. TDD discipline: write the new test file FIRST, show RED (undefined symbol),
   then implement, then GREEN.

## Guardrails

- No changes to `reasoning_translate.go`, `client.go`, `models.go`.
- No renaming of the regex vars (Leaf 03's context cites them by name).
- Pre-commit hooks run mutexio/predid — no new locking, no IDs here.
- Commit explicit paths only (`git commit -- <paths>`), never bare
  `git add -A`; siblings work in this worktree.

## Acceptance

- `go test ./internal/llm/ -run 'TestStripThinking|TestStrip' -v` green.
- `go build ./internal/llm/` green.
- `grep -n 'thinkBlockRe\|unclosedThinkRe\|reasoningContentLineRe'
  internal/llm/*.go` shows exactly one definition site (reasoning_strip.go).

## Report back

RED evidence, GREEN evidence, files changed, any deviation from this doc
(with reason).
