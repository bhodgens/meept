# Loop Economics & Capability Roadmap - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Dispatch implementation agents, review their work in-session, re-dispatch if
> incomplete, track completion. Do NOT implement code yourself.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 15 leaf documents
- **Scope:** Close the borrow-list gaps outside the containment tree: prompt-cache economics, lazy tool schemas, scheduler durability, ops lifecycle, web/shell hardening, loop guards, gated autonomy, egress policy, inter-agent messaging, local-model tier, browser automation, memory science.

## Goal

Deliver the audit borrow list (meept/docs/research/2026-08-24-agent-parity-audit.md sections 6+) excluding items covered by plans/containment-and-computer-use/. Outcome: cheaper turns via cache-stable prompts and lazy schemas, durable scheduling, hardened web/shell surfaces, resilient long-horizon loops, employee-to-employee messaging, a managed local-model tier, native browser control, and principled memory forgetting/distillation.

## Architecture

All work extends existing subsystems. Prompt assembly (internal/agent/prompt.go) reorders sections stable-first and exposes a prefix hash. Tool definitions flow through Registry.GetDefinitions() — an indexed mode stubs rare tools to one line behind a new `tool_view` builtin. Scheduler gains claim-before-deliver tick semantics inside internal/scheduler. Shell permission tables slot into SecurityEngine rule evaluation beside tirith. SSRF guards become a shared package consumed by web_fetch/websearch and later browser tools. Loop guards extend the EXISTING cycle detector (features.md documents SHA256 cycle detection — do not duplicate; add no-progress ladder + duplicate-search rollback + reasoning watchdogs beside it). Egress policy grows the containment tree's secrets proxy into host/CIDR allow-ask-deny (cross-tree dependency noted below). Inter-agent messaging rides the existing bus with a receipts table. Local-model tier extends llm.RuntimeManager with a model lifecycle CLI, then GBNF-constrained tool calls for llama.cpp-family providers only. Browser automation lands as native Go chromedp tools consuming the SSRF package. Memory voting/eviction/lessons extend internal/memory consolidation.

## Interface Contracts

### C1: Prompt Section Ordering + Prefix Hash (01)

```go
// internal/agent/prompt.go additions:
type PromptSection struct { Name string; Stable bool; Body string }
func AssembleOrdered(sections []PromptSection) (prompt string, stablePrefixHash string)
// Ordering: all Stable=true sections first (byte-order preserved as given),
// then unstable. Hash = sha256 hex of concatenated stable bodies.
```

### C2: Indexed Tool Schema Mode (02)

```go
// internal/tools/registry.go:
type SchemaMode string // "full" (default) | "indexed"
func (r *Registry) SetSchemaMode(mode SchemaMode, alwaysFull []string)
// indexed: GetDefinitions() returns ParameterSchema==nil + Description prefixed
// "[one-line] use tool_view{name} for schema" for tools NOT in alwaysFull.
// New builtin internal/tools/builtin/tool_view.go:
//   tool_view{name string} -> returns llm.ToolDefinition-equivalent JSON text
//   injected into conversation as tool result. LRU cache of expansions.
// Config [agent.tools]: schema_mode="full", always_full=[core set].
```

### C3: Scheduler Tick Claiming (03)

```go
// internal/scheduler additions:
func (s *Scheduler) ClaimTick(jobID string, tick time.Time) (claimed bool, err error)
// Atomic INSERT into claimed_ticks(job_id,tick_time UNIQUE); false if exists.
// Missed ticks COALESCE: only latest missed tick enqueues per job per wake.
```

### C4: Shell Permission Table (04)

```go
// internal/security: PermissionTableConfig{Presets map[string]Rule} where
// Rule{Action:"allow|ask|deny"} keyed by command PREFIX ("rm -rf","sudo","git push").
// Evaluation order: exact prefix match > tirith scan > default risk path.
// Config [security.shell_permissions]; presets: workspace(default)/readonly/danger.
```

### C5: SSRF Guard (05)

```go
// internal/security/ssrf/guard.go:
type GuardConfig{AllowedHosts,BlockedCIDRs []string; MaxRedirects int}
func NewGuard(cfg GuardConfig) (*Guard, error)
func (g *Guard) CheckURL(raw string) error          // scheme allowlist, IP resolve+CIDR deny
func (g *Guard) CheckRedirect(req *http.Request, via []*http.Request) error // per-hop revalidation
// Consumed by webfetch/websearch tools (this tree) + browser tools (leaf 13).
```

### C6: Daemon Lifecycle Commands (06)

```
meept doctor [--fix]    -> health report: socket, pid, sqlite integrity, runtime procs, disk, config parse; --fix repairs safe items (stale pidfile, orphan sockets)
meept status --json     -> machine-readable daemon vitals incl. uptime/version/components
meept shutdown          -> graceful: drain jobs (timeout), close listeners, persist state, exit
Orphan sweep: daemon startup scans ppid=1 children tagged MEEPT_DAEMON_CHILD env marker and kills them.
```

