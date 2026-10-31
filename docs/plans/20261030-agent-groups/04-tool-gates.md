# 04: Tool Gates (Dispatch Intersection + Call-Time Enforcement + Assignment Validation) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../master.md
- **Scope:** The core enforcement leaf: group resolution feeds BOTH existing
  gates (dispatch filterTools intersection; executor call-time check for ALL
  agents), plus assignment-time validation of policy tool names (C6).
- **Dependencies:** 01-permissions branch (COMPLETE), 02-groups-model (REVIEWED)
- **Estimated Context:** 70K
- **Concurrency Group:** C (after branch 01 - components.go/executor.go contention)

## Goal

For a grouped agent: the LLM never sees a denied tool (dispatch), a call
cannot slip through via a parent-registry path (call-time), and a policy that
names an unregistered tool can never be saved or booted (validation).
Ungrouped agents are byte-identical to today (D7) unless default_managed=true.

## Interface Contracts (From Parent)

- C3 Resolved.AllowedTools/DeniedTools - consumed via internal/groups.
- C6: validation at boot AND groups.assign RPC time, against the LIVE tool
  registry, using the EXISTING grant normalization (spec.go GrantedToolNames
  precedent: "shell_execute" -> registered "shell"). Reject names the
  offender.
- D8: grouped agent resolution ALWAYS yields a decision record; empty
  effective allowlist = deny-all at the gates (the resolution layer carries
  Managed=true so FilteredToolRegistry's empty=all legacy NEVER fires).
- C2 rule_source: group_deny/group_allow stamped by this leaf's call-time path.

## Verified Anchors

- Dispatch gate: internal/agent/registry.go:777 filterTools - allowed set
  becomes spec.ScopedToolNames ∩ Resolved.AllowedTools (∪ override allows,
  leaf 07's reader, injected via a resolver seam - define the seam here,
  07 fills it).
- Call-time gate: internal/agent/executor.go:1675-1716 checkPermission -
  generalize the PreExecChecker map consultation: employee checker if
  registered, ELSE group deny/allow verdict from the loop's resolution record,
  ELSE (default_managed=true only) the `default` policy; default_managed=false
  + ungrouped => skip (today's behavior).
- Loop-carried resolution: the AgentLoop needs its Resolved record at
  construction - registry createLoop path; THREE-SITE RULE: template loop,
  registry loops, verifier children (wave-proven wiring-defect class; a
  resolver record wired only on the template loop leaves dispatched turns
  ungated while appearing wired). RegistryConfig gains the field + opts; ALL
  creation sites wire it.
- Normalization helper: internal/groups exported normalizer-func seam (leaf 02).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | Loop resolution record: resolve at loop creation, carry on AgentLoop (typed-nil safe), all three creation sites | internal/agent/registry.go, loop.go, +test | `go test -p 2 ./internal/agent/ -run TestGroupResolution` | record absent | grouped loop carries Resolved; ungrouped+unmanaged carries nil record (compat) |
| T2 | Dispatch intersection in filterTools (deny removes from definitions AND Get); deny-all honored via explicit empty-allowlist marker, not the legacy empty slice | internal/agent/registry.go, executor.go (FilteredToolRegistry), +test | `go test -p 2 ./internal/agent/ -run TestDispatchIntersection` | no intersection | grouped agent: denied tool absent from GetDefinitions, Get()=nil; Managed+empty => everything denied |
| T3 | Call-time check for ALL agents (employee checker > group verdict > default policy > legacy skip), stamping group_deny/group_allow rule sources | internal/agent/executor.go, +test | `go test -p 2 ./internal/agent/ -run TestCallTimeGroupGate` | stage skips non-employees | deny via group short-circuits with C2 line; bypass attempt via parent registry still caught |
| T4 | Assignment-time validation (C6): boot re-validate + RPC validate hook (RPC methods themselves are leaf 03-membership/03 - expose ValidatePolicyTools(policies, normalizer) error) | internal/groups/validate.go extension, +test | `go test -p 2 ./internal/groups/ -run TestValidatePolicyTools` | absent | unregistered name => error naming it; normalized match passes |
| T5 | Override seam: resolver accepts injected extra-allows (leaf 07 fills; nil default) | internal/groups/resolve opts, +test | `go test -p 2 ./internal/groups/ -run TestOverrideSeam` | absent | override allow rescues a denied tool; never widens scopes |

## Self-Verification Checklist

- [ ] Three-site rule: resolution record wired at template + registry + verifier creation
- [ ] Ungrouped agents: zero behavior change with default_managed=false (existing tests untouched and green)
- [ ] D8: Managed empty-allowlist denies all at BOTH gates
- [ ] -race green on internal/agent; gate tests GREEN and unmodified

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each]
3. Deviations: [none / list]

## Review Checklist (For Review Agent)

- [ ] ALL loop creation sites wired (grep GetOrCreateWired / createLoop sites)
- [ ] Precedence at call time matches C3 (employee first - constitutions keep their say)
- [ ] No normalization duplicated (groups helper reused)
- [ ] Legacy empty-allowlist semantics untouched for ungrouped agents
