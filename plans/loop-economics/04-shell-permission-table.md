# Shell Permission Table - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Declarative command-prefix allow/ask/deny presets evaluated beside tirith.
- **Deps:** none | **Context:** 50K | **Group:** A

## Goal

Tirith scans for dangerous PATTERNS; users need declarative POLICY ("git push always asks; rm -rf always denies; my-cli always allowed"). Add prefix-keyed rule tables with three shipped presets, evaluated BEFORE tirith (policy decision short-circuits scanning), configurable per deployment.

## Context

SecurityEngine evaluates risk via lookupBaseRule (~engine.go:512) + evaluateCommand (~544); needsConfirmation (~855). ShellExecuteTool consults securityOrchestrator pre-exec. knownSafeCommands escape hatch exists on the tool — permission table SUPERSEDES it when configured (keep both; table wins).

Key files: internal/security/engine.go, internal/tools/builtin/shell.go, config schema.

## Interface Contracts (From Parent)

```go
// internal/security/shell_permissions.go:
type ShellRule struct{ Action string } // "allow"|"ask"|"deny"
type PermissionTable struct{ rules []tableRule /*sorted longest-prefix-first*/ }
func NewPermissionTable(rules map[string]ShellRule) *PermissionTable
func (p *PermissionTable) Evaluate(command string) (decision string, matchedPrefix string, ok bool)
// ok=false when no prefix matches. Matching on tokenized base-command+subcommand
// prefix using existing tokenizeCommand-style splitting (reuse if exported; else local).
```

Config [security.shell_permissions]: preset ("workspace" default|"readonly"|"danger"|"" custom) + rules map override/extend.
Preset contents:
- workspace: deny ["rm -rf","rm -fr","mkfs","dd if="]; ask ["sudo","git push","docker system prune","chmod 777","curl | sh","bash -c","sh -c"]; allow []
- readonly: everything ask except explicit deny list same as workspace + ["git commit","npm publish"]
- danger: empty (all fall through)
Evaluation order in engine: table match -> act (allow: skip tirith? NO — deny/ask short-circuit; allow still runs tirith as defense-in-depth) ; no match -> existing path unchanged.
"ask" surfaces via existing confirmation flow (needsConfirmation path w/ reason=prefix rule).

## Tasks
1. Failing tests table: exact-prefix match precedence (longest wins); case-insensitive base cmd; no-match ok=false; preset constructors produce documented contents; malformed action error.
2. Implement table + presets.
3. Failing engine integration tests: deny blocks before tirith; ask routes to confirmation; allow proceeds but tirith still consulted (assert scan called).
4. Wire config plumbing + shell.go consultation point; docs page section w/ examples.

## Self-Verification Checklist
- [ ] -race green internal/security internal/tools/builtin
- [ ] Default preset = workspace; behavior delta ONLY on listed prefixes
- [ ] Docs updated

## Review Checklist
- [ ] No regex injection into command parsing (token-based only)
- [ ] Ask path reuses existing confirmation machinery (no new approval channel)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: keep table pure/sync — no I/O in Evaluate (mutexio-friendly).
