# GBNF-Constrained Tool Calling - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Grammar-constrained tool-call JSON for ANY provider whose endpoint supports it: llama.cpp `grammar`, vLLM `guided_grammar`, OpenAI-compat structured outputs; schema->GBNF converter; capability detection; graceful fallback.
- **Deps:** 02 (full schemas), 11 (local model availability) | **Context:** 65K | **Group:** E

## Goal

Small models emit malformed tool-call JSON. Grammar constraints make invalid output impossible. Meept's client is a generic OpenAI-compatible payload builder (client.go buildChatRequest ~line 261-330), so constraint support is a PER-ENDPOINT capability, not per-vendor: llama.cpp server accepts `grammar` (GBNF); vLLM accepts `guided_grammar`; several OpenAI-compatible servers accept `response_format: {type:"json_schema", schema}`. This leaf adds a converter + a per-provider capability flag + attach logic with fallback.

## Context

buildChatRequest assembles `payload map[string]any` — adding a key is trivial; knowing WHICH key each endpoint honors is the actual problem. Providers resolve via config/models.json5 (base_url + capabilities). RuntimeManager launches managed llama.cpp/MLX runtimes (leaf 11 wires pulled models). MLX-server and Ollama expose OpenAI-compatible APIs WITHOUT grammar fields — those endpoints simply get no grammar (capability absent), not an error.

Key files:
- internal/llm/client.go - buildChatRequest (~290 payload block), chatOptions struct (~636) gains grammar field + WithGrammar option
- internal/config/models.json5 / models_catalog.go - per-provider capability declaration
- internal/llm/gbnf.go NEW - converter

## Interface Contracts (From Parent)

```go
// internal/llm/gbnf.go:
func GrammarForTools(defs []llm.ToolDefinition) (grammar string, complete bool)
// Root allows single-object OR array-of-objects (array-root bias fix).
// Supported schema subset: string w/ optional enum, number, integer, boolean,
// array<supported>, object{properties,required} to depth 3.
// Required-first reordering (GBNF needs required props before optional).
// Any unsupported construct -> that TOOL excluded from grammar;
// complete=false when any exclusion or empty defs.
// NEVER panics on arbitrary schemas.

func AttachGrammar(reqPayload map[string]any, mode string, g string)
// mode "llamacpp":  payload["grammar"]=g
// mode "vllm":      payload["guided_grammar"]=g
// mode "json_schema": payload["response_format"]={type:"json_schema", json_schema:{schema:<from tools>}}
//   NOTE json_schema mode converts the JSON SCHEMA directly (not GBNF) —
//   separate small converter JSONSchemaForTools(defs) in same file.
```

Capability declaration (models catalog entry):
```
providers.<id>.tool_constraint: "" (default) | "llamacpp" | "vllm" | "json_schema"
Managed runtimes auto-declare: llama.cpp->"llamacpp". MLX/Ollama/remote default "".
Config [agent.tools] gbnf_constrained bool default FALSE (global kill-switch).
```

Attach rule at buildChatRequest: gbnf_constrained AND provider declares capability AND defs present -> AttachGrammar(payload, mode, g). Incomplete grammar -> warn once/session, skip attach. Indexed-mode interplay: grammar covers always_full core tools only (stubbed tools cannot be called validly anyway).

## Tasks
1. Failing converter tests table-driven: primitives; enum strings -> alternatives; required-first ordering; depth-3 nesting; arrays; oneOf -> tool excluded + complete=false; empty defs; root single-vs-array both derivable; quote/backslash escaping golden test.
2. Golden snapshot test of full fixture grammar (regression pin).
3. Failing attach tests: llamacpp/vllm/json_schema modes set correct keys; unknown mode no-op; capability-empty provider untouched; config-off untouched; incomplete-grammar warn-once dedupe.
4. Wire chatOptions field + WithGrammar + buildChatRequest integration reading resolved provider capability (thread through existing cfg path).
5. Catalog plumbing: tool_constraint field parse + managed-runtime auto-declaration in runtime_config.go.
6. Docs: llm-management.md section (matrix of endpoint types vs constraint support) + config reference lines.

## Self-Verification Checklist
- [ ] -race green internal/llm
- [ ] Zero wire change for providers without declared capability
- [ ] Warn-once per session, not per call
- [ ] Default-off global switch honored everywhere

## Review Checklist
- [ ] Converter total (no panic paths; fuzz-lite test with random maps)
- [ ] json_schema mode emits valid JSON Schema (marshal roundtrip test)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps.

## Notes
- Correctness > coverage: exclude aggressively. A wrong grammar bricks generation entirely.
- Structured-output servers that only support response_format get PARTIAL value (schema without enum-tightness) — document honestly in the matrix.
