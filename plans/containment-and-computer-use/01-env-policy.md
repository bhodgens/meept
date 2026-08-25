# Env Policy (Child Environment Allowlist) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Replace os.Environ() inheritance in child command construction with an allowlist-based EnvPolicy, configurable via [runtime].
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Audit references:** parity-audit gap #3 (env inheritance); issue bhodgens/meept#25

## Goal

Every shell/file-exec spawned by meept currently receives the daemon's full environment (`internal/runtime/local.go:96-102 buildEnv()` seeds `os.Environ()`). Any secret exported into the daemon's process reaches agent-run shells. This leaf introduces `EnvPolicy` — an allowlist + deny-glob filter producing child environments — and rewires LocalBackend to use it. Default mode strips everything not explicitly allowed; a documented `inherit` mode preserves legacy behavior for users who need it.

## Context

Meept is a Go daemon (module github.com/caimlas/meept). Shell commands execute through `internal/runtime`: `backend.go` defines `Command{Cmd, Dir, Env map[string]string, Timeout, Interactive}` and the `ExecutionBackend` interface; `local.go` implements local exec and calls `buildEnv(cmd.Env)` which concatenates `os.Environ()` then applies overrides; `docker.go` passes only explicit cmd.Env. Config structs live in `internal/config/schema.go` (`RuntimeConfig` ~line 2359: Enabled, DefaultBackend, Docker) with JSON5+TOML tags; defaults registered near schema.go:1900. Daemon wires runtime in `internal/daemon/components.go` ~1648-1672.

Key files to understand before implementing:
- internal/runtime/local.go - buildEnv() you are replacing (lines 22-28 defaultEnv field, 54 call site, 96-102 implementation)
- internal/runtime/backend.go - Command/Config types this leaf extends
- internal/config/schema.go - RuntimeConfig struct + defaults block to extend
- internal/daemon/components.go - where runtime config flows from config into runtime.Config

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/runtime/envpolicy.go
package runtime

type EnvMode string

const (
    EnvModeAllowlist EnvMode = "allowlist"
    EnvModeInherit   EnvMode = "inherit"
)

type EnvPolicyConfig struct {
    Mode        EnvMode  `json:"env_mode"       toml:"env_mode"`
    Allowlist   []string `json:"env_allowlist"  toml:"env_allowlist"`
    DenyGlobs   []string `json:"env_deny_globs" toml:"env_deny_globs"`
}

// BaseEnvKeys always passed in allowlist mode.
var BaseEnvKeys = []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "TERM", "SHELL",
    "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}

