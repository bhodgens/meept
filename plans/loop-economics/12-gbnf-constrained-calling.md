# GBNF-Constrained Tool Calling - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Grammar-constrained tool-call JSON for llama.cpp-family providers: schema->GBNF converter, request attach, fallback warn.
- **Deps:** 02 (full schemas via tool_view/registry), 11 (local model availability path) | **Context:** 65K | **Group:** E

## Goal

Small models emit malformed tool-call JSON. llama.cpp server accepts a `grammar` field making invalid output impossible. Generate GBNF from the ACTIVE tool definitions: array-root of tool-call objects (atomic-agent lesson: array-only root defeats sampler first-token bias). Unsupported schema constructs fall back to unconstrained + one-time warning. Config-gated; only for providers whose transport is the managed llama.cpp runtime.

## Context

internal/llm provider layer — find llama.cpp request construction in runtime_process.go/client paths. Tool definitions arrive as llm.ToolDefinition w/ Parameters (llm.FunctionParameters ~ map[string]any JSON-schema-ish).

Key files: internal/llm/*, new internal/llm/gbnf.go.

## Interface Contracts (From Parent)

```go
// internal/llm/gbnf.go:
func GrammarForTools(defs []llm.ToolDefinition) (grammar string, complete bool)
// Root ::= "[" ws item ("," ws item)* "]"? — MUST allow exactly-one array too.
// Per tool: name enum-literal + arguments object from JSON schema subset:
//   string(number/string/boolean enums as alternatives), number, integer,
//   boolean, array<supported>, object{properties,required}
// Unsupported constructs (oneOf/nested-anyOf/patternProperties/additional
// schema keywords) mark that TOOL unsupported -> excluded from grammar;
// if ANY tool excluded or defs empty -> complete=false (caller decides).
func AttachGrammar(req *CompletionRequest, g string) // no-op on non-llamacpp transports
```

Provider gating: config [agent.tools] gbnf_constrained bool default FALSE; when true AND active provider is managed llama.cpp runtime -> attach GrammarForTools(current FULL defs incl. always_full set). Incomplete grammar -> log warn once per session, proceed unconstrained. Indexed-mode interplay: when schema_mode=indexed, grammar covers core tools ONLY (stubbed ones can't be called validly anyway) — document.

## Tasks
1. Failing converter tests table-driven: primitives; enum strings -> alternatives; required vs optional ordering (GBNF requires required-first reordering); nested object depth-2; array of strings; unsupported oneOf -> tool excluded + complete flag false; empty defs -> empty+false; root allows single-object array form.
2. Golden test: fixture defs -> exact grammar string snapshot (regression pin).
3. Failing attach tests: llamacpp transport receives grammar field; openai-compat transport untouched; config-off untouched.
4. Integration smoke test w/ fake llama server asserting request body contains grammar and response parsing unchanged.
5. Docs section (llm-management page from leaf 11) + config reference line.

## Self-Verification Checklist
- [ ] -race green internal/llm
- [ ] Zero behavior change default-off
- [ ] Warn-once deduplicated per session not per call

## Review Checklist
- [ ] Converter total function (never panics on weird schemas)
- [ ] Escaping of quotes/backslashes in string literals correct (test)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: correctness > coverage — a wrong grammar bricks generation; exclude aggressively.
