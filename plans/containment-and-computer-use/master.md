# Containment, Secrets, Write-Journal, Computer-Use - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 10 leaf documents under this node
- **Scope:** Close the four audited containment/approval/computer-use gaps in meept: environment isolation + fail-closed sandboxing, secret-backed credential injection, generalized write staging + reversible journal, and cua-driver computer-use.

## Goal

Meept's security stack (taint, tirith, fence, engine) operates inside one trusted process while spawned shells inherit the daemon's entire environment and fall back to unsandboxed execution when Docker is unavailable. Mutating file tools stage diffs for agent-resolved approval but writes bypass staging, accepted changes have no undo, and no user-facing surface shows pending diffs. There is no computer-use capability at all.

This tree closes those four gaps using patterns verified against duckagent (Apache-2.0), FrontierAgent (Apache-2.0), atomic-agent (MIT), and Hermes' cua-driver integration (MIT), adapted to meept's Go/SQLite/bus architecture. User-visible outcomes:

1. Agent-run shells contain only an explicit allowlist of environment variables; declared secrets reach children only as placeholders that resolve at an egress proxy.
2. Shell execution fails closed when sandboxing is required and unavailable; a lightweight bwrap backend covers Linux without Docker.
3. Every file mutation can be reviewed as a diff before landing and reverted after landing.
4. An enabled-by-config cua-driver MCP server gives agents screen-capture-and-act capability with risk-gated actions.

## Architecture

All four workstreams extend existing meept subsystems rather than introducing parallel ones. Environment isolation replaces the body of `buildEnv()` in `internal/runtime/local.go` behind a new `EnvPolicy` type configured from `[runtime]`. Backend selection moves into a resolver that honors `require_sandbox` and a new external-binary bwrap backend implementing the existing `ExecutionBackend` interface. Secrets move from ambient environment to an explicit `SecretBroker` fed by a new `[secrets]` config section, with placeholder tokens (`MEEPT_SECRET:<name>`) substituted for real values everywhere children can see them, and a loopback-only HTTP proxy performing header injection for allowlisted hosts. Write staging generalizes the existing `PendingChangesRegistry` to every mutating file tool, adds pre-image checksum verification at accept time, and persists applied changes to a new SQLite-backed `ChangeJournal` exposing revert; TUI/Flutter/HTTP surfaces read the same registry/journal over RPC. Computer-use arrives as a catalog entry in `mcp_servers.json5` for `cua-driver mcp` (external binary, MIT), SecurityEngine risk rules keyed on tool-name prefixes, and a bundled skill document teaching the capture-act-verify loop.

Control flow invariant: secrets and daemon environment variables flow downward to children only through `EnvPolicy.BuildEnv()` and `SecretBroker.ChildEnv()`; no call site may read `os.Environ()` directly inside `internal/runtime` or tool execution paths.

## Interface Contracts

### Contract 1: EnvPolicy

```go
// File: internal/runtime/envpolicy.go
package runtime

// EnvMode selects child-environment construction.
type EnvMode string

const (
    EnvModeAllowlist EnvMode = "allowlist" // default: base set + allowlist, minus deny globs
    EnvModeInherit   EnvMode = "inherit"   // legacy: full os.Environ() (logs warning at startup)
)

type EnvPolicyConfig struct {
    Mode        EnvMode  `json:"env_mode"         toml:"env_mode"`
    Allowlist   []string `json:"env_allowlist"    toml:"env_allowlist"`    // extra vars beyond BaseEnvKeys
    DenyGlobs   []string `json:"env_deny_globs"   toml:"env_deny_globs"`   // win over allowlist
}

// BaseEnvKeys is the always-passed set when Mode == allowlist.
var BaseEnvKeys = []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "TERM", "SHELL",
    "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}

// BuildChildEnv constructs the child environment. parentEnv is os.Environ()
// captured once at daemon start; secrets maps placeholder -> real value and
// its keys are ALWAYS excluded from output (placeholders arrive via cmd.Env).
// Returns env slice plus names of stripped variables for debug logging.
func BuildChildEnv(cfg EnvPolicyConfig, parentEnv []string, cmdEnv map[string]string) (env []string, stripped []string)

// Owner: 01-env-policy.md
// Consumers: 02-failclosed-backends.md (wires into LocalBackend), 03-secret-broker.md (placeholder exclusion rule)
```

### Contract 2: Sandbox Resolver

