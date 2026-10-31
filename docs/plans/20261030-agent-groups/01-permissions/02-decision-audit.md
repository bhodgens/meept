# 02: Decision Audit Lines - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Every checkPermission verdict emits one structured slog line with
  its rule source (port of the dead engine's logDecision concept, C2) - log
  lines, not a DB.
- **Dependencies:** 01-fence-stage.md (REVIEWED)
- **Estimated Context:** 45K
- **Concurrency Group:** A (serialized)

## Goal

An operator can answer "why was this tool call denied?" from the daemon log,
and the groups rule sources (group_deny/group_allow) have a defined home from
day one - before leaf 04 introduces them.

## Interface Contracts (From Parent)

C2 (verbatim shape): msg="tool permission decision", keys decision, action,
tool, agent, groups, rule_source, risk, reason. Denies WARN, allows DEBUG;
config security.groups.decision_log_level default "deny-warn" ("all" and
"off" also valid). No I/O under lock; no DB.

rule_source vocabulary (FROZEN): base_rule, group_deny, group_allow, fence,
employee_pre_exec, override, confirmation_gate, financial, unknown_action.

## Verified Anchors

- pkg/security/permissions.go:356 CheckPermissionForAgent - the Result gains
  RuleSource (CheckResult struct extension; all return sites stamped).
- Executor seam: internal/agent/executor.go:1675-1716 checkPermission - emit
  the line HERE (has agent, groups via the resolution record after leaf 04;
  this leaf logs groups="-" placeholder).
- Config: internal/config/schema.go SecurityConfig neighborhood - the
  decision_log_level knob (leaf 02-groups-model owns the groups section; this
  leaf adds ONLY the log-level field if not already present - coordinate:
  if 02-groups-model already REVIEWED, extend its struct; else add the field
  and note the overlap in the report).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | CheckResult.RuleSource field + stamp every return site in pkg/security | pkg/security/permissions.go, +unit test | `go test -p 2 ./pkg/security/ -run TestRuleSource` | field absent | every deny/allow carries a vocabulary value; zero empty rule_source on deny |
| T2 | Executor emits the C2 line per verdict (slog; level per config) | internal/agent/executor.go, +test with captured handler | `go test -p 2 ./internal/agent/ -run TestDecisionLog` | no line | deny=>WARN w/ rule_source; allow=>DEBUG; "off" suppresses |
| T3 | decision_log_level knob honored (deny-warn default) | internal/config/schema.go or groups section, +test | `go test -p 2 ./internal/config/` | knob absent | three levels behave |

## Self-Verification Checklist

- [ ] Line shape EXACTLY C2 (keys, vocabulary)
- [ ] No lock held while logging; no allocation-heavy work per call beyond line build
- [ ] Gate tests GREEN and unmodified; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each]
3. Overlap note: [did you touch the groups config struct 02 owns?]

## Review Checklist (For Review Agent)

- [ ] All pkg/security return sites stamped (grep CheckResult{ count vs stamps)
- [ ] Log line at the EXECUTOR seam, not inside pkg/security (keeps pkg pure)
- [ ] Vocabulary matches C2 verbatim