### C7: Loop Guard Extensions (07)

```go
// internal/agent: beside existing cycle detector:
NoProgressLadder{WarnAt:3,VetoAt:5,GracefulAfter:3 vetoes}
DuplicateSearchRollback — identical web_search args within window N: pop turn, re-sample, do NOT count toward iteration budget (config flag, default off until proven).
ReasoningWatchdog — reasoning-only streak > tokens/time thresholds injects nudge.
Config [agent.guards].
```

### C8: Quality-Gated Autonomy (08)

```go
// internal/employee/goal_loop.go REFLECT stage addition:
GateConfig{Command string; SkipWhenUnchanged bool}
// Gate runs Command in goal workspace via ExecutionBackend; exit 0 = may complete;
// non-zero output feeds next PLAN as feedback. Workspace hash cached; unchanged
// workspace skips re-running failed gate. Config per-goal + [employee.defaults].
```

### C9: Egress Policy (09)

```go
// Extends containment-tree internal/secrets proxy (DEPENDENCY: that tree's leaf 04 merged):
PolicyRule{Match string /*host suffix or CIDR*/, Action:"allow|ask|deny"}
[security.egress]{mode:"proxy|deny|allow", rules:[...], scrub_no_proxy:true}
// Proxy consults rules pre-injection; resolved IPs double-checked (LookupHost then CIDR match);
// NO_PROXY family env vars cleared for children when mode=proxy.
```

### C10: Inter-Agent Messaging (10)

```go
// Bus topic agent.message {from,to,body,msg_id}; receipts SQLite table agent_messages(id,from,to,body,state queued|delivered|read,created_at)
// Tools: send_agent_message{to,message} -> receipt id; inbox unread injection next turn start.
// Roster: platform_agents gains per-agent reachability state.
```

### C11: Model Lifecycle CLI (11)

```
meept model pull <repo-id> [--quant q4_k_m]  -> hf download into ~/.meept/models/<name> (hf hub HTTP, resumable)
meept model list / test <name>               -> inventory / 1-token probe through RuntimeManager
RuntimeManager.RegisterLocalModel(path) wires pulled models as provider endpoints.
```

### C12: GBNF-Constrained Calling (12)

```
For providers whose transport is llama.cpp server: attach "grammar" field to
completion requests when [agent.tools] gbnf_constrained=true (default false).
Grammar generated from active tool definitions: array-root JSON of tool call objects
(JSON-schema->GBNF converter covering the subset meept emits; unsupported constructs
fall back to unconstrained + warn once).
```

### C13: Browser Automation (13)

```
Tools (internal/tools/builtin/browser_*.go, chromedp dep added):
browser_navigate{url} (guard.CheckURL first; scheme http/https only)
browser_click{selector} browser_type{selector,text} browser_read_text{selector?}
browser_screenshot{} (returns image ref) browser_close{}
Chrome lifecycle: managed headless process like other runtimes; singleton per session.
```

### C14/C15: Memory Science (14, 15)

