# Shared Conventions — Claude Code Parity Forest

## §1 Project Layout

```
cmd/meept-daemon/     — daemon entrypoint
cmd/meept/            — CLI entrypoint
internal/agent/       — agent loop, orchestrator, dispatcher, hooks, prompts
internal/agent/prompt/  — filesystem-based prompt component loader/builder
internal/agent/prompts/ — Go-const specialist prompts (BuildBaselinePrompt)
internal/agent/specs/   — agent definition types and loading
internal/llm/         — LLM client, adapters, token cache, context compactor
internal/memory/      — memory manager, episodic, consolidation, epistemic
internal/tools/       — tool interface, registry, executor
internal/tools/builtin/ — builtin tool implementations
internal/security/    — security engine, fence, taint, secrets, prompt guard
internal/selfimprove/ — learning pipeline (detect→analyze→generate→validate→apply)
internal/compress/    — compression pipeline, SmartCrusher, CCR store
internal/daemon/      — daemon config, lifecycle
config/prompts/       — filesystem prompt components (base/, conditional/, etc.)
config/agents/        — built-in agent definition files (JSON5)
```

## §2 Go Coding Conventions

- **Predictable ID prevention:** Never use `time.Now().UnixNano()` or `math/rand` for IDs. Use `pkg/id.Generate()`. Run `go run ./tools/analyzers/predid/... ./...` to verify.
- **Setter nil guards:** Every `Set*` method MUST include `if x != nil { t.field = x }`. Verified by `internal/tools/builtin/setters_test.go`.
- **Typed-nil interface guard:** Guard `*ConcreteType` → interface assignments at call sites and in `With*` functions.
- **Mutex scope:** Never hold a mutex across I/O. Collect under lock, release, then operate.
- **Wiring requirement:** Every feature must have at least one user-facing interface (CLI, TUI, GUI, or HTTP API). Data structures without interfaces are INCOMPLETE.
- **UI text:** All UI text must be lowercase.
- **Error handling:** Wrap errors with `fmt.Errorf("context: %w", err)`. Never discard errors silently.
- **Imports:** Group stdlib, external, internal. Use `gofmt` ordering.
- **Naming:** Follow Go conventions — MixedCaps for exported, camelCase for unexported. Acronyms all-caps (HTTP, LLM, SSRF).

## §3 Test Conventions

- Table-driven tests with `t.Run(name, ...)` for all new functionality.
- Use `testify/assert` and `testify/require` (already in go.mod).
- Run `go test ./... -race` before reporting completion.
- Run `go vet ./...` before committing.
- No test may depend on network access or external services.

## §4 Commit Policy

**Only orchestrators commit. Implementation agents must NOT commit.**

Implementation agents: write code, run tests, report results. Do NOT run `git add` or `git commit`. The orchestrator handles all git operations after review passes.

## §5 Prompt Component Conventions

- Filesystem prompt components live in `config/prompts/<category>/<name>.md`
- Referenced via dot notation: `"base.anti_patterns"` → `config/prompts/base/anti_patterns.md`
- Go-const prompts live in `internal/agent/prompts/` as exported constants
- `BuildBaselinePrompt()` concatenates: `BaselineCapabilities + BaselineGuidelines + MemoryInstructions + ToolUsageGuidelines + EvidenceRequirements`
- Agent definitions in `config/agents/` use JSON5 format with front matter fields

## §6 Cross-Tree Interface Contracts

### Verification Mode (Tree 01 → all agent definitions)
```go
// In agent spec front matter (JSON5):
{
  "verification": {
    "enabled": true,           // default true; false to disable for this agent
    "model": "",               // empty = inherit agent's model; or model ID override
    "auto_trigger": true,      // auto-spawn verifier after non-trivial changes
    "max_fix_loops": 3         // auto-fix iterations before escalating to user
  }
}
```

### Prompt Cache Boundary (Tree 03 → prompt builder)
```go
// Marker inserted by prompt builder between static and dynamic sections:
const PromptCacheBoundary = "__MEEPT_PROMPT_CACHE_BOUNDARY__"

// CacheScope annotation on prompt sections:
type CacheScope int
const (
    CacheScopeNone   CacheScope = iota // no caching
    CacheScopeStatic                   // stable across sessions (system prompt core)
    CacheScopeSession                  // stable within a session (memory, tools)
)
```

### Per-Tool Safety (Tree 05 → tool interface)
```go
// Added to tools.Tool interface:
type Tool interface {
    // ... existing methods ...
    IsReadOnly(input map[string]any) bool      // default false
    IsConcurrencySafe(input map[string]any) bool // default false
}
```

### Daemon Verification Config (Tree 01 → daemon settings)
```go
// In daemon config (meept.json5):
{
  "verification": {
    "enabled": true,
    "default_model": "",        // empty = inherit; or model ID
    "max_fix_loops": 3,
    "auto_trigger_threshold": 3 // file edits before auto-trigger
  }
}
```

## §7 Pitfalls

- Do NOT use `read_file` output in `write_file` — line-number prefixes corrupt source. Use `search_files` or `terminal("cat file")` for exploration.
- Do NOT commit from implementation agents.
- Run `go build ./...` after every file change to catch compile errors early.
- Run `go vet ./...` before reporting completion.
- Check for unused functions/imports before reporting (U1000 linter).
- Float comparisons in tests: use epsilon, not `!=`.
- Map iteration order is nondeterministic — sort keys before accumulating.
