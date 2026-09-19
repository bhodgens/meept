# Tool Arg Boundary Validation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat. After
> writing a file, do NOT read it back to verify. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A registry-level argument validation gate: every tool's declared `Required` schema becomes server-enforced at `Registry.Execute`, with typed errors naming the argument.
- **Dependencies:** none
- **Estimated Context:** ~55K (exploration 20K + generation 20K + iteration 10K + overhead 5K)
- **Concurrency Group:** A
- **Audit references:** 2026-09-18 e2e (45x `task_create{}` with per-call "name is required" errors; schema `Required:["name"]` unenforced)

## Goal

Today each builtin tool hand-rolls its own required-arg checks (or omits
them). The declared schema (`Parameters().Required`) is advisory: a model that
sends `{}` gets a hand-written error string at best, silent no-op at worst,
and there is no single place that guarantees the boundary. This leaf makes the
schema binding: `Registry.Execute` validates args against the tool's own
declared schema BEFORE `tool.Execute`, returning a typed
`ArgValidationError` whose message names the argument and the expected shape.

## Context

Meept is a Go agent daemon. Tools implement `internal/tools.Tool`:

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() llm.FunctionParameters  // Type, Properties map[string]ParameterProperty, Required []string
    Execute(ctx context.Context, args map[string]any) (any, error)
}
```

`ParameterProperty` is `internal/llm` (check its exact field set: Type string,
Description string, and possibly Items/Enum). The registry lives in
`internal/tools/registry.go` (`func (r *Registry) Execute(...)` at ~:297).
`NewErrorResult`/`NewErrorResultErr` produce `*ToolResult` error envelopes.
Tools may return `(any, error)` or a `*ToolResult` directly - validation runs
before any of that.

Key files:
- `internal/tools/registry.go` - Execute seam; add validation at its top
- `internal/tools/tool.go` (or wherever the Tool interface lives - find it) - interface
- `internal/tools/builtin/task.go` - task_create (the e2e offender); has `Required: []string{schemaPropName}` and a hand-rolled `name is required` check at :68
- `internal/llm/` - FunctionParameters/ParameterProperty definitions

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/tools/argvalidate.go
package tools

type ArgValidationError struct {
    Tool    string // tool name
    Arg     string // schema name of the offending argument
    Problem string // "missing" | "empty" | "wrong type: want string, got number"
}

func (e *ArgValidationError) Error() string
// Error(): "<Tool>: <Arg> is <Problem> (required by the tool schema; pass it exactly as named)"

func ValidateToolArgs(tool Tool, args map[string]any) error
```

Wire behavior in `Registry.Execute`: on validation error, return
`NewErrorResultErr(err), nil` (the existing error-envelope path) AND attach
`ErrCode: "invalid_args"` on the ToolResult if the envelope supports error
codes (check `ToolResult` fields; if there is no code field, add one with
zero-value empty for other paths - keep the struct additive).

### What This Leaf Consumes

- `Tool.Parameters()` schema (existing)
- `Registry.Execute` (existing; you modify its top)

## Tasks

### Task 1: ValidateToolArgs core

**Objective:** Pure function validating args against a tool's declared schema.

**Files:**
- Create: `internal/tools/argvalidate.go`
- Test: `internal/tools/argvalidate_test.go`

**Step 1: Write failing tests** (table-driven; use a fake tool):

```go
func fakeToolWith(required []string, props map[string]llm.ParameterProperty) Tool { ... }

func TestValidateToolArgs_MissingRequired(t *testing.T) {
    tool := fakeToolWith([]string{"name"}, map[string]llm.ParameterProperty{"name": {Type: "string"}})
    err := ValidateToolArgs(tool, map[string]any{})
    var ae *ArgValidationError
    require.ErrorAs(t, err, &ae)
    assert.Equal(t, "name", ae.Arg)
    assert.Equal(t, "missing", ae.Problem)
}

func TestValidateToolArgs_EmptyString(t *testing.T) {
    // args{"name": "  "} -> Problem "empty"
}
func TestValidateToolArgs_NilValue(t *testing.T) { /* Problem "missing" */ }
func TestValidateToolArgs_WrongType(t *testing.T) {
    // declared string, got float64 -> Problem "wrong type: want string, got number"
}
func TestValidateToolArgs_TypedNilString(t *testing.T) { /* typed-nil guard: interface holding (*string)(nil) -> "missing" */ }
func TestValidateToolArgs_ArrayAndObjectTypes(t *testing.T) { /* []any ok; map[string]any ok */ }
func TestValidateToolArgs_NoRequiredPasses(t *testing.T) { /* empty args, no Required -> nil */ }
func TestValidateToolArgs_ExtraKeysAllowed(t *testing.T) { /* forward-compat */ }
```