```go
// File: internal/runtime/resolver.go
package runtime

type SandboxOrder string // "auto" | "bwrap" | "docker" | "local"

type ResolverConfig struct {
    Order          SandboxOrder `json:"sandbox_backend_order" toml:"sandbox_backend_order"`
    RequireSandbox bool         `json:"require_sandbox"       toml:"require_sandbox"`
}

// Resolve picks a backend. requireSandbox==true and no qualifying backend
// returns ErrSandboxRequired (execution REFUSED, never falls back to local).
var ErrSandboxRequired = errors.New("runtime: sandbox required but no qualifying backend available")

func ResolveBackend(mgr *ContainerManager, cfg ResolverConfig, logger *slog.Logger) (ExecutionBackend, error)

// Owner: 02-failclosed-backends.md
// Consumers: daemon/components.go wiring (~line 1650 block), 10-integration-tests.md
```

### Contract 3: SecretBroker

```go
// File: internal/secrets/broker.go
package secrets

type Source struct {
    Kind     string   `json:"kind"        toml:"kind"`        // "env" | "file"
    Name     string   `json:"name"        toml:"name"`        // env var name or file path
    Hosts    []string `json:"hosts"       toml:"hosts"`       // host suffixes proxy may inject toward
    Header   string   `json:"header"      toml:"header"`      // e.g. "Authorization"
    Format   string   `json:"format"      toml:"format"`      // e.g. "Bearer {}"; {} replaced by value
}

// Placeholder returns the token children receive: "MEEPT_SECRET:<name>".
func Placeholder(name string) string

// ChildValue returns the placeholder (NEVER the real value) for env passing.
func (b *Broker) ChildValue(name string) (string, error)

// Resolve returns the real value (broker/proxy internal use ONLY; unexported
// outside package via method on unexported receiver where possible).
func (b *Broker) resolve(name string) (string, error)

// Owner: 03-secret-broker.md; proxy consumer: 04-egress-proxy.md
```

### Contract 4: Egress Proxy Injection

```go
// File: internal/secrets/proxy.go
package secrets

type ProxyConfig struct {
    Enabled  bool   `json:"enabled"   toml:"enabled"`
    Listen   string `json:"listen"    toml:"listen"`   // default "127.0.0.1:0" (ephemeral)
}

// Start launches the loopback proxy. It scans outgoing request headers and
// bodies for MEEPT_SECRET:<name> placeholders; a request whose target host
// matches that secret's Hosts list gets the placeholder replaced by
// Format-formatted real value in Header. Non-matching hosts: request passes
// through UNMODIFIED and a metric counts the leak attempt.
func (p *Proxy) Start(ctx context.Context, cfg ProxyConfig) (addr string, err error)

// Owner: 04-egress-proxy.md; Consumers: 10-integration-tests.md
```

### Contract 5: Staged Mutation Coverage + Checkpointed Accept

```go
// Existing types extended (internal/tools/builtin/pending_changes.go):
type PendingChange struct {
    // ... existing fields ...
    PreImageSHA256 string `json:"pre_image_sha256"` // sha256(Original); verified at accept
}

// NewRegistry methods:
func (r *PendingChangesRegistry) StageWrite(sessionID, path string, original, modified []byte) (*PendingChange, error)

// ResolveTool accept path: re-read file; if current content hash != PreImageSHA256
// AND != hash(Modified) => refuse with drift message (file changed since staging).

// Owner: 05-write-staging.md; Consumers: 06-journal.md, 07-user-surfaces.md
```

### Contract 6: ChangeJournal

```go
// File: internal/tools/builtin/change_journal.go
package builtin

type JournalEntry struct {
    ID         string    // pkg/id.Generate()
    SessionID  string
    FilePath   string
    PreImage   []byte    // content BEFORE apply (cap size; skip+journal-skip flag > 1MiB)
    AppliedAt  time.Time
    ChangeIDs  []string  // pending-change IDs that produced it
}

func NewJournal(dbPath string, maxEntryBytes int64) (*Journal, error)
func (j *Journal) Record(entry *JournalEntry) error
func (j *Journal) List(sessionID string) ([]JournalEntry, error)   // newest first
func (j *Journal) Revert(id string, fence FenceChecker) (restoredPath string, err error)
// Revert refuses unless current file hash == hash of post-image recorded at
// apply time (drift guard, duckagent pattern).

// Owner: 06-journal.md; Consumers: 07-user-surfaces.md, CLI cmd changes revert
```

### Contract 7: Computer-Use Risk Rules

