# Fail-Closed Sandbox Backends - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Do NOT commit.
> Do NOT use read_file on existing source files — explore with search_files or
> terminal cat. Write once; do not read files back.

## Meta

- **Parent:** ../master.md
- **Scope:** Backend resolver with require_sandbox fail-closed semantics + new external-binary bwrap backend for Linux.
- **Dependencies:** 01-env-policy.md (LocalBackend constructor now takes Config with EnvPolicy)
- **Estimated Context:** 70K
- **Concurrency Group:** B
- **Audit references:** parity-audit gap #1; components.go:1664 silent-degrade fix

## Goal

Today, when `[runtime] enabled` and Docker manager creation fails, daemon logs a warning and commands run unsandboxed locally (internal/daemon/components.go ~1664). This leaf adds: (1) a `ResolverConfig{Order, RequireSandbox}` with resolution order auto|bwrap|docker|local; (2) fail-closed semantics — when require_sandbox is true and no qualifying backend exists, execution REFUSES with ErrSandboxRequired instead of degrading; (3) a BwrapBackend that jails commands via external `bwrap` invocation (bubblewrap user namespaces) on Linux. No bubblewrap source vendored (LGPL); we exec the binary.

## Context

`internal/runtime/backend.go` defines ExecutionBackend interface {Execute(ctx, *Command) (*CommandResult, error); Name() string; Close() error} and Config. `manager.go` ContainerManager holds backends map + GetDefaultBackend(). `local.go` LocalBackend, `docker.go` DockerBackend exist. Daemon wiring at internal/daemon/components.go ~1648-1672 constructs runtime.Config from cfg.Runtime then NewContainerManager; failure path warns and sets containerMgr=nil which makes ShellExecuteTool fall back to direct exec.

Key files:
- internal/runtime/manager.go - ContainerManager, GetBackend(name), GetDefaultBackend()
- internal/runtime/local.go - local backend shape to mirror
- internal/runtime/docker.go - availability probing pattern (dockerHostFromEnv)
- internal/security/fence.go CheckCommand - existing pre-exec path checks (unchanged; resolver composes BEFORE fence)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/runtime/resolver.go
package runtime

type SandboxOrder string // "auto" | "bwrap" | "docker" | "local"

type ResolverConfig struct {
    Order          SandboxOrder `json:"sandbox_backend_order" toml:"sandbox_backend_order"`
    RequireSandbox bool         `json:"require_sandbox"       toml:"require_sandbox"`
}

var ErrSandboxRequired = errors.New("runtime: sandbox required but no qualifying backend available")

// Qualifies reports whether backend name provides OS-level confinement.
func Qualifies(name string) bool // bwrap,docker => true; local => false

func ResolveBackend(mgr *ContainerManager, cfg ResolverConfig, logger *slog.Logger) (ExecutionBackend, error)
```

```go
// File: internal/runtime/bwrap.go
package runtime

type BwrapConfig struct {
    BinaryPath string   `json:"binary_path"`  // default "bwrap"
    ExtraArgs  []string `json:"extra_args"`
    TmpfsDirs  []string `json:"tmpfs_dirs"`   // default /tmp
}

func NewBwrapBackend(cfg BwrapConfig, envPolicy EnvPolicyConfig, logger *slog.Logger) (*BwrapBackend, error)
// error if exec.LookPath fails OR runtime.GOOS != "linux"
// Implements ExecutionBackend. Execute builds:
//   bwrap --ro-bind /usr /usr --ro-bind /bin /bin --ro-bind /lib /lib \
//         --ro-bind /lib64 /lib64 (each only if present) \
//         --bind <workdir> <workdir> --dev /dev --proc /proc \
//         [--tmpfs /tmp] <ExtraArgs...> -- sh -c <cmd>
// Env via BuildChildEnv(envPolicy, capturedParentEnv, cmd.Env).
```

Config additions (backend.go): `Sandbox ResolverConfig json:"sandbox"` on runtime.Config; BwrapConfig field.

Daemon change (components.go runtime block): replace warn-and-nil fallback with ResolveBackend call; when it returns ErrSandboxRequired AND cfg.Runtime.Enabled, log ERROR and mark ShellExecuteTool sandbox-refusing (tool returns refusal error per command) — never silently exec locally. When [runtime].enabled=false entirely, behavior unchanged (direct local exec, existing posture) — document this distinction in docs.

### What This Leaf Consumes

- 01-env-policy.md: BuildChildEnv signature; EnvPolicyConfig type.
- Existing: ExecutionBackend interface, Command/CommandResult.

## Tasks

### Task 1: Resolver core + Qualifies

**Files:** Create internal/runtime/resolver.go; Test internal/runtime/resolver_test.go

**Step 1 failing test:** table-driven ResolveBackend cases: order=auto w/ docker available -> docker; auto no-docker bwrap-present -> bwrap; explicit order respected; require=true nothing qualifies -> ErrSandboxRequired; require=false falls through to local WITH warning log recorded (use slog test handler).

**Step 2:** verify FAIL undefined. **Step 3:** implement; availability probes: docker via manager's existing probe, bwrap via LookPath+GOOS check injected as func fields for testability. **Step 4:** PASS.

### Task 2: BwrapBackend

**Files:** Create internal/runtime/bwrap.go + bwrap_linux.go (build-tagged exec logic) / bwrap_other.go (returns unsupported); Test bwrap_test.go (skip unless linux + bwrap present — mirror hasDocker() pattern in docker_test.go)

**Step 1 failing test (linux-only):** Execute echo hello inside jail -> output "hello", exit 0; command attempting to read outside bind (e.g. ls /root) exits non-zero or empty per environment; env sentinel stripped per EnvPolicy (reuse Task pattern from leaf 01).
**Steps 2-4:** standard cycle.

### Task 3: Daemon wiring + config plumbing

**Files:** Modify internal/config/schema.go (RuntimeConfig.Sandbox), internal/daemon/components.go runtime block, docs/configuration page from leaf 01 extended with sandbox_backend_order/require_sandbox.

**Step 1 failing test:** daemon components test asserting: require_sandbox=true + unavailable probes => shell tool receives refusing backend (assert error surfaces ErrSandboxRequired text), NOT nil-manager fallback.
**Standard cycle.**

## Self-Verification Checklist

- [ ] go test ./internal/runtime/... -race green; linux bwrap tests skip cleanly on darwin
- [ ] No warn-and-degrade path remains in components.go when runtime enabled
- [ ] Contracts exact: ResolverConfig, ErrSandboxRequired, Qualifies, NewBwrapBackend
- [ ] No bwrap source copied anywhere (grep vendor dirs)
- [ ] Docs updated

**DO NOT COMMIT.**
**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Fail-closed proven by test (no silent local fallback)
- [ ] Bwrap arg assembly injection-safe (cmd passed as single sh -c argument, args array not string concat)
- [ ] GOOS build tags correct; darwin CI unaffected
- [ ] Conventions per orchestrator (wrapped errors, nil-safe setters)

Output: APPROVED or gaps with file:line.

## Notes

- macOS dev machine: bwrap tests will skip; do not fake-pass them. Linux verification happens in integration phase or CI later.
- SandboxOrder "auto": docker > bwrap > local (docker strongest containment present today).
