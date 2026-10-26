# Wiring, Registration, Config, Docs — Implementation Leaf (05)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Gate tests EXIST and are committed — make them pass; do NOT weaken them.
> Do NOT commit. Explore with search_files or terminal cat, not read_file.

## Meta

- **Parent:** ../master.md
- **Scope:** `[github]` config block, daemon registration, ReArmSink impl,
  CLI surface, docs updates.
- **Dependencies:** 01, 02, 03, 04 (all tools exist)
- **Estimated Context:** 60K
- **Concurrency Group:** C

## Goal

Make the tools actually appear: config schema, pool construction from
config, registry wiring in the daemon, the real cron re-arm sink, and the
documentation the repo requires for feature changes (AGENTS.md rule:
feature docs mandatory; wiring requirement: no feature ships
internal-only).

## Context

The daemon registers builtin tools in internal/daemon/components.go
(search `NewGitCommitTool` at components.go:6699 for the pattern). Config
schema lives in internal/config/schema.go. The repo's doc mapping:
`internal/<pkg>/` -> `docs/workflows/<pkg>.md`.

Key files:
- internal/daemon/components.go (~6699) — registration pattern
- internal/config/schema.go — config structs
- internal/scheduler/instructions.go — instruction row creation
- docs/workflows/ — feature doc home

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/config/schema.go
type GitHubConfig struct {  // Contract 5 verbatim
    Enabled      bool   `json:"enabled"`
    TokenSecret  string `json:"token_secret"`
    DefaultOwner string `json:"default_owner"`
    DefaultRepo  string `json:"default_repo"`
    BaseURL      string `json:"base_url,omitempty"`
}
// + GitHub GitHubConfig `json:"github"` on the root config struct

// internal/githubsvc/rearm.go (extend from 03's stubs)
func NewInstructionReArmSink(store *preferences.Store) ReArmSink
// ScheduleReArm writes a preferences instruction row:
//   Trigger: "cron:<at minute>", Action: "agent_trigger",
//   ActionArgs: {"prompt": "resume github_ci_wait <waitID> ..."}
```

CLI (cmd/meept): `meept github status` (config/token/registry diagnostics,
report-only) — mirrors the doctor report-only convention.

### What This Leaf Consumes

```
All seven tool constructors (from 02/03/04)
githubsvc.RegisterGitHubTools(r, opts) error   (defined HERE, in
  internal/githubsvc/register.go — implements the signature the gate
  test pinned; constructs + registers all tools when cfg.Enabled)
```

## Tasks

### Task 1: Config + RegisterGitHubTools

**Objective:** F8 — registration honored, disabled by default.

**Files:**
- Modify: `internal/config/schema.go` (add GitHubConfig + root field)
- Create/Modify: `internal/githubsvc/register.go` (full impl)
- Test: gate `TestGitHubToolRegistration`

Default `enabled=false`. Pool built from TokenSecret + BaseURL. Also
update any config template under config/ if one lists tool toggles
(check `config/meept.json5` — add a commented `[github]` example, opt-in).

### Task 2: Daemon wiring + ReArmSink

**Objective:** tools live in the daemon registry; parked waits re-arm.

**Files:**
- Modify: `internal/daemon/components.go` (register when enabled)
- Modify: `internal/githubsvc/rearm.go` (InstructionReArmSink)
- Test: extend gate test file `github_register_test.go` (new test
  `TestInstructionReArmSink` writing to a temp store)

### Task 3: CLI + docs

**Objective:** user-facing surface per the wiring requirement.

**Files:**
- Create: `cmd/meept/github.go` (meept github status)
- Create: `docs/workflows/github.md` (overview/problem/behavior sections
  matching sibling workflow docs)
- Modify: `docs/reference/generated/` only via `make docs-generate` —
  do NOT hand-edit; run the target if it exists locally, else note it
- Modify: `AGENTS.md` package table row for internal/githubsvc (same
  commit rule)

Run: `go build -o bin/meept ./cmd/meept && ./bin/meept github status`
-> prints disabled/missing-token state cleanly, exit 0.

## Self-Verification Checklist

- [ ] Gate test F8 PASS; tools absent when disabled
- [ ] `go build ./...` clean; `go vet` clean
- [ ] Config template updated (opt-in, commented)
- [ ] docs/workflows/github.md complete; AGENTS.md table row added
- [ ] CLI compiles and runs report-only

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Registration gated on enabled flag everywhere (daemon + templates)
- [ ] No token printed by CLI or logs
- [ ] Docs present; AGENTS.md updated in same commit scope
- [ ] graphs: `make graphs` run if any HTTP/RPC surface changed

## Notes

- components.go is huge — patch surgically; do not reorder existing
  registrations.
- If `preferences.Store` instruction shape differs from the pinned guess,
  adapt to the real shape and record the deviation (gate test asserts
  sink behavior via the interface, not the row shape).
