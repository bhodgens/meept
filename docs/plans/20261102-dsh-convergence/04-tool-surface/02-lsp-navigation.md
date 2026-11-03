# 02 LSP Navigation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. This branch runs a tests-first gate;
> the failing gate tests ALREADY EXIST and are committed (00T-tests.md T2/T2b
> + the tool-surface e2e suite). Your job is to make them pass - do NOT
> delete, skip, weaken, or rewrite any gate test (report suspected-wrong gate
> tests to the orchestrator instead). Do NOT commit - the orchestrator handles
> all git operations after review. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. After writing a file, do
> NOT read it back - write once and stop. Report what you built, files touched,
> and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add a model-facing read-only `lsp_navigate` tool
  (goToDefinition/findReferences/goToImplementation/hover) that reuses the
  existing `internal/code/lsp` client the writethrough notifier already spawns
  (master C8 + D8: reuse the existing LSP client within go availability).
- **Dependencies:** B1 (TimeoutMs, from 01-tool-timeout.md), B2 (parent
  orchestrator); gate tests in 00T-tests.md
- **Estimated Context:** 22K
- **Concurrency Group:** A
- **Audit references:** /tmp/dsh-audit/runtime-safety.md (LSP tool F)

## Goal

Expose precise code navigation to the model: a single `lsp_navigate` tool with
four read-only operations over `internal/code/lsp.Client`. The plumbing
(transport, lifecycle, writethrough server spawn) already exists; what is
missing is the read-only navigation surface (verified: meept's client has
GotoDefinition/FindReferences/Hover/TypeDefinition but NO GoToImplementation,
and no builtin tool registers any navigation tool). This leaf adds the tool,
adds the one missing client method, wires the ToolActionMap entry so it is not
"Unknown action"-denied, and declares a timeout so a hung LSP query returns a
structured TOOL_TIMEOUT instead of hanging the turn.

## Context

- `internal/code/lsp/client.go` - `Client` with `Start(ctx)` (:49),
  `GotoDefinition(ctx, uri, line, char) ([]Location, error)` (:282),
  `FindReferences(ctx, uri, line, char, includeDecl) ([]Location, error)`
  (:308), `Hover(ctx, uri, line, char) (*Hover, error)` (:331),
  `TypeDefinition(...)` (:461). NO GotoImplementation exists - add it wired to
  `textDocument/implementation`.
- `internal/code/lsp/manager.go` - `NewManager(cfg, opts...)` (:52),
  `GetServerForLanguage(ctx, langID)` (:67), `StartServerForLanguage(...)`
  (:86), `StartServer(...)` (:111), `GetServer(name)` (:265). The writethrough
  notifier (`internal/tools/builtin/lsp_writethrough.go:81 NewLSPWriteNotifier`)
  already constructs and holds a Manager - the nav tool should drive the SAME
  manager seam so a language server spawned for writethrough is reused, not a
  second copy.
- `internal/tools/builtin/` - where the tool is registered. Follow the pattern
  of adjacent builtin tools: a constructor registered via the builtin init /
  container that returns a tool; embed `tools.ToolDefaults`; set `IsReadOnly`
  true.
- `internal/agent/executor.go:404-407` - ToolActionMap maps `lsp_goto_definition`
  etc to `file_read`, but those are DEAD MAP ENTRIES (no tool registers them).
  This leaf adds ONE `lsp_navigate` entry -> `file_read` so the new tool is
  granted and never "Unknown action"-denied (bughunt H7 class).
- Naming and args follow the dsh `lsp` tool (packages/lsp/tool-lsp/src/index.ts):
  operation enum, file_path, one-based line, one-based UTF-16 character. The
  master contract C8 names the tool `lsp_navigate{query/op}`; we reconcile to
  dsh's concrete positional args (operation/file_path/line/character) - see
  the parent's Notes.

## Verified Anchors

