# acp_agent Tool — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ../master.md
- **Scope:** internal/tools/builtin/acp_agent.go: single meta-tool exposing launch/send/read/stop verbs over acp.Manager
- **Dependencies:** 04 (Manager)
- **Estimated Context:** 50K
- **Concurrency Group:** D (owns internal/tools/builtin/acp_agent.go only)

## Goal

Models see ONE tool: `acp_agent`. Verbs map to Manager/Session operations. The tool layer translates Manager errors into tool-result errors with lowercase text, enforces the "daemon CWD is not the user's project" rule by passing the session's working dir (never os.Getwd), and returns structured evidence (agent id, state, elapsed).

## Context

- internal/tools/builtin/ — existing tool family conventions: ToolResult envelope with string Evidence, SetX nil-guards, runner-function pattern (runX(ctx, injectableDeps, ..., io.Writer)) so tests need no registry
- internal/acp/manager.go (from 04) — Manager, sentinel errors
- How the MCP tool family registers into the registry: internal/tools/mcp + registration in daemon wiring — leaf 07 wires acp the same way; this leaf only builds the tool.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/builtin/acp_agent.go
type ACPAgentTool struct { /* manager getter (injectable), config */ }
func NewACPAgentTool(getMgr func() *acp.Manager) *ACPAgentTool
func (t *ACPAgentTool) SetEnabled(bool)  // nil-guard style per repo setter rule

// tools.Tool implementation:
// Name(): "acp_agent"
// Parameters(): {"agent": string req, "verb": enum launch|send|read|stop req,
//                "message": string opt, "session": string opt (future multi-session)}
// Execute(): dispatch table by verb:
//   launch -> GetOrCreate + State report
//   send   -> GetOrCreate + Session.Send(message)
//   read   -> drain Events channel snapshot (recent chunks/tool calls) for agent
//   stop   -> Manager.Stop(agent)
// Manager disabled -> tool Result error "acp disabled" (lowercase), not a panic.
```

### What This Leaf Consumes

From 04: acp.Manager, GetOrCreate, Stop, sentinel errors, Session.Send/Events/State. From repo: tools.Tool interface, ToolResult envelope conventions.

## Tasks

### Task 1: Failing tests — verb dispatch (fake manager)

**Files:** Create: internal/tools/builtin/acp_agent_test.go

Minimal fake Manager (interface or closure injection — pick the lighter option consistent with sibling builtin tools): launch returns state report; send returns agent reply text; read returns drained events; stop returns stopped. Missing agent param -> validation error. Bad verb -> validation error listing valid verbs. send without message -> validation error.

### Task 2: Failing tests — disabled path + error translation

Disabled (nil manager or Enabled()==false): every verb returns "acp disabled" lowercase result error; no calls reach the manager (fake counts calls = 0). ErrAgentNotFound/ErrAgentDisabled/ErrMaxAgents -> specific lowercase messages including the agent id (errors.Is dispatch, not string matching).

### Task 3: Tool implementation

**Files:** Create: internal/tools/builtin/acp_agent.go

Follow builtin family structure exactly (compare a sibling like the browser tool family: param validation, context use, ToolResult assembly, Evidence). Lowercase all user-visible strings. Evidence: {"agent": id, "state": stateName, "elapsed_ms": n, "reply": text?}.

### Task 4: Schema mode compatibility

**Files:** Extend test file

The registry has schema-mode compression (tool_view meta-tool). Assert the tool's Parameters() survives full-mode and the compact schema path (grep how sibling tools declare Parameters; mirror exactly so tool_view doesn't choke). Test: schema renders without panic in both modes (reuse the registry's own rendering helper if exported; otherwise construct definitions directly).

## Self-Verification Checklist

- [ ] go build ./internal/tools/... green
- [ ] go test ./internal/tools/builtin/ -race -count=1 green (full package)
- [ ] Setter has nil guard (repo rule)
- [ ] All user-visible strings lowercase
- [ ] No os.Getwd anywhere (grep the new file)
- [ ] No TODOs, no debug prints

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Verb table exactly launch|send|read|stop; unknown rejected with valid list
- [ ] Error translation covers all four sentinel errors + disabled
- [ ] File scope: acp_agent.go + its test ONLY
- [ ] Tool name constant "acp_agent" — security leaf 06 rules key on this name
- [ ] Conventions: ToolResult Evidence shape, no bare panic, %w wrapping

## Notes

- This tool does NOT auto-launch sessions implicitly on `send` unless GetOrCreate does (it does — lazy). `launch` exists so a model can warm an agent and see capability state before committing to a send.
- `session` param accepted but unused in v1 (single session per agent id); kept in schema so v2 multi-session is non-breaking. Document that in the tool description text.
