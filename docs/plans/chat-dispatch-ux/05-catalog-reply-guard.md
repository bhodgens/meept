# Catalog Reply Guard - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** A reply-shaped guard in AgentLoop.RunOnce keeps raw platform_* tool catalogs / agent rosters / status JSON from becoming the user's chat reply.
- **Dependencies:** none
- **Estimated Context:** 25K
- **Audit references:** finding F5 (75-tool catalog, 28-agent roster with system-prompt bodies, status-JSON dump as chat replies)

## Goal

Three 2026-09-04 runs returned machine-shaped output to a naive user: a
75-tool catalog ("*Total: 75 tools*"), an "## Available Agents" roster
embedding specialist system-prompt fragments ("You are Meept, an autonomous
assistant serving your creator. Your core values:"), and a raw
platform_status JSON dump (session message 792). This leaf adds one guard
function that classifies such replies and substitutes a short user-language
message. The model's genuine prose replies pass through untouched.

## Context

AgentLoop.RunOnce (internal/agent/loop.go, the per-turn entry — find the
final-response assembly; the tool-execution path is executeToolCalls at
loop.go:5693) returns the assistant text that becomes ChatResponse.Reply.
The offender tools are platform_status / platform_tools / platform_agents
(internal/tools/builtin/platform.go — their outputs end with
"*Total: N tools*" / "*Total: N agents*" and the roster uses a
"## Available Agents" header). loop_hardening_test.go:54 shows test
patterns for formatting tool results.

Key files to understand before implementing:
- internal/agent/loop.go - RunOnce response assembly; where the final string is chosen before return
- internal/agent/loop_hardening_test.go - existing loop-level test setup
- internal/tools/builtin/platform.go - the output shapes being guarded

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/loop.go (new file internal/agent/reply_guard.go allowed)
// func sanitizeCatalogReply(reply string) string
//   - returns reply unchanged when it does not look like raw tool output
//   - when dominated by catalog/roster/status dumps returns a short
//     lowercase user-language fallback naming what happened
// RunOnce (or the single response-return site) calls it on the final text.
// Detection heuristic (documented in the function):
//   rawJSON     = strings.TrimSpace starts with '{' or '[' AND contains a
//                 platform_* payload key ("status": "running", "uptime_seconds",
//                 "tools": [ , "agents": [ )
//   agentRoster = contains "## Available Agents" header
//   toolCatalog = contains "*Total: " AND " tools*" or " agents*"
// Owner: 05. Consumers: 07 (same file area), 10 (e2e asserts).
```

### What This Leaf Consumes

```
// nothing outside stdlib
```

## Tasks

### Task 1: sanitizeCatalogReply

**Objective:** Pure function + full unit coverage.

**Files:**
- Create: `internal/agent/reply_guard.go`
- Test: `internal/agent/reply_guard_test.go`

**Step 1: Write failing test**

```go
func TestSanitizeCatalogReply(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantSame  bool   // true = pass-through expected
		contains  string // when !wantSame, fallback must contain this
	}{
		{"prose untouched", "I created water_reminder.py for you. Run it with python3.", true, ""},
		{"empty untouched", "", true, ""},
		{"markdown answer untouched", "## how to run\n\n1. open terminal", true, ""},
		{"status json", "{\n  \"status\": \"running\",\n  \"uptime_seconds\": 1840.8,\n  \"version\": \"1.0\"\n}", false, "platform"},
		{"tool catalog", "### Shell Tools\n\n- **shell**: Execute a shell command...\n\n*Total: 75 tools*", false, "tools"},
		{"agent roster", "## Available Agents\n\n### Coder (`coder`)\n**Role**: executor\n\nYou are Meept, an autonomous assistant serving your creator.\n\n*Total: 28 agents*", false, "agents"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeCatalogReply(tc.in)
			if tc.wantSame {
				if got != tc.in {
					t.Errorf("want pass-through, got %q", got)
				}
				return
			}
			if got == tc.in {
				t.Errorf("catalog reply passed through unguarded")
			}
			if tc.contains != "" && !strings.Contains(strings.ToLower(got), tc.contains) {
				t.Errorf("fallback %q must mention %q", got, tc.contains)
			}
		})
	}
}
```

**Step 2: verify failure** — `go test -p 2 ./internal/agent/ -run TestSanitizeCatalogReply -v` → FAIL (undefined).

**Step 3: implement** per the contract heuristic; fallback template (lowercase, short):

```
"i looked up the platform <tools|agents|status> information, but that isn't a useful answer on its own. ask me to do something specific — for example 'make me a program that ...' — and i'll get to work."
```

Pick the noun from which detector fired; prefer prose pass-through when a
reply has >40% non-symbol word content outside the matched header (cheap
check: catalog replies are dominated by '-'/'*' bullets and JSON braces).

**Step 4: verify pass** — same run → PASS.

### Task 2: wire into RunOnce response path

**Objective:** The single final-response return site calls the guard.

**Files:**
- Modify: `internal/agent/loop.go` (response assembly in RunOnce)

**Step 1: Write failing test** — loop-level: construct minimal AgentLoop
(mirror loop_hardening_test.go:54 setup), simulate a turn whose final text
is the tool-catalog string, assert RunOnce's returned reply != catalog.
If driving a full turn is too heavy in unit scope, test the seam function
you add (`applyReplyGuard(final string) string`) and call it from RunOnce —
cover both.

**Step 2: verify failure, implement (one-line call at the response-assembly
site), verify pass** with `go test -p 2 ./internal/agent/ -run 'ReplyGuard|SanitizeCatalog' -v`.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec
- [ ] No scope creep
- [ ] Guard is additive: any reply not matching the heuristic is byte-identical

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] sanitizeCatalogReply covers JSON-dump, roster, catalog detectors
- [ ] Fallback text lowercase, short, names the category
- [ ] RunOnce (or seam) wiring present and covered
- [ ] No false positive on prose/markdown replies (test-proven)
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Do NOT touch the platform_* tools themselves — delegation and TUI panels
  legitimately consume their outputs (agents.list is a real RPC surface).
- The roster case leaks system-prompt bodies; the guard removes them from
  the CHAT path. True prompt-leak prevention in tool outputs is out of scope.
- Leaf 07 edits skill-discovery elsewhere in loop.go — keep hunks disjoint.
