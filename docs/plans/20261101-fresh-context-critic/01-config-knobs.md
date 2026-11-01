# Config Knobs - Implementation Leaf

> For the implementing agent: Implement ALL tasks below. Do NOT commit.
> Do NOT git add. Do NOT edit forbidden files in contract C10.
> After writing a file, do NOT read it back. Report files touched and verify output.

## Meta

- Parent: master.md
- Scope: Add three default-false plan flags to PlansConfig. No runtime behavior.
- Dependencies: 00T TESTS_VALIDATED
- Estimated Context: 30K
- Concurrency Group: A

## Goal

Land contract C1 so later leaves can wire setters without editing schema.go. Absent config stays false. This leaf does not spawn a critic and does not change the critique loop.

## Context

PlansConfig is internal/config/schema.go:501. SelfSealEnabled at line 536 is the pattern: bool, default false, comment says ships dark. NormalizePlansDefaults (line 543) clamps the round cap only. Do not add the new flags to that clamp in a way that turns them on.

## Verified Anchors

- internal/config/schema.go:527 - ComplexMaxCritiqueRounds field and json tag
- internal/config/schema.go:536 - SelfSealEnabled field
- internal/config/schema.go:543 - NormalizePlansDefaults

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract C1, exact tags:

```go
FreshCriticEnabled   bool `json:"fresh_critic_enabled"   toml:"fresh_critic_enabled"`
HostileCriticEnabled bool `json:"hostile_critic_enabled" toml:"hostile_critic_enabled"`
InterviewOptionGates bool `json:"interview_option_gates" toml:"interview_option_gates"`
```

### What This Leaf Consumes

- C1 from master.md. No other contract.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add the three fields under SelfSealEnabled with comments that say default false, ships dark | Modify: internal/config/schema.go | `go test -p 2 ./internal/config/ -count=1 -run TestConfig` | Field missing, JSON unmarshal drops unknown only if the struct lacks the field | Unmarshal of each key sets the bool; omit leaves false | Given a PlansConfig zero value; When JSON has no critic keys; Then all three bools are false |
| T2 | Pin the omit-is-false behavior in the existing config test file if one already covers PlansConfig; otherwise add the pin next to an existing PlansConfig test in internal/config, not a new file if an existing test file already owns schema pins | Modify: the existing schema test file that already unmarshals PlansConfig | same go test | n/a after T1 | Test passes | Given JSON `{"plans":{}}`; When loaded; Then FreshCriticEnabled, HostileCriticEnabled, and InterviewOptionGates are false |

Do not edit internal/daemon/daemon.go. Leaf 07 wires the setters.

Do not re-activate wiring_test.go.staged. That file belongs to leaf 07.

## Self-Verification Checklist

- [ ] Three fields exist with the exact json and toml tags in C1
- [ ] Zero value and omitted JSON both yield false
- [ ] NormalizePlansDefaults does not set any new flag to true
- [ ] No critique-loop behavior change
- [ ] No C10 file touched

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Tags match C1 exactly
- [ ] Defaults stay false
- [ ] daemon.go not in the diff
- [ ] No C10 file in the diff

Output: APPROVED or specific gaps with file + line references.

## Notes

Do not edit config/agents/verifier/AGENT.md, internal/agent/prompts/verifier.go, or internal/agent/verification_hook.go.
