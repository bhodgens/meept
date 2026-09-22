# Leaf 01 — Config knob: ClassifierConfig.session_state_upgrade

DISPATCH INSTRUCTION: Any agent may implement this leaf. Do NOT commit.
Do NOT run `git add`. Write code, run tests, report results. The
orchestrator handles all git operations.

**Parent:** `docs/plans/quickplan-session-upgrade/master.md`
**Scope:** Add the `orchestrator.classifier.session_state_upgrade` config
key (default false), template entry, and a defaults test. No dispatcher
changes.
**Dependencies:** none.
**Estimated context:** ~30K.

## Verified source facts (2026-09-21 — trust these over the leaf text)

- `internal/config/schema.go:2420` — `Prefilter ClassifierPrefilterConfig`
  field inside the orchestrator config struct.
- `internal/config/schema.go:2462` — `// ClassifierPrefilterConfig
  configures the Stage-0 embedding prefilter` block; field style:
  `VetoPath string json:"veto_path" toml:"veto_path"` (:2506).
- `internal/config/schema.go:3235` — `Prefilter: ClassifierPrefilterConfig{`
  inside the defaults function.
- `config/meept.json5:664` — `"classifier_prefilter": {` template block.

## Tasks

### Task 1 (RED first) — defaults test

Add to the existing config defaults test file (find it:
`grep -rln 'DefaultConfig' internal/config/*_test.go`) a test:

```go
func TestClassifierSessionStateUpgradeDefaultsOff(t *testing.T) {
    cfg := DefaultConfig()
    if cfg.Orchestrator.Classifier.SessionStateUpgrade {
        t.Fatal("session_state_upgrade must default to false (C3)")
    }
}
```

(Adapt the accessor path to the real struct nesting you find at
schema.go:2420 — if `Prefilter` sits on `cfg.Orchestrator`, the new
`Classifier` struct sits beside it on the same level.) Run it: it must
FAIL to compile (field missing) — that is the RED evidence. Record the
output.

### Task 2 (GREEN) — schema

In `internal/config/schema.go`, beside `ClassifierPrefilterConfig`
(~:2462), add:

```go
// ClassifierConfig configures the LLM intent-classifier ancillary gates
// that are not part of the Stage-0 prefilter. Session-state upgrade:
// one-way verdict upgrade to quickplan when the session holds an
// approved/executing plan or active tracked tasks (docs/plans/
// quickplan-session-upgrade/). Default false = byte-identical legacy
// behavior; the gate itself lives in internal/agent/session_state_gate.go.
type ClassifierConfig struct {
    // SessionStateUpgrade enables the one-way session-evidence upgrade
    // to quickplan at dispatch time. Session evidence is never a
    // classifier feature and never downgrades a verdict (C4).
    SessionStateUpgrade bool `json:"session_state_upgrade" toml:"session_state_upgrade"`
}
```

Add the field beside `Prefilter` at ~:2420:

```go
Classifier ClassifierConfig `json:"classifier" toml:"classifier"`
```

And in the defaults function beside the `Prefilter:` literal (~:3235):

```go
Classifier: ClassifierConfig{
    SessionStateUpgrade: false,
},
```

### Task 3 — config template

In `config/meept.json5`, inside the orchestrator block that holds
`"classifier_prefilter"` (:664), add a sibling block with a comment:

```
// one-way session-evidence upgrade to quickplan (approved plan / active
// tasks in session). default off — see docs/plans/quickplan-session-upgrade/
"classifier": {
    "session_state_upgrade": false,
},
```

Match the file's JSON5 style exactly (quote style, trailing commas —
read the surrounding block first).

### Task 4 — template-loading round-trip test

Extend the test from Task 1 (or a sibling test): marshal a DefaultConfig
to JSON5-Load round trip (find the existing pattern:
`grep -rn 'json.Marshal\|Load' internal/config/config_test.go | head`)
and assert the key round-trips. Also parse `config/meept.json5` with the
repo's own loader and assert `session_state_upgrade == false` — this
catches the wrong-nesting silent-default trap (a key at the wrong level
parses and invisible defaults apply).

## Interface Contract (what this leaf exposes)

- `ClassifierConfig.SessionStateUpgrade bool` at
  `Orchestrator.Classifier.SessionStateUpgrade` (or the actual nesting
  you find — REPORT the exact path; leaf 02 consumes it).
- Template key `orchestrator.classifier.session_state_upgrade`.
- Leaf 02 reads the path from this leaf's report — the report line
  `CONFIG PATH: cfg.<...>.Classifier.SessionStateUpgrade` is the
  contract.

## Self-Verification Checklist

- [ ] RED evidence captured (test fails to compile before schema change)
- [ ] GREEN: `go test ./internal/config/... -run SessionStateUpgrade -count=1` passes
- [ ] `go build ./internal/config/...` green; gofmt clean
- [ ] Template round-trip test passes (config/meept.json5 parses with the
      key at the correct nesting)
- [ ] No other config keys touched

## Review Checklist (orchestrator)

- [ ] Struct/field/defaults at the verified locations; no drive-by edits
- [ ] Default false in BOTH defaults function and template
- [ ] JSON tags match C3 (`session_state_upgrade`)
- [ ] `./bin/meept config get orchestrator.classifier.session_state_upgrade`
      returns false (build the binary if needed:
      `go build -o bin/meept ./cmd/meept`)

Suggested commit (orchestrator):
`git commit -m "feat(config): orchestrator.classifier.session_state_upgrade knob (default off)" -- <paths>`
