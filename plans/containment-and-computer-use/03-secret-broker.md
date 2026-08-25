# Secret Broker - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Do NOT commit.
> Do NOT use read_file on existing source — search_files/terminal cat only.

## Meta

- **Parent:** ../master.md
- **Scope:** [secrets] config section + SecretBroker storing declared secrets and issuing MEEPT_SECRET:<name> placeholders into child environments.
- **Dependencies:** 01-env-policy.md (placeholder passthrough convention)
- **Estimated Context:** 60K
- **Concurrency Group:** B
- **Audit references:** parity-audit gap #3 (injection half)

## Goal

Users declare secrets once in config; children (shell commands, MCP server subprocesses) receive ONLY placeholder strings `MEEPT_SECRET:<name>`. Real values live in broker memory (loaded from env var or file at startup) and are resolved exclusively by the egress proxy (leaf 04) or explicit user CLI. Plaintext never appears in cmd.Env, logs, or bus payloads.

## Context

Config: internal/config/schema.go — add top-level Secrets section. IDs must use pkg/id.Generate() if any generated. The daemon already resolves env vars for LLM providers via api_key_env pattern — this is analogous but for TOOL-side credentials. MCP server launching lives in internal/tools/mcp (stdio transport spawns subprocesses with env) — wire ChildValue placeholders into that env construction too.

Key files:
- internal/config/schema.go - add SecretsConfig
- internal/tools/mcp/transport/stdio.go - subprocess env construction to extend
- pkg/id - ID generation

## Interface Contracts (From Parent)

### Exposes

```go
// File: internal/secrets/broker.go
package secrets

type Source struct {
    Kind   string   `json:"kind"   toml:"kind"`   // "env"|"file"
    Name   string   `json:"name"   toml:"name"`   // env var name / file path
    Hosts  []string `json:"hosts"  toml:"hosts"`  // host suffixes proxy may inject toward
    Header string   `json:"header" toml:"header"` // e.g. Authorization
    Format string   `json:"format" toml:"format"` // e.g. "Bearer {}"
}

type Config map[string]Source // secret name -> source

const PlaceholderPrefix = "MEEPT_SECRET:"

func Placeholder(name string) string // "MEEPT_SECRET:" + name

type Broker struct{ /* unexported */ }

func NewBroker(cfg Config, logger *slog.Logger) (*Broker, error)
// Loads every source eagerly; missing env var/file => error listing all failures.

func (b *Broker) Names() []string
func (b *Broker) Source(name string) (Source, bool)
func (b *Broker) ChildValue(name string) (string, error) // returns Placeholder(name), error iff unknown name
```

resolve(name) stays UNEXPORTED in package secrets — leaf 04 (same package) uses it; nothing else may.

Setter convention: any Set* method gets typed-nil guard per CLAUDE.md.

### Consumes

Nothing from siblings except conventions.

## Tasks

### Task 1: Config schema

**Files:** Modify internal/config/schema.go (+defaults: empty map); Test schema_test.go extension.
Failing test: defaults produce empty non-nil Secrets map; round-trip a TOML snippet through existing config load path. Standard TDD cycle.

### Task 2: Broker load + child values

**Files:** Create internal/secrets/broker.go + broker_test.go.

**Step 1 failing tests:** (a) env-kind loads from t.Setenv value; (b) file-kind reads file, trims trailing newline; (c) missing source aggregates errors ("2 secrets failed: a (env MISSING_VAR), b (file /nope)") ; (d) ChildValue known -> exact placeholder string; unknown -> error; (e) resolve returns real value; (f) Names sorted.

Standard cycle.

### Task 3: MCP stdio env wiring

**Files:** Modify internal/tools/mcp/transport/stdio.go (env construction); Test stdio env test extension.

Where MCP server subprocess env is assembled, entries whose VALUE equals Placeholder(x) stay as-is (they already do — placeholders are literal). The wiring task: allow mcp_servers.json5 env values of form "${secret:name}" to be substituted with Placeholder(name) at launch (NOT real value). Failing test proves substitution yields placeholder text, never plaintext. Standard cycle. Document syntax in the catalog header comment (~/.meept/mcp_servers.json5 template in repo config/ dir).

## Self-Verification Checklist

- [ ] grep proves no resolve( ) callers outside package secrets
- [ ] No plaintext in logs: logger calls audited in review
- [ ] Tests green under -race; contracts exact
- [ ] ${secret:name} doc'd in catalog template

**DO NOT COMMIT.**
**Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Eager-load failure aggregation correct
- [ ] Placeholder format exactly MEEPT_SECRET:<name>
- [ ] resolve unexported; no leakage paths in new code
- [ ] Schema tags json+toml both present

Output: APPROVED or gaps.

## Notes

- Broker holds values in memory only; no persistence layer in this leaf. If restart-time re-entry from keyring wanted later, separate leaf.
