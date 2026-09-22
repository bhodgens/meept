# Config + CLI + Docs - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify - write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Add the `output_filters` config schema with daemon->agent override chain, expose it via `meept config`/`agents show`, wire the config snapshot into TaskService, and document the feature (docs/workflows + README section).
- **Dependencies:** 01-filter-interface.md, 03-gate-wiring.md (needs the limiter seam + setter to exist)
- **Estimated Context:** 55K (exploration 15K + generation 18K + iteration 12K + overhead 10K)
- **Concurrency Group:** C

## Goal

Make the filter stage configurable and observable: a config key users can set
(mirroring the verification-config override chain), CLI visibility, and docs
that state the frozen gate order and the independent retry cap.

## Context

The daemon wires TaskService at startup; per-agent overrides follow the
verification-config pattern (daemon defaults -> agent AGENT.md front matter ->
runtime metadata; see `internal/agent/verification_config.go` and
`internal/config/schema.go` `VerificationDefaults`). Leaf 03 left a
`filterRetryLimiter` seam reading `MaxFilterRetries()` - this leaf replaces
the default with the real config snapshot.

Key files to understand before implementing:
- `internal/agent/verification_config.go` - the override-chain pattern to mirror
- `internal/config/schema.go` - where `VerificationDefaults` lives; add `OutputFiltersDefaults` beside it
- `internal/agent/registry.go` (`verificationFromMetadata` call site) - where runtime agent metadata conversion happens
- `cmd/meept/config_cmd.go` and `cmd/meept/agents_cmd.go` - CLI display patterns (`agents show` renders the Verification: section; mirror it)
- `docs/workflows/` - feature spec docs live here (one per internal package topic)
- `README.md` - "Post-Step Validation Pipeline" section (authored separately before this leaf) - amend it, do not rewrite it

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go - NEW:
type OutputFiltersConfig struct {
    Enabled          bool     `json:"enabled"`
    MaxPasses        int      `json:"max_passes"`         // rewrite sweeps; 0 -> default 2
    MaxFilterRetries int      `json:"max_filter_retries"` // rejections; 0 -> default 2
    Filters          []string `json:"filters"`            // builtin names, ordered
}
// Daemon defaults field beside VerificationDefaults, wired in DefaultConfig():
// Enabled: false (ZERO behavior change until opted in - frozen default),
// MaxPasses: 2, MaxFilterRetries: 2, Filters: [].

// internal/agent/filter_config.go - NEW (mirrors verification_config.go):
type FilterConfig struct { Enabled bool; MaxPasses int; MaxFilterRetries int; Filters []string }
func DefaultFilterConfig() FilterConfig
func (fc FilterConfig) MaxFilterRetriesOrDefault() int   // satisfies leaf 03's limiter seam
func EffectiveFilterConfig(daemon, agent FilterConfig) FilterConfig
// Override chain: daemon defaults -> agent AGENT.md front matter
// (output_filters key) -> runtime agent metadata. Scalar fields: agent wins
// when explicitly set; Filters: agent list REPLACES daemon list when non-empty.

