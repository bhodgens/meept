# Plan: Classifier Reliability — Fix Empty-Response Failures

## Goal

Make meept's LLM-based classification (intent classifier + intent analyzer)
reliable against local thinking models. Today every classification call
returns empty content, because:

1. **Token caps truncate mid-think.** LLMClassifier uses `WithMaxTokens(200)`
   and IntentAnalyzer `WithMaxTokens(300)`. The served model (LFM2.5-8B-A1B,
   a thinking model) spends its entire budget reasoning before emitting
   content; generation stops at the cap with no EOS and `content == ""`.
2. **Thinking is never disabled.** Nothing sends
   `chat_template_kwargs: {"enable_thinking": false}` to the llama.cpp
   endpoint, so the model reasons on every call.
3. **Alias failover is dead for classification.** `RecordAliasFailure` is
   only called from the agent loop (`loop.go:3795`). ClassifierClient and
   the intent analyzer hold plain `*llm.Client`s resolved once at startup;
   empty responses never rotate the alias to glm-4.5-air/ollama.

Verified 2026-08-25 from `~/.meept/logs/runtimes/127.0.0.1-8080.process.log`:
task 0 generated exactly 300 tok (= analyzer cap), task 302 exactly 200 tok
(= classifier cap), both `truncated = 0`, both followed by "empty content"
warnings in the daemon log.

Also answered: why the 8B loads when config says 1.2B.
`~/.meept/models.json5` line 73 sets `lifecycle.model_path` (singular) to
the **8B F16** gguf; runtime_config.go synthesizes `{"default": <that path>}`
and `${MODEL_PATH}` expands to it. The per-model entries under `models:`
(lfm-1.2b-q8 etc.) are request-routing metadata only — they do NOT select
which file llama-server loads. One server, one loaded file, all `local/*`
aliases hit it.

## Architecture Overview

Three leaves, all Go-only, all independent of each other:

- **Leaf 01**: disable thinking for classification calls (client-side
  request field). Fixes the root cause with zero token waste.
- **Leaf 02**: raise hardcoded caps AND derive them from model config so
  any future thinking-model-with-large-budget works. Defense in depth.
- **Leaf 03**: wire alias failure reporting into the classifier/analyzer
  path so the fallback chain actually rotates. Restores dead config.

Leaf 01 alone unblocks benchmarks. 02 and 03 are hardening.

All changes are in `internal/llm/`, `internal/agent/`, and one config file.
No Flutter, no schema changes, no cross-language seams.

## Interface Contracts

### Leaf 01 → shared
- `reasoning_translate.go`: new case arm `"local"` (and any unknown
  OpenAI-compatible provider) in `applyOpenAICompatReasoning`:
  ```go
  body["chat_template_kwargs"] = map[string]any{"enable_thinking": enable}
  ```
  where `enable := rc.ResolveEnabled()`. Only emitted when rc != zero
  (the existing `shouldSendReasoning` gate stays).
- Callers opt in via the existing `WithReasoning(&ReasoningConfig{...})`.
- `ReasoningConfig.Enabled=false` must serialize as `enable_thinking:false`
  (not be skipped) — `ResolveEnabled()` already returns false correctly;
  verify `rc.IsZero()` does not treat Enabled=false as zero (it must not:
  check IsZero implementation).

### Leaf 02 → shared
- New helper in `internal/agent/`:
  ```go
  // effectiveCap returns max(requested, min(cfg.MaxOutput, hardCeiling)).
  func effectiveCap(requested int, cfg *llm.ModelConfig) int
  ```
- `LLMClassifier.Classify` and `IntentAnalyzer.AnalyzeTrueIntent` use it.
- Constants: `classifierMinCap = 512`.

### Leaf 03 → shared
- `LLMClassifierConfig` and `IntentAnalyzer` gain optional fields:
  ```go
  Resolver    *llm.Resolver
  AliasName   string
  ```
- On Chat error (including ErrEmptyResponse), callers invoke
  `Resolver.RecordAliasFailure(alias, err)` then re-resolve via
  `ResolveForAlias(alias)` and retry once against the next candidate.
- Success calls `RecordAliasSuccess`.

### Config contract (`~/.meept/models.json5`, user-owned)
- Leaf 02 documents (README/config comment): set `max_output` per model to
  the real budget you want classification to have.
