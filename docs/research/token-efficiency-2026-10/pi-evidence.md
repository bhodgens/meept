# Pi token-efficiency audit evidence (2026-10-31)

Clone: /tmp/pi-eval/pi (TMPDIR - re-clone after reboot). Repo:
earendil-works/pi @ 8ffea8a1bfdc32523ab056549b26141018c1e700.
NOT can1357/oh-my-pi (the compare-prep.sh alias 'oh-my-pi' is a different project).

## Pi mechanisms (verified file:line)

1. Minimal system prompt: one-sentence preamble + per-tool one-line snippets +
   rules. packages/coding-agent/src/core/system-prompt.ts:155-168. Default
   tools = [read, bash, edit, write] (settings-manager.ts:85).
2. Skills = one line each (name/description/location XML), model reads SKILL.md
   on demand; `disable-model-invocation: true` hides a skill entirely.
   skills.ts:358-388.
3. Output caps at tool layer: 2000 lines / 50KB (tools/truncate.ts:11-12);
   bash keeps TAIL (truncateTail), spills full output to temp file, path in
   result schema `full_output_path` (tools/bash.ts:255,57-59).
4. Sectioned system prompt, diff-patched on change (system-prompt.ts:216-228) -
   cache-prefix stability.
5. Compaction: shouldCompact at contextTokens > contextWindow - reserveTokens
   (reserve 16384, compaction/compaction.ts:267-269); chars/4 estimator;
   incremental UPDATE_SUMMARIZATION_PROMPT (line 577).
6. Cache warmer: 1-token replay only when expected savings >= $0.05
   (core/cache-warmer.ts:23).
7. Codemode: model-written JS in QuickJS/wasm sandbox; nested tool calls never
   enter LLM context, only script output+return value (packages/codemode/README.md:3).
8. Anthropic cache_control markers on system prompt, last tool def, last
   message (packages/ai/src/api/anthropic-messages.ts:1526-1547).
Third-party cold-start numbers: pi ~18.2K vs Claude Code ~29.3K ("Hello",
Opus 5.5); bare pi < 1K (agents.cocode.dk benchmark, lws.io blog).

## Meept state (verified file:line, 2026-10-31)

At parity or better:
- Indexed schema mode DEFAULT-ON: non-core tools -> one-line stub
  '<desc> use tool_view{name}' + empty params (internal/tools/registry.go:238-240;
  resolution internal/agent/loop.go:1411-1445; always-full set
  internal/config/schema.go:1954-1971).
- Cache-stable prompt: AssembleOrdered + stable prefix SHA (prompt.go:468-492),
  StabilizeToolPrefix per call (loop.go:4442-4448), PromptCacheBoundary (prompt.go:323).
- Incremental summary update (internal/llm/context_compactor.go:610-611).
- Compaction-time tool-output pruning with optional spill (pruneToolOutputs,
  context_compactor.go:802-817; protectedBudget 40000, minSavings 20000;
  SpillEnabled default FALSE, line 26-27).
- SpillService exists: $MEEPT_HOME/spill/<session_id>/ (internal/llm/spill.go:46-61).

Gaps (the branches of this tree):
- GAP-1: shell truncation keeps HEAD 50000 bytes and DISCARDS the rest
  (internal/tools/builtin/shell.go:31 MaxOutputSize=50000, :510, :719). No
  spill pointer at tool layer.
- GAP-2: no codemode equivalent (zero hits codemode|goja|quickjs|nested-tool
  in internal/; go.mod has no JS runtime dep).
- GAP-3: tool definitions still enumerate all ~75 registered tools as stubs
  every turn; no task-driven subset. (loop.go:4437 GetDefinitions sends all.)
- GAP-4: skill routing matches fuzzy MatchAll against the SYSTEM PROMPT BASE
  (context_injector.go:121-166: relevantSkillNames(base)), capped at 5
  (maxRelevantSkills, line 15), and on zero matches falls back to ALL cached
  skills - both the wrong signal and the wrong failure mode.

## Per-agent tool lists today

- config/agents/<id>/AGENT.md frontmatter `additional_tools` (e.g. coder lists
  file_read..tool_view) feeds FilteredToolRegistry (internal/agent/executor.go:1927,
  registry.go:790). Skills carry allowed_tools frontmatter honored at skill
  execution (loop.go:2992-3011, 3477-3479; internal/skills/index.go:28-29).
- Intent analyzer (internal/agent/intent_analyzer.go, lfm classifier alias)
  produces intent labels; it does NOT currently emit tool subsets.
