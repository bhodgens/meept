# Reasoning Strip in History + Think-Block Doctor Check - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 3 leaf documents under this node
- **Scope:** Strip reasoning (`<think>` blocks) from assistant replies BEFORE they enter conversation history (defense in depth for deepseek-style reasoning models served over inline-tag wire forms), and add a report-only `meept doctor` check that detects misconfigured reasoning endpoints. Both fixes are repo-level so every meept deployment benefits.

## Motivation (frozen)

Local deepseek-style reasoning models (BTL-4 benchmark finding, 2026-10) degrade
when stale reasoning text replays in multi-turn history. Two wire forms exist:

1. Separate channel: server emits `reasoning_content` / `reasoning` deltas -
   meept already parses these into `Response.Reasoning` (`internal/llm/client.go:2622`)
   and that field never enters history. SAFE.
2. Inline tags: server inlines `<think>...</think>` inside `content` (llama.cpp
   `reasoning_format:"none"`, mlx_lm, raw APIs). The ONLY stripper today is
   `stripThinking` (`internal/llm/task_summarizer.go:269`) and its call sites are
   all in the summarizer. `conv.AddAssistantMessage(finalResponse)`
   (`internal/agent/loop.go:3073, 3250, 3469/3474, 4959, 4421`) stores content
   verbatim. GAP: inline reasoning persists in history and replays every turn.

Fix: strip at the choke point so history never carries reasoning, and give
operators a doctor check so misconfigured endpoints are visible (doctor is
report-only by repo convention - never auto-fix).

## Architecture

- `internal/llm`: move the think-strip regexes into a new exported
  `internal/llm/reasoning_strip.go` (`StripThinking(content) string`); the
  existing unexported `stripThinking` in task_summarizer.go delegates to it.
  No behavior change to summarization.
- `internal/agent`: every assistant message that enters a `Conversation` passes
  through the strip first. Choke point: `Conversation.AddAssistantMessage` /
  `AddAssistantMessageWithToolCalls` / `AddMessage` in
  `internal/agent/conversation.go` - NOT the 6+ scattered loop.go call sites.
- `internal/daemon` (doctor): new report-only check "reasoning wire form"
  probing each local OpenAI-compat endpoint with a reasoning-capable model and
  classifying: separate-channel (ok), inline-tags (warn: "model emits <think>
  inside content; meept strips it, but the endpoint wastes tokens - consider
  --jinja / reasoning parser flags"), leak (warn: reasoning visible in content
  with no tags). Follows existing doctor conventions: report-only, one check
  per health endpoint, surfaced in `meept doctor` output with install_hint-style
  remediation text.

## Interface Contracts (frozen)

### Contract 1: llm.StripThinking

```
// File: internal/llm/reasoning_strip.go
package llm

// StripThinking removes reasoning wire-forms from model content text:
// closed <think>...</think> blocks anywhere, a leading unclosed <think>
// block, and a leading reasoning_content fragment. Trimmed. Exported so
// the agent loop / conversation layer can strip history-side too.
func StripThinking(content string) string
```

- The regexes `thinkBlockRe`, `unclosedThinkRe`, `reasoningContentLineRe` MOVE
  from task_summarizer.go to reasoning_strip.go.
- task_summarizer.go keeps its unexported `stripThinking` as a one-line
  delegate: `func stripThinking(c string) string { return StripThinking(c) }`
  (or replace call sites with StripThinking directly - implementer's choice,
  but no duplicate regex definitions may remain).
- Existing `task_summarizer_test.go` TestStripThinking cases MUST pass
  unchanged (they pin behavior; update only the call target if calls move).

### Contract 2: Conversation-level strip

- `internal/agent/conversation.go`: AddAssistantMessage,
  AddAssistantMessageWithToolCalls, and any add-path that takes assistant
  content apply `llm.StripThinking` to the content field before append.
- Tool calls are NOT stripped (they carry `arguments` JSON, never prose
  reasoning); only the message Content field is stripped.
- Empty-after-strip content stays an empty-string message (callers already
  handle empty content; do not drop the message - it may carry ToolCalls).
- History restore paths (session thread reload into Conversation) route
  through the same add methods OR apply the strip once at restore - both
  acceptable; pick one and note it in the leaf report.
- The reply-guard rewrite path at loop.go:3073 adds the PRE-strip content
  today; after this change the conversation stores the stripped form
  automatically. No loop.go call-site edits REQUIRED for correctness of
  history; do not refactor loop.go beyond what the tests demand.

### Contract 3: doctor reasoning check (report-only)