```go
// SecurityEngine base rules (internal/security/engine.go lookupBaseRule data):
//   tool name prefix "computer." :
//     computer.capture / computer.screenshot        -> RiskLow
//     computer.click/type/hotkey/scroll/drag/wait   -> RiskHigh  (confirmation-gated)
//     computer.set_value                            -> RiskHigh
// Config override via existing [security.rules] mechanism.
// MCP server id: "cua-driver" in ~/.meept/mcp_servers.json5 (enabled: false default),
// command: ["cua-driver", "mcp"].
// Skill file: config/skills/computer-use/SKILL.md bundled default.

// Owner: 08-cua-driver-wiring.md (catalog + rules), 09-computer-use-skill.md (skill doc)
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-env-policy.md | leaf | none | 60K | A |
| 02 | 02-failclosed-backends.md | leaf | 01 | 70K | B |
| 03 | 03-secret-broker.md | leaf | 01 (placeholder exclusion convention) | 60K | B |
| 04 | 04-egress-proxy.md | leaf | 03 | 80K | C |
| 05 | 05-write-staging.md | leaf | none | 60K | A |
| 06 | 06-journal.md | leaf | 05 | 70K | C |
| 07 | 07-user-surfaces.md | leaf | 05, 06 | 90K | D |
| 08 | 08-cua-driver-wiring.md | leaf | none | 40K | A |
| 09 | 09-computer-use-skill.md | leaf | 08 (tool naming) | 30K | B |
| 10 | 10-integration-tests.md | leaf | 01-09 | 70K | E |

**Concurrency groups:** A runs first (independent). B after A. C after B. D after C. E last.

## Dispatch Protocol

Dispatch one concurrency group at a time, in order A -> B -> C -> D -> E. Max 3 children in flight per group batch; group sizes above fit.

For each child:

1. **Read** the leaf document. Dispatch via `delegate_task`:
   - Goal: "Implement all tasks from <leaf path>"
   - Context: FULL leaf text verbatim + the relevant Interface Contracts from this orchestrator + Coding Conventions below + these repo facts INLINED:
     - Repo root /Users/caimlas/git/meept; module github.com/caimlas/meept
     - Config is JSON5: internal/config/schema.go holds structs; defaults near line 1900
     - Runtime package: internal/runtime/{backend,local,docker,manager}.go; Command/ExecutionBackend defined in backend.go; buildEnv at local.go:96-102
     - Daemon wiring: internal/daemon/components.go runtime manager block ~1648-1672; registerBuiltinTools called ~1680
     - Pending changes: internal/tools/builtin/pending_changes.go (PendingChange struct lines 9-19); consumers file_edit.go:385-426, resolve.go
     - Security engine: internal/security/engine.go (lookupBaseRule ~512, needsConfirmation ~855); FenceChecker internal/security/fence.go
     - IDs use pkg/id.Generate(); never math/rand or UnixNano (predid analyzer enforces)
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Include: "Do NOT use read_file on existing source files — explore with search_files or terminal cat. After writing a file do NOT read it back."

2. **Review in-session** (main model, not delegated): run `go build ./... && go test ./internal/<pkg>/...`, check leaf Review Checklist + contract conformance + no debug artifacts + AGENTS.md conventions (typed-nil guards on Set* methods, mutex collect-under-lock-release-then-operate, wrapped errors %w, lowercase UI strings).

3. **Re-dispatch on gaps** (max 3 cycles) with original leaf + specific findings. Escalate to user if still failing.

4. **On APPROVED:** commit exact paths: `git add <leaf file list> && git commit -m "feat(<scope>): <leaf summary>"`. Update tracking table.

### Integration Phase (after all REVIEWED)

Run 10-integration-tests.md contents via a final dispatch, then in-session:
- `go build ./... && go test ./... -race`
- `make lint-ci` (golangci-lint + mutexio/predid/fieldguard/selflock analyzers)
- Manual smoke checklist: daemon boots with new config defaults; `meept config get runtime.env_mode` returns allowlist; staged write appears via new RPC endpoint; revert restores bytes; cua-driver entry visible in `meept mcp list` (disabled).
- Normalize gofmt across touched files; verify no `N|` line-prefix corruption: `grep -rcE '^\s+[0-9]+\|' --include='*.go' .` returns zero.
- Commit integration fixes separately: `feat(containment): integration pass`.

## Review Checklist

Per child, in-session:

- [ ] All leaf tasks implemented; tests written first and passing
- [ ] Interface contracts satisfied exactly (names, signatures, file paths)
- [ ] No os.Environ() reads introduced outside envpolicy.go within internal/runtime
- [ ] No secret plaintext logged, persisted outside broker DB/memory, or placed in cmd.Env
- [ ] Typed-nil guards on every Set* method; mutexio-clean (no I/O under lock)
- [ ] Errors wrapped with %w; no bare panic; two-value type assertions on bus payloads
- [ ] IDs from pkg/id.Generate(); UI strings lowercase
- [ ] No scope creep beyond the leaf; no TODO/debug prints/placeholders
- [ ] gofmt clean; no `N|` corruption

Output per child: APPROVED or specific gaps with file:line.

## Coding Conventions

- Language: Go 1.24+ (module github.com/caimlas/meept); stdlib-first, match neighboring imports
- Config: JSON5 tags both json+toml in schema.go structs; defaults registered beside existing defaults (~schema.go:1880-1960)
- Tests: table-driven where natural, `_test.go` alongside, `go test -race` clean
- Errors: `fmt.Errorf("context: %w", err)`; sentinels for refusal cases
- Concurrency: collect-under-lock then operate; never I/O under mutex (mutexio analyzer)
- Set* methods: nil guards per CLAUDE.md pattern
- Docs: every user-facing knob gets a paragraph in docs/configuration/ (leaf 07 and 08 include their own doc updates)

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-env-policy | PENDING | 0 | |
| 02-failclosed-backends | PENDING | 0 | |
| 03-secret-broker | PENDING | 0 | |
| 04-egress-proxy | PENDING | 0 | |
| 05-write-staging | PENDING | 0 | |
| 06-journal | PENDING | 0 | |
| 07-user-surfaces | PENDING | 0 | |
| 08-cua-driver-wiring | PENDING | 0 | |
| 09-computer-use-skill | PENDING | 0 | |
| 10-integration-tests | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

Executed by leaf 10 plus in-session checks above:

1. Sentinel test: launch daemon test fixture with MEEPT_SENTINEL_SECRET=xyz in env; run shell tool `printenv MEEPT_SENTINEL_SECRET`; expect empty output and sentinel listed in stripped-names debug log.
2. Placeholder round-trip: configure [secrets.test] kind=env name=MEEPT_TEST_TOKEN hosts=["api.test"] header=Authorization format="Bearer {}"; child shell echoes $MEEPT_SECRET_test -> literal placeholder; curl through proxy to stub server -> receives Authorization: Bearer <real>; curl to other host -> placeholder passes through unmodified, leak-attempt metric increments.
3. Sandbox refusal: require_sandbox=true with no bwrap/docker -> shell tool returns ErrSandboxRequired-derived error; no local execution occurs (audit log proves).
4. Staged write flow: file_write tool -> pending change with PreImageSHA256 -> external edit drifts file -> resolve accept refuses with drift message -> restore -> accept succeeds -> journal entry exists -> revert restores original bytes (hash-verified).
5. Cross-surface: pending change created in session S visible via HTTP GET /api/pending-changes and TUI modal; approve from HTTP applies identical to agent-side resolve.
6. Full suite: `go test ./... -race && make lint-ci` green.

## Structural Completeness Check (Before Dispatch)

Required sections present in this orchestrator: Dispatch Protocol ✓, Interface Contracts ✓, Review Checklist ✓, Coding Conventions ✓, Completion Tracking Table ✓, Integration Test Plan ✓. Verify each leaf contains Meta, Goal, Context, Interface Contracts, Tasks, Self-Verification Checklist, Review Checklist, Notes.

## Notes

- Branch from current main; this tree touches internal/runtime, internal/secrets (new), internal/tools/builtin, internal/config, internal/security, internal/comm/http, internal/tui, ui/flutter_ui. Commit per leaf with explicit paths only.
- The env allowlist default CHANGES behavior deliberately (issue bhodgens/meept#25). Users needing legacy behavior: runtime.env_mode="inherit".
- Do NOT vendor bubblewrap source (LGPL inside Apache upstream). Exec external binary; document install requirement.
- cua-driver ships disabled; enabling is user action. Skill (leaf 09) bundles regardless so it loads on enable.
- References: meept/docs/research/2026-08-24-agent-parity-audit.md sections 3 (gaps 1,3,4,5) and 6 (borrow tiers); issues bhodgens/meept#25 (env) and #26 (cua-driver).
