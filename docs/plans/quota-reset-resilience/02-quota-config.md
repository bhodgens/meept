# Quota retry config - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** `llm.quota_retry` config section: struct, defaults, JSON5
  parsing, validation, and a template entry in `config/models.json5`.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A

## Goal

Users tune the quota-resilience posture without touching code: enable/disable,
bound waits (MaxWait), set the default estimate for unknown reset times, and
set the deferral poll cadence. All behavioral settings live in config (never
env vars, per AGENTS.md).

## Context

Meept config is JSON5 in `~/.meept/meept.json5`, parsed into structs in
`internal/config` (find the LLM section struct with search_files — look for
the type holding `llm.budget` / `llm.budget.hourly_token_limit` fields;
`BudgetConfig` lives in `internal/llm/budget.go` but the config-side mirror
is in `internal/config`). Templates in `config/` are copied on `make install`.
JSON5 with quoted keys (hujson rejects unquoted).

Key files to understand before implementing:

- `internal/config/` — the config root struct and the LLM sub-struct
  (search for existing `llm.` sections like budget/timeout to mirror style).
- `config/models.json5` — model/provider template; add the documented
  section here so `make install` users see it.
- `docs/configuration/` — where config reference docs live (leaf 10
  consolidates docs, but add the template now).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/config (the file holding the LLM config struct)
package config

type QuotaRetryConfig struct {
    Enabled            bool          // default true
    MaxWait            time.Duration // default 24h
    DefaultEstimate    time.Duration // default 1h
    DeferCheckInterval time.Duration // default 10m
}

// Field on the existing LLM config struct:
//   QuotaRetry QuotaRetryConfig `json:"quota_retry"`
// Config key: llm.quota_retry with quoted JSON5 keys:
//   "llm": { "quota_retry": { "enabled": true, "max_wait": "24h",
//            "default_estimate": "1h", "defer_check_interval": "10m" } }
// Durations parse via the same helper existing duration fields use
// (VERIFY: find how existing "24h"-style fields parse — reuse it).
// Validation: negative values clamp to defaults; zero values take defaults
// (distinguish "unset" from explicit zero: if the existing config pattern
// uses pointers for optional fields, mirror it — check first).
```

### What This Leaf Consumes

Nothing — leaf 01/03/04/05/06 read the struct after you define it.

## Tasks

### Task 1: QuotaRetryConfig struct + defaults + parse

**Objective:** Add the struct to the LLM config, wire JSON5 parsing with
defaults applied on unset, validate/clamp negatives.

**Files:**
- Modify: the `internal/config` file holding the LLM section (exact path
  discovered during exploration; report it)
- Test: `internal/config/quota_retry_test.go`

**Step 1: Write failing test**

Table-driven: empty config -> all defaults; explicit values -> honored;
negative MaxWait -> clamped to default; `max_wait: "48h"` parses to
48*time.Hour. Also a JSON5 fixture string parsed through the existing
config-loading path (quoted keys) end-to-end.

**Step 2: Run test to verify failure**

Run: `go test ./internal/config/ -run TestQuotaRetryConfig -v`
Expected: FAIL (undefined: QuotaRetryConfig)

**Step 3: Write minimal implementation**

Follow the existing pattern for duration fields in the LLM section exactly
(field tags, parse helper, where defaults get applied).

**Step 4: Run test to verify pass**

Run: `go test ./internal/config/ -run TestQuotaRetryConfig -v`
Expected: PASS

### Task 2: Template + plumbing

**Objective:** Template visibility and config plumbing to consumers.

**Files:**
- Modify: `config/models.json5` (add commented `"quota_retry"` example with
  quoted keys inside the `"llm"` section if present there; if the llm
  section lives in `config/meept.json5` template instead, modify that one —
  verify which template carries the `llm` section)
- Test: existing config template tests if any assert template validity
  (run them); otherwise extend Task 1's fixture test to load the template
  file itself.

**Step 1: Write failing test**

Assert the template file parses and the quota_retry section (commented or
active) doesn't break loading. If the template ships the section active,
assert defaults parse correctly from it.

**Step 2: Run test to verify failure**

Run: `go test ./internal/config/ -run TestQuotaTemplate -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add the template section.

**Step 4: Run test to verify pass**

Run: `go test ./internal/config/ -run 'TestQuotaRetryConfig|TestQuotaTemplate' -v`
Expected: PASS

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] JSON5 keys quoted; no env vars introduced

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (field names, types, defaults)
- [ ] Defaults: enabled=true, max_wait=24h, default_estimate=1h,
      defer_check_interval=10m
- [ ] Negatives clamped; unset takes defaults
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Consumers (leaves 03-06) read this struct via the daemon's config object.
  Do NOT wire it into the LLM clients/broker yourself — that happens in the
  consuming leaves. Your deliverable is config + tests + template.
- If the existing config uses a different duration-string convention
  (e.g. integer seconds), follow the existing convention and document the
  deviation — consistency beats the contract's example strings.