```
14: votes table memory_votes(memory_id,delta,reason,created_at); tool memory_vote{id,delta,reason};
usefulness = clamp(base + votes*W - staleness*W2 + accesses*W3); consolidation eviction uses
usefulness percentile not age alone; config [memory.usefulness].
15: types "lesson"/"procedure" in task memory; reflection collector classifies eligible
outcomes -> distilled entries (cap length, dedup TF-IDF vs existing); injected via
existing memory injection tiers with type tag.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Group |
|---|----------|------|-------------|-------------|-------|
| 01 | 01-stable-prefix-prompt.md | leaf | none | 55K | A |
| 02 | 02-lazy-tool-schemas.md | leaf | none | 65K | A |
| 03 | 03-scheduler-crash-safety.md | leaf | none | 45K | A |
| 04 | 04-shell-permission-table.md | leaf | none | 50K | A |
| 05 | 05-ssrf-guards.md | leaf | none | 45K | A |
| 06 | 06-doctor-lifecycle.md | leaf | none | 60K | B |
| 07 | 07-loop-guards.md | leaf | none | 60K | B |
| 08 | 08-quality-gated-autonomy.md | leaf | none | 55K | B |
| 09 | 09-egress-policy.md | leaf | containment tree 04 | 55K | D |
| 10 | 10-inter-agent-messaging.md | leaf | none | 60K | C |
| 11 | 11-local-model-cli.md | leaf | none | 60K | C |
| 12 | 12-gbnf-constrained-calling.md | leaf | 02, 11 | 65K | E |
| 13 | 13-browser-automation.md | leaf | 05 | 80K | E |
| 14 | 14-memory-usefulness.md | leaf | none | 60K | C |
| 15 | 15-memory-lessons-procedures.md | leaf | 14 | 55K | D |

Groups: A parallel batch 1; B batch 2; C batch 3; D batch 4; E batch 5 (heaviest last).

## Dispatch Protocol

Per group in letter order, parallel within group:

1. Read leaf; dispatch delegate_task with: FULL leaf text + relevant contracts above + conventions block + repo facts (module github.com/caimlas/meept; config schema.go json+toml tags; IDs pkg/id.Generate; AGENTS.md conventions apply; containment tree paths for cross-deps).
2. Include verbatim: "Do NOT commit. Do NOT git add. Write code, tests pass, report only. Do NOT read files back after writing."
3. Review in-session: go build ./... && go test ./internal/<pkg>/... -race; contract conformance; checklist below. Re-dispatch ≤3 cycles with findings; escalate after.
4. APPROVED -> git add <exact paths> && commit "feat(<scope>): <leaf>". Update table.

Integration phase after E: full `go test ./... -race`, `make lint-ci`, smoke: `meept doctor` runs clean; schema_mode toggle flips definition payload sizes; browser_navigate against example.com with guard denying an RFC1918 URL. Commit fixes separately.

## Review Checklist

Per leaf: tasks done; tests first/passing -race; contracts exact; no debug artifacts; typed-nil guards; mutexio-clean; errors %w; lowercase UI strings; predid-clean IDs; docs updated where leaf specifies; no scope creep.

## Coding Conventions

Go 1.24+, stdlib-first (new deps ONLY: chromedp leaf 13, rest stdlib/existing). Table-driven tests alongside. JSON5 config w/ defaults registered in schema.go defaults block. Errors fmt.Errorf %w + sentinels for refusals. Follow AGENTS.md invariants (session/conversation ids, no os.Getwd in daemon).

## Completion Tracking Table

| Child | Status | Iterations | Notes |
|-------|--------|-----------|-------|
| 01-stable-prefix-prompt | COMPLETE | 1 | a3a93ef2 (AssembleOrdered + AddSectionWithStability; complements committed loop.go refs) |
| 02-lazy-tool-schemas | COMPLETE | 1 | 86be1fb4 (indexed mode default-on + tool_view LRU; config/llm resolution + loop wiring via subagents) |
| 03-scheduler-crash-safety | COMPLETE | 1 | 33d51b2f (claimStore + coalescing; core guard pkg in 936a7d4e) |
| 04-shell-permission-table | COMPLETE | 1 | 94031f53 + Execute wiring in 4cfe9386 |
| 05-ssrf-guards | COMPLETE | 2 | 936a7d4e (guard pkg) + 85171998 (web_fetch/web_search wiring + config tests) |
| 06-doctor-lifecycle | COMPLETE | 1 | babc8f57 |
| 07-loop-guards | COMPLETE | 1 | e30c861a |
| 08-quality-gated-autonomy | COMPLETE | 1 | b87f2674 |
| 09-egress-policy | COMPLETE | 2 | af109d77 (EgressPolicy engine + proxy enforcement, landed as leaf-09 complement; docs at HEAD since 0dbeb378-era) |
| 10-inter-agent-messaging | COMPLETE | 1 | 4cfe9386 |
| 11-local-model-cli | COMPLETE | 2 | b562c30a (modelstore/model CLI; IPv6 test-premise fix same commit) |
| 12-gbnf-constrained-calling | COMPLETE | 1 | d746ff54 |
| 13-browser-automation | COMPLETE | 1 | 92f7876a ([browser] config struct landed in 0dbeb378) |
| 14-memory-usefulness | COMPLETE | 2 | 2e69e57d (repaired abandoned WIP + wired consolidation) |
| 15-memory-lessons-procedures | COMPLETE | 2 | ac41a129 (subagent died on API 429 mid-run; dedup + FTS injection fixed in-session) |

## Integration Test Plan

Cross-boundary: indexed mode + tool_view roundtrip executes a real tool; gate-blocked goal cannot complete; browser tool blocked by ssrf guard on private IP; votes shift eviction ordering; receipts delivered across two employees; doctor detects seeded stale pidfile. Commands: go test ./... -race; make lint-ci; manual smokes listed.

## Structural Completeness Check

Required orchestrator sections present: Dispatch Protocol ✓ Interface Contracts ✓ Review Checklist ✓ Coding Conventions ✓ Completion Tracking ✓ Integration Test Plan ✓.

## Notes

- Cross-tree deps: leaf 09 waits for containment tree merge (its proxy). Leaf 13 consumes leaf 05 guard.
- Existing cycle detector documented in docs/features.md — leaf 07 extends, never duplicates.
- Defaults preserve current behavior everywhere except scheduler coalescing (strict improvement) and eviction scoring (flagged [memory.usefulness] enabled=false initially).