- internal/code/lsp/client.go:282/308/331/461 - GotoDefinition / FindReferences / Hover / TypeDefinition (no GotoImplementation)
- internal/code/lsp/client.go:49 - Start
- internal/code/lsp/manager.go:52/67/86/111/265 - Manager seams
- internal/tools/builtin/lsp_writethrough.go:81 - NewLSPWriteNotifier (Manager reuse)
- internal/agent/executor.go:404-407 - ToolActionMap lsp_* entries (dead; add lsp_navigate -> file_read)
- internal/tools/interface.go:24-68 - Tool interface + ToolDefaults
- pkgsecurity BuiltinRules / seed_rules.go - lsp tools already file_read per ToolActionMap; no new risk rule needed

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/code/lsp/client.go - ADD:
//  func (c *Client) GotoImplementation(ctx context.Context, uri string, line, char int) ([]Location, error)
//    // textDocument/implementation request; returns Locations.
// internal/tools/builtin/lsp_navigate.go (NEW):
//  Name() "lsp_navigate"
//  Parameters(): operation (enum goToDefinition|findReferences|
//        goToImplementation|hover, required), file_path (string, required),
//        line (int, one-based, required), character (int, one-based UTF-16,
//        required), workspace (string, optional)
//  IsReadOnly(input) -> true
//  TimeoutMs() time.Duration -> 60 * time.Second   // B1 declared budget
// Constants (package scope): defaultMaxLocations = 100,
//        defaultMaxResultChars = 16000, lspTimeout = 60s
// internal/agent/executor.go ToolActionMap: "lsp_navigate": ToolFileRead
```

### What This Leaf Consumes

- B1 (parent) - `TimeoutMs() time.Duration`; declare 60s on the tool.
- B2 (parent) - exact tool shape above. No signature invented beyond B2/B1.
- Consumers: 00T gate tests (T2/T2b) + tool-surface e2e + the 04 branch's
  Review Checklist (wiring requirement).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|--------------|
| T1 | Add Client.GotoImplementation | Modify internal/code/lsp/client.go; Modify internal/code/lsp/client_navigation_test.go (gate 00T T2) | `go test -p 2 ./internal/code/lsp -run 'GotoImplementation' -v` | FAIL: method undefined (gate test calls it) | PASS: method present, issues textDocument/implementation, returns []Location | Given a Client and a captured request; When GotoImplementation(uri,5,3) is called; Then it transmits textDocument/implementation and parses a Location response |
| T2 | Register lsp_navigate tool | Create internal/tools/builtin/lsp_navigate.go | `go build ./...` + `go test -p 2 ./internal/tools/builtin/lsp_navigate_test.go -v` | FAIL: tool not found / args unresolved | PASS: tool registered; args parsed per B2; four operations dispatch | Given args {operation:"goToDefinition",file_path:"a.go",line:5,character:1}; When Execute runs; Then parseLspArgs yields zero-based position {4,0} and the operation routes to GotoDefinition |
| T3 | Result caps + truncation | Modify internal/tools/builtin/lsp_navigate.go | as T2 | FAIL: unbounded results | PASS: locations capped at 100 with an omission marker; rendered text capped at 16000 chars | Given 150 locations; When rendered; Then 100 returned + truncation metadata (mirrors dsh formatLocations) |
| T4 | Workspace-required sentinel | Modify internal/tools/builtin/lsp_navigate.go | as T2 | FAIL: missing sentinel | PASS: unresolvable workspace returns a detectable error containing "workspace cwd"; isError true (mirrors tools.ErrNoWorkingDir stays) | Given no session working dir; When lsp_navigate executes; Then it fails with the workspace-required error, not a panic or empty guess |
| T5 | Capability guard + ToolActionMap entry | Modify internal/tools/builtin/lsp_navigate.go; Modify internal/agent/executor.go (ToolActionMap :404) | `go test -p 2 ./internal/agent -run ToolActionMap -v` | FAIL: lsp_navigate still unknown-action denied | PASS: `lsp_navigate`: ToolFileRead mapped; when the server capability lacks the request (e.g. no implementationProvider) the tool returns a clear "not supported by server" result, never a hang | Given the tool called through the executor; When permission checks run; Then it is granted as file_read; a goToImplementation on an incapable server returns the explicit unsupported message |

**Worked example - T2 (canonical row expansion):**

T2: Register lsp_navigate and route the four operations.
1. Confirm the committed gate test (internal/tools/builtin/lsp_navigate_test.go) parses `{operation,file_path,line,character}` and expects dispatch to the matching client call:
2. Implementation - `internal/tools/builtin/lsp_navigate.go`:
```go
type lspNavigateTool struct {
    tools.ToolDefaults
    manager *lsp.Manager
    languageID func(filePath string) string  // extension -> language mapping seam
}
const (
    defaultMaxLocations   = 100
    defaultMaxResultChars = 16000
    lspTimeout            = 60 * time.Second
)
func (t *lspNavigateTool) Name() string { return "lsp_navigate" }
func (t *lspNavigateTool) TimeoutMs() time.Duration { return lspTimeout }
func (t *lspNavigateTool) IsReadOnly(map[string]any) bool { return true }
func (t *lspNavigateTool) Execute(ctx, args) (any, error) {
    // parse -> resolve workspace (session working-dir resolver; error if unresolvable)
    // srv, _ := t.manager.GetServerForLanguage(ctx, t.languageID(filePath))
    // switch operation { goToDefinition: c.GotoDefinition; ... }
    // render capped locations/hover; return map[string]any
}
```
   Wire it through the builtin registration container (follow the adjacent
   builtin's outermost factory, e.g. the writethrough notifier's wiring path),
   passing the existing Manager so a server spawned for writethrough is reused.
3. Verify: `go test -p 2 ./internal/tools/builtin -run Navigate -v` -> PASS; `go build ./...` clean.

## Self-Verification Checklist

- [ ] All tasks implemented and gate tests pass GREEN (00T T2/T2b + e2e tool-surface LSP case)
- [ ] Interface contract B2 satisfied exactly (lsp_navigate, 4 ops, caps, TimeoutMs 60s, workspace sentinel)
- [ ] Additive GotoImplementation present on the Client; no existing method changed
- [ ] ToolActionMap has `lsp_navigate` -> file_read; no other tool registered
- [ ] No gate test deleted/weakened; gate-test diff empty
- [ ] No deviations (or documented in the report)
- [ ] No scope creep - no new engines, no computer-use, no browser changes

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [the verify commands in Tasks; last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests passing and untouched
- [ ] B2 matches exactly (signatures, types, paths); client only gained GotoImplementation
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption
- [ ] Tool is read-only + TimeoutMs declared; workspace-required sentinel is detectable (errors.Is-compatible)

Output: APPROVED or specific gaps with file + line references.

## Notes

- dsh result caps and the workspace-required error are the frozen contract
  reference (packages/lsp/tool-lsp/src/index.ts): DEFAULT_MAX_LOCATIONS=100,
  DEFAULT_MAX_RESULT_CHARS=16000, "the lsp tool requires a session workspace
  cwd" (LSP_WORKSPACE_REQUIRED), and guidance text positions LSP as precision
  aid. Port the caps + message; adapt the sentinel to meept's ErrNoWorkingDir
  conventions (keep `errors.Is` routeable).
- Position conversion: one-based UTF-16 -> zero-based (line-1, char-1). Off-symbol
  positions are allowed to return no results (dsh behavior) - do NOT error them.
- findReferences semantics: dsh says findReferences always includes the
  declaration. meept's Client.FindReferences already takes `includeDecl` - pass
  true.
- The e2e suite may configure gopls OR a stub LSP stdio server. Prefer reusing a
  server the harness already has running for writethrough; document which binary
  the suite selected.