// Config snapshot wiring: TaskService.SetFilterChain is constructed at daemon
// startup from the effective config; per-agent rebuild follows the registry
// spec-conversion path.
```

Config shape (user-facing, frozen in parent Contract 6):
```json5
"output_filters": {
    "enabled": true,
    "max_passes": 2,
    "max_filter_retries": 2,
    "filters": ["json_format", "language_en"]
}
```

### What This Leaf Consumes

```
// From 03-gate-wiring.md:
TaskService.SetFilterChain(fc *validator.FilterChain)   // nil-guarded
type filterRetryLimiter interface { MaxFilterRetries() int } // seam to satisfy
// From 01-filter-interface.md + 02-builtin-filters.md:
validator.NewFilterChain(filters, maxPasses)
validator.NewBuiltinFilter(name, cfg) (OutputFilter, error)
```

## Tasks

### Task 1: Config schema + defaults

**Objective:** Add `OutputFiltersConfig` to the config schema with the frozen
disabled default.

**Files:**
- Modify: `internal/config/schema.go`
- Test: `internal/config/schema_test.go` (extend)

**Step 1: Write failing test:** DefaultConfig() carries
`OutputFilters.Enabled == false`, `MaxPasses == 2`, `MaxFilterRetries == 2`,
`Filters == nil`; a JSON5 config body with the key parses into the struct.

**Step 2:** `go test -p 2 ./internal/config/ -run TestOutputFilters -v` -> FAIL
**Step 3:** implement
**Step 4:** PASS

### Task 2: EffectiveFilterConfig override chain

**Objective:** Daemon -> agent override resolution mirroring verification config.

**Files:**
- Create: `internal/agent/filter_config.go`
- Test: `internal/agent/filter_config_test.go`

**Step 1: Write failing test** - table-driven:
- daemon-only: agent zero-value -> daemon values (+OrDefault applied)
- agent override: agent Enabled true + Filters non-empty -> agent wins
- agent partial: agent sets only max_passes -> other fields from daemon
- MaxFilterRetriesOrDefault(): 0/negative -> 2 (the seam default leaf 03 reads)

**Step 2:** FAIL **Step 3:** implement **Step 4:** PASS

### Task 3: Daemon wiring of the chain from config

**Objective:** Startup builds a FilterChain from effective config and hands it
to TaskService via SetFilterChain.

**Files:**
- Modify: `internal/daemon/components*.go` (where TaskService is constructed -
  locate via search_files for `ValidatorManager` wiring, which is the
  adjacent precedent)
- Test: `internal/daemon/filter_wiring_test.go` (new file)

**Step 1: Write failing test:** with config enabled + filters
["json_format"], the TaskService handed to the service layer has a non-nil
chain whose filter set matches; with disabled, holder is nil (or chain nil)
and SetFilterChain is skipped.

**Step 2:** FAIL **Step 3:** implement (guard: typed-nil; follow the
validatorManager != nil precedent at tactical.go:1186)
**Step 4:** PASS

### Task 4: CLI visibility

**Objective:** `meept config get output_filters` works; `agents show` renders
an `output filters:` section, lowercase values per UI conventions.

**Files:**
- Modify: `cmd/meept/agents_cmd.go` (Verification: section precedent)
- Test: `cmd/meept/agents_filter_display_test.go` (new file)

**Step 1: Write failing test:** the renderer that builds the agents-show
output includes `output filters: enabled, filters: json_format, language_en,
max passes: 2, retries: 2` for an agent with overrides, and `output filters:
off (daemon default disabled)` when disabled. Assert lowercase strings.

**Step 2:** FAIL **Step 3:** implement **Step 4:** PASS
Also verify manually: `go run ./cmd/meept config get output_filters` returns
the defaults without error.

### Task 5: Documentation

**Objective:** docs + README reflect the mechanism, order contract, caps.

**Files:**
- Create: `docs/workflows/output-filters.md`
- Modify: `README.md` (amend the "Post-Step Validation Pipeline" section)
- Modify: `AGENTS.md` (new cross-boundary invariant under Critical Invariants:
  the frozen gate order + independent retry cap - AGENTS.md maintenance rule
  requires this in the same change)

**Step 1 (docs content, verify-by-reading):** write `docs/workflows/output-filters.md`:
- What it is (milter-style pass/rewrite/fail content stage)
- Frozen pipeline order (the 6-stage list from master Contract 4)
- Tri-state semantics, MaxPasses convergence cap, idempotency requirement
- INDEPENDENT retry cap (max_filter_retries vs MaxValidationLoops - a table)
- Config reference (the JSON5 shape), per-agent override chain
- Builtin filter reference (json_format/language_en/lint_go + Reason formats)
- Observability: the exact slog keys logged per action

**Step 2:** amend the README "Post-Step Validation Pipeline" section: insert
the output-filter stage at position 3 with a 3-sentence description and link
to the new doc. Keep the existing layers' text intact.

**Step 3:** add the AGENTS.md invariant paragraph: gate order is frozen;
filter rejections use FilterRetryCount and never consume validation retries;
every action logs stage=output_filter. Keep it under 10 lines.

**Step 4 (verify):** cross-references resolve (README link -> doc path exists);
`make docs-check` NOT required (this doc is hand-written, not mage-generated) -
verify with `ls docs/workflows/output-filters.md` and a link grep.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing:
      `go test -p 2 ./internal/config/ ./internal/agent/ ./internal/daemon/ ./cmd/... -v`
- [ ] Default config disabled - zero behavior change (Task 1 test)
- [ ] Override chain matches verification-config semantics (Task 2 tests)
- [ ] CLI strings lowercase (Task 4 test asserts)
- [ ] docs/workflows/output-filters.md exists with all 7 sections
- [ ] README amendment inserts stage 3 without touching other layers' text
- [ ] AGENTS.md invariant added (under 10 lines)
- [ ] gofmt clean
- [ ] No deviations from spec (or documented below)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Default disabled; override chain order correct
- [ ] Config snapshot reaches TaskService (wiring test proves the chain is live)
- [ ] CLI output lowercase and complete
- [ ] Docs state the frozen order, both caps, and the log keys
- [ ] AGENTS.md invariant present and accurate
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- `config get/set` for nested keys may already be generic - if
  `config get output_filters` works without code changes, keep the manual
  verification output in your report instead of forcing a code change.
- The docs must NEVER present the filter stage as a review replacement: it
  handles mechanical content failures; ReviewStep and adversarial
  verification remain the judgment layers.
