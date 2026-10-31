# 01: Frontmatter Groups - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** `groups:` field on AGENT.md frontmatter through the 5-step field
  pipeline (C4 agent half).
- **Dependencies:** 02-groups-model (sibling of parent, REVIEWED)
- **Estimated Context:** 45K
- **Concurrency Group:** B (serialized within branch)

## Goal

`groups: [coders, oncall]` in an agent's AGENT.md frontmatter reaches
AgentSpec.Groups - and survives merge with base specs (step 5 is where fields
die silently).

## Interface Contracts (From Parent)

C4: `AgentMetadata.Groups []string yaml:"groups,omitempty"` -> AgentSpec.
Groups (json tag `groups,omitempty`). Unknown group name: Warn + ignored at
resolution (resolution lives with 04's loop wiring; THIS leaf delivers the
field + pipeline + a GroupsResolved helper if the registry path makes it
natural). Merge rule: discovery shadowing takes the higher tier's value
OUTRIGHT (scalar-replacement semantics, NOT additive union - matches
model_alias precedent, not additional_tools).

## Verified Anchors (5-step pipeline - missing ANY step = silent loss)

1. internal/agents/models.go AgentMetadata struct (~line 87 neighborhood)
2. internal/agents/parser.go parseMetadata - underscore yaml key parses
   natively; add a merge lane ONLY if altMeta carries it (line ~235 pattern)
3. internal/agent/spec.go AgentSpec struct (~line 123 neighborhood)
4. internal/agent/registry.go definitionToSpec (~line 1379 pattern)
5. internal/agent/registry.go mergeSpec (~line 1318 pattern) - scalar
   replacement, documented in a comment citing model_alias precedent

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | Steps 1-3: AgentMetadata.Groups + parse (underscore form; altMeta lane) + AgentSpec.Groups | internal/agents/models.go, parser.go; internal/agent/spec.go; parser_test additions | `go test -p 2 ./internal/agents/` | field absent | YAML `groups:` parses; altMeta merge lane works |
| T2 | Steps 4-5: definitionToSpec maps it; mergeSpec scalar-replaces with precedent comment | internal/agent/registry.go, +test | `go test -p 2 ./internal/agent/ -run TestSpecMerge` | mapping absent | override AGENT.md replaces base groups (not union) |
| T3 | Discovery shadowing consistency: shadowed definition's groups follow the same outright-take rule | internal/agents/discovery.go, +test | `go test -p 2 ./internal/agents/ -run TestDiscovery` | n/a | union helper NOT used for groups (contrast additional_tools) |

## Self-Verification Checklist

- [ ] All FIVE pipeline steps touched (grep each site)
- [ ] Scalar replacement documented with model_alias precedent reference
- [ ] Gate tests GREEN and unmodified; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each]
3. Deviations: [none / list]

## Review Checklist (For Review Agent)

- [ ] Step 2 altMeta lane present (camelCase guard unnecessary - underscore primary)
- [ ] mergeSpec does NOT union (deliberate, commented)
- [ ] Existing catalog AGENT.md files still parse (config load test green)
