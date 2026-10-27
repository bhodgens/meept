# [intake] Config Block — Implementation Leaf (04)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed — make them pass; do NOT
> weaken them. Do NOT commit. Explore with search_files or terminal cat,
> not read_file. After writing a file, do NOT read it back.

## Meta

- **Parent:** master.md
- **Scope:** `[intake]` block in internal/config/schema.go + the json5
  duration-loader interplay.
- **Dependencies:** 02, 03 (their gate tests reference the config only
  via overlays; the schema must exist before wiring)
- **Estimated Context:** 45K
- **Concurrency Group:** C

## Goal

Make intake configurable (opt-in), and survive the config loader's
duration-rewrite pass without the type corruption the llm-resilience
forest hit (`queue.interactive_window` class).

## Context

Config schema: internal/config/schema.go (see the `[github]` block,
added by #64 — GitHubConfig at schema.go:134, root field at :120, with
//gendoc: section markers). The json5 loader rewrites quoted
duration-looking values to ns integers (grep `quotedDurationToNanos` in
internal/config/) with a key-aware exemption set. Contract 4 pins
PollInterval as a STRING field — it MUST join the exemption set, with a
two-sided regression test.

Key files:
- internal/config/schema.go — struct pattern + gendoc markers
- internal/config/ — the loader + its duration rewrite + exemption set
- internal/config/*_test.go — existing round-trip test patterns

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract 4 verbatim: `IntakeConfig` with all seven fields + the root
`Intake IntakeConfig \`json:"intake"\`` field, gendoc markers matching
the GitHubConfig pattern:
`//gendoc:section intake` + `//gendoc:desc` + `//gendoc:example`.

### What This Leaf Consumes

```
internal/config loader + exemption set (existing)
```

## Tasks

### Task 1: IntakeConfig + root field (F6)

**Objective:** the config block parses.

**Files:**
- Modify: `internal/config/schema.go` (add the struct + root field,
  gendoc markers, field comments: Enabled default false; DefaultEmployee
  REQUIRED when enabled; WatchRepo "owner/repo" format)
- Test: gate `TestIntakeConfig_Parse`

Also add a `Validate()`-style check helper where the daemon wiring will
call it (leaf 05): if `Enabled && (DefaultEmployee == "" || WatchRepo
== "")` -> error naming both missing fields. Implement as
`func (c IntakeConfig) ValidateEnabled() error` on the config type
(test it in the same gate test file additively if the gate test
doesn't already cover it — additive only, never weaken).

Run: `go test ./internal/config/ -run TestIntakeConfig -v` -> PASS.

### Task 2: duration-loader round-trip (F6)

**Objective:** `poll_interval = "5m"` survives load/save/load.

**Files:**
- Modify: the loader's key-aware duration-rewrite exemption set (add
  `intake.poll_interval`; follow the existing exemption registration
  pattern — grep the `queue.interactive_window` precedent)
- Test: gate `TestIntakeConfig_DurationRoundTrip` (both sides: the
  exempt key stays a string; a real duration field still converts)

Run: `go test ./internal/config/ -run TestIntakeConfig -v` -> PASS.

### Task 3: config template

**Objective:** the shipped template documents the opt-in block.

**Files:**
- Modify: `config/meept.json5` — commented `[intake]` example block
  (same commented-JSON5 style as the `[github]` example at :1200-1211),
  opt-in, no secrets.

## Self-Verification Checklist

- [ ] Gate test F6 PASS; round-trip covers BOTH sides
- [ ] `go build ./...` clean; `go vet ./internal/config/` clean
- [ ] gendoc markers present (docs-check depends on them)
- [ ] Config template updated (opt-in, commented)
- [ ] gofmt clean; no unused code

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Contract 4 fields verbatim (names, json tags, defaults in comments)
- [ ] Loader exemption added; regression test pins both sides
- [ ] gendoc section renders (run `make docs-generate` if mage is local,
      else note it for the integration phase)
- [ ] Template block commented out (opt-in)

## Notes

- Do NOT touch any other config section; this is an additive schema
  change. Sibling sessions may hold schema.go — patch surgically on
  unique anchors.
