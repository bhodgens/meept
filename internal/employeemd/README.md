# internal/employeemd

Pure parser that converts an `EMPLOYEE.md` file (YAML frontmatter + markdown body)
into a `map[string]any` byte-compatible with the `agents create/update/validate`
RPC payload. No daemon, no DB, no registry.

## Public API

```go
func Parse(data []byte) (map[string]any, error)
```

The entire markdown body after the closing `---` becomes `"prompt"` (TrimSpace'd).
`id` is required.

## Frontmatter schema (YAML)

| Key | Maps to | Notes |
|-----|---------|-------|
| `id` | `id` | required, string |
| `name` / `description` / `model` | same | |
| `memory_scope` | `memory_scope` | `private`\|`shared`\|`read_only` |
| `tools` | `tools` | `[]string` |
| `additional_tools` | `tools` | merged into `tools` |
| `constraints` | `constraints` | see below |
| `triggers` | `triggers` | list of `{type, schedule?, topic?, prompt_template?, enabled}` |
| `enabled` | `enabled` | bool |
| `executor_agent` | `executor_agent` | |
| `constitution` | `constitution` | passed through verbatim (not validated here) |
| `goals` | `goals` | passed through |

### constraints sub-keys -> BotConstraints JSON

- `max_iterations` int -> `"max_iterations"`
- `timeout_seconds` int -> `"timeout"` as an integer nanosecond value
  (`time.Duration(secs) * time.Second`, e.g. 300 -> 300000000000)
- `max_tokens_per_turn` int -> `"max_tokens_per_turn"`
- `daily_budget_cents` int -> `"daily_budget_cents"`
- `max_invocations_per_day` int -> `"max_invocations_per_day"`

Agent-style top-level aliases `max_iterations` and `timeout_seconds` fold into
`constraints` only when the canonical `constraints` map does not set that key.

Unknown top-level frontmatter keys are preserved in the output (forward-compatible
passthrough). Scalar YAML types are preserved so `json.Marshal` emits numeric
values, not strings.

Constitution validation is left to `Manager.Hire` and `agents validate`.