**Step 2:** Run `go test ./internal/tools/ -run TestValidateToolArgs -v` - FAIL (undefined).

**Step 3: Implement.** Rules:
- For each `Required` name: key must exist, value non-nil, strings must be
  non-empty after TrimSpace; type must match declared property type
  (`string`→string, `number`→float64, `boolean`→bool, `array`→[]any,
  `object`→map[string]any). JSON numbers arrive as float64.
- Two-value type assertions everywhere (AGENTS.md).
- Typed-nil guard: `val == nil` is insufficient for typed nils in `any` - use
  reflect for the emptiness check on non-basic kinds, mirroring the typed-nil
  pattern in internal/tools/builtin/json_extract.go.

**Step 4:** Tests PASS.

### Task 2: Wire into Registry.Execute

**Objective:** Every tool call passes the gate.

**Files:**
- Modify: `internal/tools/registry.go` (top of `Execute`, ~:297)
- Test: `internal/tools/registry_argvalidate_test.go`

**Step 1: Failing test:**

```go
func TestExecute_RejectsMissingRequiredBeforeToolRun(t *testing.T) {
    reg := NewRegistry(...)
    reg.Register(&countingFakeTool{...}) // Execute increments a counter
    res, err := reg.Execute(ctx, "fake", map[string]any{})
    require.Nil(t, err)           // error goes in the envelope, not the error return
    require.NotNil(t, res)
    assert.Equal(t, "invalid_args", res.ErrCode) // add field if absent
    assert.Contains(t, res.Error, "name is missing")
    assert.Equal(t, 0, fake.runCount) // tool NEVER ran
}

func TestExecute_ValidArgsRunNormally(t *testing.T) { /* counter==1, no error envelope */ }
```

**Step 2:** FAIL. **Step 3:** Insert `if err := ValidateToolArgs(tool, args); err != nil { log + return NewErrorResultErr(err), nil }` at the top of Execute (after the tool-nil check, before the Debug log is fine - keep the log accurate). Add `ErrCode string` to ToolResult if absent (additive; zero value for all existing constructors). **Step 4:** PASS.

### Task 3: Sweep the builtin registry for latent boundary gaps

**Objective:** Find tools whose Execute would misbehave on missing required
args today; do NOT rewrite their internals - the registry gate now covers
them. Only fix hand-rolled checks that RETURN WRONG SHAPES (e.g. silently
returning success on empty required arg).

**Files:**
- Test: `internal/tools/builtin/arg_boundary_sweep_test.go` (new)

**Step 1:** Write a sweep test: for a curated table of {tool, required arg,
minimal valid args}, assert each tool executes with valid args and rejects
with an error envelope on empty-required-args. Cover at least: task_create,
task_get, task_update, file_write, file_edit, web_search (query), json_extract
(text or file_path - one required), cron_create (name). Discover each tool's
real Required list via `reg.Get(name).Parameters()` so the sweep stays honest.

**Step 2:** Run - note any tool that SUCCEEDS with missing required args
(those are latent silent no-ops: fix by adding Required to its schema if the
arg is truly required, NOT by weakening the gate). **Step 3:** Apply minimal
schema fixes (expected: 0-3 tools). **Step 4:** All green.

### Task 4: task_create regression pin

**Objective:** The e2e offender gets a named pin.

**Files:**
- Modify: `internal/tools/builtin/task.go` test file `task_test.go` (or create)

**Step 1:** `TestTaskCreate_InvalidArgsRejectedAtBoundary`: registry-level
execute of task_create with `{}` → error envelope mentions `name is missing`,
zero tasks created in the store. **Step 2:** FAIL if envelope shape wrong;
**Step 3:** trivial (gate already covers); **Step 4:** PASS.

## Self-Verification Checklist

- [ ] All tasks implemented; `go test -p 2 -count=1 ./internal/tools/...` green
- [ ] `go build ./...` clean; gofmt clean on touched files
- [ ] Contract 1 signatures exact
- [ ] No per-tool behavior changed except documented schema fixes (Task 3)
- [ ] No scope creep

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] ValidateToolArgs handles all 5 JSON types + typed-nil + empty-string
- [ ] Registry.Execute never invokes the tool on validation failure
- [ ] ErrCode "invalid_args" present on the envelope (additive field)
- [ ] Task 3 sweep table uses each tool's REAL schema
- [ ] Pins green verbosely

Output: APPROVED or specific gaps with file:line.

## Notes

- `task_create`'s own `name is required` check at task.go:68 becomes
  defense-in-depth; leave it (removing it would churn sibling tests).
- The e2e offender repeated the call because nothing structural stopped it -
  the loop-side breaker is leaf 02's job, NOT this leaf. Do not add
  loop-level logic here.