- Optional leaf-03 companion: point `lfm-1.2b-q8` at a genuinely small
  non-thinking GGUF on its own port, or accept that all local/* aliases
  share :8080's single loaded file.

## Coding Conventions

- Follow CLAUDE.md: no mutex across I/O, nil-guard setters, errors wrapped
  with context.
- All new test names start with the function under test.
- No new exported symbols without doc comments.

## Commit Policy

Only the orchestrator commits. Implementation agents must NOT commit.

## Child Index

| # | Document | Est. Context | Dependencies | Concurrency |
|---|----------|-------------|--------------|-------------|
| 1 | `01-disable-thinking.md` | ~30K | None | A |
| 2 | `02-token-caps.md` | ~25K | None | B |
| 3 | `03-alias-failover.md` | ~35K | None | C |

Leaves are fully independent (different files except trivial overlap in
dispatcher wiring for leaf 03 — serialized by orchestrator if needed).

## Verification Plan (orchestrator)

After all leaves merge:
1. `go build ./... && go vet ./... && go test ./internal/llm/ ./internal/agent/`
2. Restart daemon; run probe chat RPC ("Reply with exactly: OK" style
   classification path) and confirm NO "empty content" warnings in daemon log.
3. Rerun `meept-bench run --suite suites/smoke.json --task write-answer`
   from /Users/caimlas/git/meept-bench; expect routing to chat agent and a
   tool-call attempt (file_write), not introspection dump.

## Review Checklist (Orchestrator)

Per leaf, before committing:
- [ ] Interface contract items from master.md present and honored
- [ ] Tests cover the behavior table / failure paths in the leaf spec
- [ ] No unrelated files touched; no drive-by refactors
- [ ] go build/vet/test green on affected packages
- [ ] Existing tests updated, not deleted

## Completion Tracking Table

| Leaf | Status | Reviewed | Committed |
|------|--------|----------|-----------|
| 01-disable-thinking | pending | — | — |
| 02-token-caps | pending | — | — |
| 03-alias-failover | pending | — | — |

## Dispatch Protocol

For each child leaf, in order 01 → 02 → 03 (01 and 02 may run concurrently;
03 edits the same classifier/analyzer files as both and must be serialized
after them):

1. Dispatch implementation agent via delegate_task with the leaf document
   as context. Include: "Do NOT commit. Do NOT run git add. Write code,
   run tests, report results only."
2. Review in-session (main model): read changed files, verify interface
   contracts above, run package tests.
3. Re-dispatch with specific feedback on gaps (max 3 iterations).
4. After review passes: orchestrator stages the leaf's exact files and
   commits `fix(classify): <leaf summary>` referencing this plan dir.
5. Update tracking table.

Concurrency: leaves 01 and 02 touch different files except dispatcher.go
construction wiring (02 adds ModelConfig pass-through; 03 adds resolver
fields). If dispatching concurrently, instruct each agent to keep
DispatcherConfig changes additive and merge sequentially.

## Integration Test Plan

After all leaves complete:
1. `go build ./... && go vet ./... && go test ./internal/llm/ ./internal/agent/`
2. Restart daemon (`kill -TERM $(cat ~/.meept/meept.pid)` then relaunch).
3. Probe classification via chat RPC; assert zero "empty content" warnings
   in /tmp/meept-daemon-test.log for the request.
4. Rerun meept-bench smoke: `/tmp/meept-bench-bin run --suite suites/smoke.json`
   from /Users/caimlas/git/meept-bench. Success = write-answer-file task
   attempts file_write (routing fixed end-to-end).

## Non-Goals

- JSON-schema constrained decoding (follow-up plan; kills the failure class
  structurally but needs response_format plumbing).
- Replacing LFM with another model family.
- Multi-port model topology (one llama-server per model file).

## Pre-Registered Expectations

Written BEFORE execution so we cannot rationalize later:
- Leaf 01 should eliminate >90% of empty-content warnings immediately.
- If empties persist after leaf 01, suspect the model emitting only
  whitespace/punctuation — check raw completions in llama-server log.
- Leaf 03 will surface whether glm-4.5-air fallback actually works; if it
  also fails, the alias chain needs its own investigation (do not paper over).