- New check in `cmd/meept/` following the `checkModelsDoctor` pattern
  (cmd/meept/doctor_models.go:24 — verified: `doctorCheck{name, ok, warn,
  detail}` struct at cmd/meept/doctor.go:33, config load via
  `llm.LoadProvidersConfigDefault()`). New file `cmd/meept/doctor_reasoning.go`
  exposing `checkReasoningDoctor() []doctorCheck`, appended to the checks
  slice in `runDoctor` (cmd/meept/doctor.go:99) next to the models checks.
- Check name: `reasoning-wire-form`. For each configured provider whose
  lifecycle is local (runtime llama-cpp / mlx) AND has at least one model with
  the `reasoning` capability: send ONE minimal chat completion
  ("Reply with the word: ready") and inspect the response:
  - `reasoning_content` or `reasoning` field populated, content clean → PASS
    "separate reasoning channel".
  - `<think>` in content → WARN "inline reasoning tags; meept strips them from
    history, but the endpoint wastes output tokens - enable --jinja with a
    reasoning parser or reasoning_format to split the channel".
  - reasoning-looking prose in content with no tags → WARN "reasoning leaked
    into content untagged".
  - endpoint unreachable / disabled → SKIP (never block doctor on a down
    endpoint; doctor is report-only).
- Timeout 10s per endpoint. Never auto-restart, never auto-fix, never mutate
  config. Output follows the existing doctor check formatting (leaf greps an
  existing check for the exact shape).
- Cloud providers: SKIP (no network calls to remote APIs from doctor).

## Non-goals (frozen)

- No change to `shouldSendReasoning` / `applyOpenAICompatReasoning` wire-form
  logic (reasoning_translate.go untouched).
- No BTL-4 provider entry in models.json5 (user adds when the model ships).
- No new retry/rotation behavior. Strip is silent and unconditional.
- No summarizer behavior change (TestStripThinking pins it).
- No GUI/TUI surface for the doctor check beyond what doctor already prints.

## Wave Plan

Wave 1 (parallel, disjoint files):
- Leaf 01: `internal/llm/reasoning_strip.go` + move regexes + delegate +
  unit tests. Owns: internal/llm/reasoning_strip.go, task_summarizer.go (regex
  move only), task_summarizer_test.go (call-target touch only).
- Leaf 02: doctor reasoning-wire-form check. Owns: doctor check file(s) in
  internal/daemon or cmd/meept per grep, its test file. Does NOT touch
  internal/agent or internal/llm.

Wave 2 (serial, depends on Leaf 01 landed):
- Leaf 03: conversation.go choke-point strip + unit tests (append to
  conversation_test.go or a new conversation_strip_test.go). Owns:
  internal/agent/conversation.go, its test file, and the e2e suite addition
  if the leaf author opts for one (e2e/suites/ per AGENTS.md policy: new
  feature tests go in the hermetic e2e tier).

Integration gate (orchestrator, after Wave 2):
- `go build ./...`
- `go test -p 2 ./internal/llm/... ./internal/agent/... ./internal/daemon/...`
- `make test` (short-mode full suite)
- `make e2e-affected` if the diff touches e2e-mapped packages
- `make graphs-check` - the tree does NOT change bus/RPC/HTTP/WS surfaces;
  if graphs-check fails it is line-offset churn: run `make graphs` and commit
  the regeneration separately.
- AGENTS.md update if the doctor gains a user-visible surface worth a line
  in the root doc (doctor list).

## Tracking Table

| Leaf | Title | Status | Commit |
|------|-------|--------|--------|
| 01 | llm.StripThinking export + regex home | COMPLETE 2026-10-07 | 98873e7c |
| 02 | doctor reasoning-wire-form check | COMPLETE 2026-10-07 | 7346fbfb |
| 03 | conversation choke-point strip | COMPLETE 2026-10-07 | e9414023 |

## Acceptance (tree-complete definition)

1. `TestStripThinking` (moved) green; new tests: exported-symbol smoke,
   conversation add-path strip (closed tags, unclosed leading block,
   reasoning_content fragment, tool-call content untouched), doctor check
   classification matrix (pass / warn-inline / warn-leak / skip-unreachable).
2. Full short-mode suite green with `-p 2`.
3. A hand-verified trace: add an assistant message containing
   `<think>reasoning</think>Final answer.` to a Conversation; the message
   list built for the LLM shows only "Final answer."
4. `meept doctor` prints the reasoning-wire-form line per local reasoning
   endpoint (live check on this machine's :8080/:8081/:8083/:8084 endpoints).
5. Docs: `docs/workflows/` feature note updated (internal/llm + internal/agent
   behavior; doctor check listed wherever existing doctor checks are
   documented).