// BuildChildEnv builds the child env slice. parentEnv is the daemon's captured
// environ (pass nil in tests). cmdEnv entries override/deny per same rules.
// Placeholder-form values (strings with prefix "MEEPT_SECRET:") pass through
// untouched. Returns stripped variable NAMES (allowlist mode only) for logging.
func BuildChildEnv(cfg EnvPolicyConfig, parentEnv []string, cmdEnv map[string]string) (env []string, stripped []string)
```

```go
// File: internal/runtime/backend.go (modify)
// Add to Config:
type Config struct {
    DefaultBackend string           `json:"default_backend"`
    Docker         DockerConfig     `json:"docker"`
    EnvPolicy      EnvPolicyConfig  `json:"env_policy"`   // NEW
}
```

LocalBackend stores its resolved EnvPolicyConfig at construction (via NewLocalBackend signature extension or new setter `SetEnvPolicy(EnvPolicyConfig)` WITH typed-nil-safe semantics — setter takes value type, no nil possible) and replaces the body of buildEnv with BuildChildEnv.

### What This Leaf Consumes

Nothing from sibling leaves. Standard library only (path/filepath for glob matching via path.Match on NAME, not value).

## Tasks

### Task 1: EnvPolicyConfig plumbed through config

**Objective:** [runtime] gains env_policy fields with safe defaults.

**Files:**
- Modify: internal/config/schema.go (RuntimeConfig struct + defaults)
- Test: internal/config/schema_test.go (extend existing)

**Step 1: Write failing test**

```go
func TestRuntimeConfig_EnvPolicyDefaults(t *testing.T) {
    cfg := defaultTestConfig(t) // however existing tests obtain defaults
    if cfg.Runtime.EnvPolicy.Mode != "" {
        t.Fatalf("expected empty mode pre-normalization, got %q", cfg.Runtime.EnvPolicy.Mode)
    }
    // Normalization function turns empty -> allowlist; deny globs defaulted.
}
func TestRuntimeConfig_EnvPolicyNormalize(t *testing.T) {
    cfg := defaultTestConfig(t)
    NormalizeRuntimeDefaults(&cfg.Runtime) // exported helper or reuse existing defaults path
    if cfg.Runtime.EnvPolicy.Mode != "allowlist" { t.Fatal("default must be allowlist") }
    foundDeny := false
    for _, g := range cfg.Runtime.EnvPolicy.DenyGlobs {
        if g == "*KEY*" { foundDeny = true }
    }
    if !foundDeny { t.Fatal("default deny globs missing") }
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/config/ -run TestRuntimeConfig_EnvPolicy -v`
Expected: FAIL (fields do not exist)

**Step 3: Implement**

Add `EnvPolicy EnvPolicyConfig` json/toml-tagged to RuntimeConfig (import cycle note: define the struct in config package mirroring runtime's shape OR move EnvPolicyConfig definition into config and have runtime import config — choose whichever avoids cycle given existing dependency direction; document choice in code comment). Defaults: Mode empty->normalized "allowlist"; DenyGlobs default ["*KEY*","*TOKEN*","*SECRET*","*PASSWORD*","*CREDENTIAL*"].

**Step 4: Verify**

Run: `go test ./internal/config/ -v`
Expected: PASS

### Task 2: BuildChildEnv implementation

**Objective:** Pure function implementing allowlist/inherit filtering.

**Files:**
- Create: internal/runtime/envpolicy.go
- Test: internal/runtime/envpolicy_test.go

**Step 1: Write failing tests** (table-driven)

Cases: (a) sentinel var MEEPT_SENTINEL_SECRET present in parentEnv absent from output in allowlist mode, present in inherit mode; (b) PATH/HOME/TMPDIR survive allowlist; (c) cmdEnv override wins over parent value for allowed key; (d) cmdEnv key denied by glob (*KEY*) is dropped even though caller asked; (e) unknown extra key in cfg.Allowlist is included when present in parentEnv; (f) value containing prefix "MEEPT_SECRET:" passes even if name would be denied (placeholder passthrough); (g) inherit mode returns full parentEnv + cmdEnv minus nothing, stripped==nil.

**Step 2: Run to verify fail** — `go test ./internal/runtime/ -run TestBuildChildEnv -v` → FAIL undefined

**Step 3: Implement** — exact signature from contract. Glob matching on variable NAME with path.Match. Deterministic ordering: preserve parentEnv order for passed vars, append cmdEnv-only vars sorted.

**Step 4: Verify** — PASS; also run `go test ./internal/runtime/...`

### Task 3: LocalBackend rewiring

**Objective:** local exec uses BuildChildEnv; no direct os.Environ() remains in execution path.

**Files:**
- Modify: internal/runtime/local.go (buildEnv → delegate; store policy on backend)
- Modify: internal/runtime/backend.go (Config.EnvPolicy)
- Test: internal/runtime/local_test.go (add sentinel case)

**Step 1: Write failing test**

```go
func TestLocalBackend_StripsDaemonSecrets(t *testing.T) {
    t.Setenv("MEEPT_SENTINEL_SECRET", "topsecret")
    parentEnv := os.Environ()
    b := NewLocalBackend(Config{EnvPolicy: EnvPolicyConfig{Mode: EnvModeAllowlist,
        DenyGlobs: []string{"*SECRET*"}}}, slog.Default())
    res, err := b.Execute(context.Background(), &Command{Cmd: "printenv MEEPT_SENTINEL_SECRET"})
    require.NoError(t, err)
    require.Equal(t, 0, res.ExitCode)
    require.Empty(t, strings.TrimSpace(res.Output))
}
```

(Adjust to actual Execute signature on read.)

**Step 2: verify fail** (current code leaks → Output == topsecret)

**Step 3: implement** — NewLocalBackend captures policy; buildEnv delegates to BuildChildEnv(policy, osEnvironCapturedAtConstruct, cmd.Env). Capture os.Environ() ONCE at backend construction (document why: consistency + test injection).

**Step 4: verify** — `go test ./internal/runtime/... -race` PASS. Then repo-wide guard: `grep -rn "os.Environ()" internal/runtime/ --include=*.go` shows ONLY envpolicy.go/local.go construction site.

### Task 4: Inherit-mode warning + docs pointer

**Objective:** legacy mode loud but available.

**Files:**
- Modify: internal/daemon/components.go (runtime init block ~1648: log.Warn when env_mode==inherit)
- Modify: docs/configuration/runtime.md (or nearest existing page — search_files for "default_backend" under docs/) — document env_mode/env_allowlist/env_deny_globs with examples

**Step 1: failing test** — components test asserting warn emitted (follow existing logger-capture pattern in daemon tests; if none exists, assert via log hook).
**Step 2: verify fail. Step 3: implement warn + doc paragraph. Step 4: verify.**

## Self-Verification Checklist

- [ ] All tasks implemented; `go test ./internal/runtime/ ./internal/config/ -race` passing
- [ ] Contract signatures exact (BuildChildEnv, EnvPolicyConfig, BaseEnvKeys)
- [ ] No os.Environ() outside envpolicy.go + single construction capture in local.go
- [ ] Deny globs beat allowlist; placeholder passthrough works
- [ ] Docs updated; no scope creep

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Every task implemented, tests first, passing under -race
- [ ] Signatures match orchestrator Contract 1 exactly
- [ ] Default behavior change documented (docs + startup warn only in inherit mode)
- [ ] Conventions: wrapped errors, no panics, table-driven tests, JSON5+TOML tags
- [ ] predid analyzer clean (no rand IDs introduced)

Output: APPROVED or specific gaps with file:line.

## Notes

- This changes default behavior intentionally — that IS issue #25's fix. The inherit escape hatch keeps upgrades non-breaking for power users.
- If config package already imports runtime (check!), place EnvPolicyConfig in config and alias in runtime to avoid cycle. Record decision in code comment.
- Do not touch docker.go env passing (already explicit-only).
