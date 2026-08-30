# SecurityEngine Rules for ACP Tools — Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD.
> Do NOT commit. Do NOT use read_file on existing source files.

## Meta

- **Parent:** ../master.md
- **Scope:** pkg/security: base rules + tests for acp_agent verb gating (HIGH/LOW/fail-closed)
- **Dependencies:** none (runs in parallel with leaf 01; needs no other leaf)
- **Estimated Context:** 35K
- **Concurrency Group:** A

## Goal

Every acp_agent call passes through the SecurityEngine like every other tool family. Class rules (parent Contract 7): launch and send are HIGH (spawn a process / inject content into an external agent); read and stop are LOW; anything unrecognized under the acp_agent prefix fails closed HIGH. Mirror the cua-driver precedent exactly (pkg/security/computer_use.go): prefix-matched base rules + DB-seeded override precedence.

## Context

Key files:
- pkg/security/computer_use.go — ComputerUseRule: the exact pattern to mirror (prefix match on registered tool name, risk levels, tests)
- pkg/security/computer_use_test.go — test shape to mirror
- internal/security/engine.go lookupBaseRule (~line 512) — where base rules resolve; verify whether the acp rule belongs HERE or in pkg/security alongside computer_use (computer_use lives in pkg/security and engine consults it — follow that same direction)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// pkg/security/acp_agent.go (mirroring computer_use.go)
// ACPAgentRule: prefix "acp_agent"
//   verb=launch|send -> HIGH
//   verb=read|stop   -> LOW
//   unknown acp_agent.* -> HIGH (fail-closed)
// Exported helper used by engine lookup or registered the same way ComputerUseRule is.
```

Behavior contract: with confirmation enabled, a HIGH acp_agent call requires user confirmation; LOW runs free; unknown fails closed. With `[tools.security]` confirmation disabled, HIGH executes without prompting (existing engine semantics — do not change engine behavior, only add rules).

### What This Leaf Consumes

Existing engine/rule plumbing only. No leaf-05 code needed — rules key on the tool NAME string ("acp_agent"), which is frozen in SHARED-CONVENTIONS §3.

## Tasks

### Task 1: Failing tests — rule classification

**Files:** Create: pkg/security/acp_agent_test.go (mirror computer_use_test.go shape)

Table-driven: {name: "acp_agent", args:{verb:"launch"}} -> HIGH; send -> HIGH; read -> LOW; stop -> LOW; acp_agent with garbage verb -> HIGH; a DIFFERENT tool name ("browser_navigate") -> not classified by this rule (no cross-talk).

### Task 2: Rule implementation

**Files:** Create: pkg/security/acp_agent.go

Mirror ComputerUseRule structure: param-aware (reads verb from args map with two-value assertions). Register/lookup path identical to how engine.go consults computer_use (grep lookupBaseRule and the registration direction; if engine needs a call site addition, add the MINIMAL hook there and keep the rule logic in pkg/security).

### Task 3: Engine integration test

**Files:** Extend: pkg/security/acp_agent_test.go

Through the real engine path (however computer_use_test drives it): check_permission / evaluate for an acp_agent launch returns the confirmation-required posture; read returns run-free posture; DB-seeded override on acp_agent (if the override mechanism applies to prefix rules for cua-driver, it applies here — test one override case if the mechanism supports it; otherwise document why not in your report).

## Self-Verification Checklist

- [ ] go build ./pkg/security/ ./internal/security/ green
- [ ] go test ./pkg/security/ -race -count=1 green
- [ ] Fail-closed proven: unknown verb -> HIGH, not LOW, not skipped
- [ ] No changes to engine.go beyond (at most) a minimal lookup hook — report the exact diff if any
- [ ] No TODOs

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Classification table matches Contract 7 exactly
- [ ] Mirrors computer_use.go structure (reviewer diff the two files for shape)
- [ ] Cross-talk test proves other tools unaffected
- [ ] Tests table-driven, -race clean

## Notes

- The verb lives inside tool ARGS; rules must parse args (that's why computer_use.go is the right template — it also inspects args, unlike plain name-prefix rules).
- Do NOT gate on `[acp] enabled` here — security rules are static posture; the disabled path never reaches the engine because the tool returns "acp disabled" first (leaf 05).
