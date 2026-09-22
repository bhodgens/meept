# Output Filters

The output-filter stage is a milter-style pass/rewrite/fail content check
applied to every task step result before the result reaches the later gates.
It handles **mechanical content failures** — malformed JSON, wrong language,
unformatted Go code. It is never a replacement for review: `ReviewStep` and
adversarial verification remain the judgment layers.

- **Pass** — the result continues unchanged.
- **Rewrite** — the result is replaced by the filter's repaired form (logged;
  silent mutation is forbidden).
- **Fail** — the result is rejected; the step is requeued for rework up to
  `max_filter_retries`, then finalized as failed with the filter's reason.

## Frozen pipeline order

The post-step pipeline order in `internal/agent/tactical.go` is FROZEN — it
never swaps:

```
1. step job result arrives
2. claim-vs-evidence marking
3. OUTPUT FILTER CHAIN        <- this feature
4. evidence validation gate
5. ReviewStep policy/reviewer
6. adversarial verification
```

Filters run strictly before the evidence-validation gate: the filter chain
validates/repairs result **content**, the evidence gate validates
**side-effects**. Neither substitutes for the other.

## Convergence: MaxPasses

A *pass* is one sweep over all configured filters in declared order. A
rewrite restarts the sweep from the first filter with the new output.
`max_passes` bounds the sweeps: if the output still changes after N passes,
the chain fails with reason `filter chain did not converge after N passes`.
Default: 2.

## Idempotency requirement

Rewrite filters MUST be idempotent: `Process(p(x)) == Process(x)`. Canonical
input must be byte-identical to its own canonical form, so the second sweep
passes instead of looping to the convergence cap.

## Retry caps are independent

Filter rejections and validation failures use SEPARATE counters. A filter
rejection consumes a filter retry (`FilterRetryCount`, persisted) and never
consumes a validation retry — and vice versa.

| Failure class | Counter | Cap | Persisted |
|---------------|---------|-----|-----------|
| Filter rejection | `FilterRetryCount` (step column) | `max_filter_retries` (default 2) | yes |
| Validation failure | validation retry loops | `orchestrator.max_validation_loops` (default 3) | in-memory |

When `max_filter_retries` is exhausted the step finalizes **failed** with the
filter's reason as the error text; it never rides the validation-retry path.

## Configuration

Daemon level, `~/.meept/meept.json5`:

```json5
"daemon": {
    "output_filters": {
        "enabled":            false, // frozen zero-behavior default; opt in
        "max_passes":         2,     // rewrite sweeps before chain fail
        "max_filter_retries": 2,     // independent of validation retries
        "filters":            ["json_format", "language_en"]
    }
}
```

Inspect with the CLI:

```
meept config get daemon.output_filters
```

When the stage is disabled (the default) the completion path is
byte-identical to the pre-filter pipeline: zero invocations, zero filter log
lines.

## Per-agent override chain

Per-agent overrides mirror the verification-config chain: **daemon defaults
→ agent AGENT.md front matter (`output_filters` key) → runtime agent
metadata**. Semantics (`agent.EffectiveFilterConfig`):

- Scalars (`enabled`, `max_passes`, `max_filter_retries`): the agent value
  wins when explicitly set; absent falls through to the daemon value.
- `filters`: the agent list REPLACES the daemon list when non-empty; an
  empty agent list keeps the daemon list.

## Builtin filters

Builtins are constructed with `validator.NewBuiltinFilter(name, cfg)`. An
unknown name is a config error: the stage is disabled and the daemon logs a
warning naming the valid set.

### `json_format`

Parses output as JSON (applies to steps with a JSON-ish tool hint: `api`,
`api_call`, `curl`, `fetch`, `http`, `http_request`, `json`, `rest`,
`web_fetch`). On success reformats canonically (2-space indent, sorted keys,
trailing newline) as a rewrite; already-canonical input passes. Parse failure
reason format: the parser's error with byte offset, e.g.

```
invalid JSON at offset 42: unexpected token
```

### `language_en`

Script/stopword-based language detection (pure stdlib + bundled lists; no
network, no model call). Fails when the detected language is not the
expected one. Reason format:

```
lang=<code> confidence=<float>
```

e.g. `lang=fr confidence=0.87`.

### `lint_go`

Shells `gofmt -l` / `go vet` (advisory) on code-bearing outputs; applies
`gofmt -w` as a rewrite in a temp dir. Subprocess timeout 10s; a timeout
fails with reason `timeout`.

## Observability

Every stage outcome is logged with structured slog keys — a rejection that
does not name its stage in the log is a bug:

```
stage=output_filter  filter=<name>  pass=<n>
action=pass|rewrite|fail|rejected_exhausted
step_id=<id>  reason=<text on fail>
```

- `action=pass` — filter ran, changed nothing.
- `action=rewrite` — output replaced (silent mutation is forbidden).
- `action=fail` — rejection; requeue until the retry cap.
- `action=rejected_exhausted` — retry cap exhausted; step finalized failed.
