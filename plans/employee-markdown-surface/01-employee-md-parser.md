# 01 — Employee Markdown Parser (`internal/employeemd`)

## Dispatch Instruction

Do NOT commit. Do NOT run git add. Write the Go package, run the tests (the 00T e2e suite is the gate — run it after CLI wiring lands, but compare against your own unit-check via a scratch `go build`), report results. The orchestrator reviews and commits.

## Objective

Create a new package `internal/employeemd` that parses an `EMPLOYEE.md` file (YAML frontmatter + markdown body) and returns a `map[string]any` byte-compatible with the payload `agents.create` / `agents.update` / `agents.validate` expect. No daemon, no DB — pure parse-to-map.

## Files

- Create: `internal/employeemd/md.go` (all parser logic + exported funcs)
- Create: `internal/employeemd/md_test.go` (helper smoke test — NOTE repo policy: new feature tests go in e2e; include a minimal parse test here ONLY as a local dev aid, gate verification stays in the 00T e2e suite)
- Create: `internal/employeemd/README.md` (short: format reference)

## Contract (from master.md — frozen, do not change)

Read master.md's "Interface Contract (frozen — all leaves build to this)" section. It is the sole binding spec. Key mappings you must produce:

**Frontmatter → payload map (JSON keys, matching bot types):**

```
id              -> "id"           (required, string)
name            -> "name"
description     -> "description"
model           -> "model"
memory_scope    -> "memory_scope"   ("private"|"shared"|"read_only")
tools           -> "tools"          ([]string)
additional_tools-> (merged into "tools")
constraints     -> "constraints"    (map; see below)
triggers        -> "triggers"       ([]map: {type, schedule?, topic?, prompt_template?, enabled})
enabled         -> "enabled"        (bool)
executor_agent  -> "executor_agent"
constitution    -> "constitution"   (nested map, passed through VERBATIM)
goals           -> "goals"          ([]map, passed through)
```

`constraints` sub-keys → `BotConstraints` JSON:
- `max_iterations` int → `"max_iterations"`
- `timeout_seconds` int → `"timeout"` as `time.Duration` integer nanoseconds (`time.Duration(secs)*time.Second`)
- `max_tokens_per_turn` int → `"max_tokens_per_turn"`
- `daily_budget_cents` int → `"daily_budget_cents"`
- `max_invocations_per_day` int → `"max_invocations_per_day"`

Agent-style top-level aliases (folded into `constraints` only when the canonical `constraints` map does not already set that key):
- `max_iterations` (top-level int) → `constraints.max_iterations`
- `timeout_seconds` (top-level int) → `constraints.timeout`

**Body:** entire markdown after closing `---` → `"prompt"`, `strings.TrimSpace`.

**Unknown top-level frontmatter keys** are PRESERVED in the output map (pass-through for forward compatibility).

## Public API

```go
package employeemd

// Parse parses an EMPLOYEE.md file's YAML frontmatter + body into a
// map[string]any whose JSON encoding is byte-compatible with the
// agents.create / agents.update / agents.validate payload. The markdown
// body becomes "prompt". id is required.
func Parse(data []byte) (map[string]any, error)
```

- `data` is the raw file bytes. Return `errors.New` with a clear message when `id` is missing or the frontmatter fails YAML decode. Do NOT validate the constitution (Manager.Hire + validate command own that).
- Reuse the frontmatter-splitting approach from `internal/agents/parser.go` (`splitFrontmatter`, lines 88-145) — copy that logic, do not import `internal/agents` (it is for agent roster discovery, unrelated domain; duplication is deliberate and small).
- Use `gopkg.in/yaml.v3` for frontmatter decode (already a repo dependency). Decode into a `map[string]any`, then normalize keys (underscore keys are canonical).

## Hidden details / tests you must account for

- `timeout_seconds` → `"timeout"` must be an integer-nanosecond value, not a string like `"5m0s"`. The existing `BotConstraints.Timeout` is `time.Duration` with `json:"timeout"`; the RPC handler unmarshals it. Verify your map round-trips: `json.Marshal(m)` then `json.Unmarshal` into `bot.BotConstraints` yields `Timeout == 300*time.Second`. Add a quick local check in md_test.go for this exact round-trip.
- A frontmatter with a `constitution` containing a `constraints` sub-map (its own `risk_ceiling`, `max_tokens_per_turn`) must pass that map through untouched — do NOT merge it with the top-level employee `constraints`.

## Self-Verification

- [] `gofmt -l internal/employeemd/` clean.
- [] `go build ./internal/employeemd/` succeeds.
- [] `go test ./internal/employeemd/ -run . -count=1` passes the smoke tests.
- [] `json.Marshal` of your output for the `companion.md` fixture decodes into `bot.BotConstraints` with `Timeout == 300_000_000_000` ns.

## Review Checklist

- Parser output map is byte-compatible with the existing JSON5 path (compare field names to bot.BotDefinition JSON tags).
- No import of `internal/agents`; no DB/daemon touches.
- Pass-through of unknown keys and nested constitution is exact.
- No `time.Now().UnixNano()` id generation; no ignored errors.
