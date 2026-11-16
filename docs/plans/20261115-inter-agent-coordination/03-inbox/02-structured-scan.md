# Structured Taint Scan - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. The gate tests already exist and are committed - make them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT git add. Do NOT use read_file on existing source files - explore with search_files or terminal cat. After writing a file, do NOT read it back - write once and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** In prepareHandoff, when the raw peer output parses as an AgentInboxBody with a structured envelope, scan each structured field (tool_targets, file_paths, plan_entries) plus the human body via the existing boundary patterns; legacy free text keeps the whole-blob scan byte-identically.
- **Dependencies:** 03-01 REVIEWED and committed (AgentInboxBody JSON contract)
- **Estimated Context:** 45K
- **Concurrency Group:** B
- **File ownership:** `internal/agent/collaboration_handoff.go` ONLY. Do NOT import `internal/employee` (cycle) — mirror the JSON shape locally per C7.

## Goal

Make the inter-agent taint scan precise: structured messages get field-keyed deny matching (a hit names the field in the block reason); free-text messages keep today's behavior exactly.

## Context

- `internal/agent/collaboration_handoff.go:97-136` — prepareHandoff: records the untrusted segment, then `h.boundary.InspectHandoffContent(rawOutput)` (whole blob).
- `internal/security/agent_boundary.go:121-141` — InspectHandoffContent(content string) []InterAgentBoundaryMatch (class + pattern + reason).
- `internal/agent/taint_hooks.go` / `internal/security/taint` — RecordUntrustedSegment stays as-is (one segment per handoff, keyed "collab:<driver>:<role>").

## Verified Anchors

- `internal/agent/collaboration_handoff.go:111` — the InspectHandoffContent call (insert the structured branch before it).
- `internal/security/agent_boundary.go:33-37` — InterAgentBoundaryMatch{Class, Pattern, Reason}.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/collaboration_handoff.go — local mirror (no employee import):
type inboxBodyMirror struct {
    Body       string `json:"body"`
    Structured *struct {
        ToolTargets []string `json:"tool_targets"`
        FilePaths   []string `json:"file_paths"`
        PlanEntries []string `json:"plan_entries"`
    } `json:"structured"`
}
// prepareHandoff logic:
//   1. RecordUntrustedSegment (unchanged)
//   2. off-mode / nil boundary -> pass (unchanged)
//   3. try json.Unmarshal(rawOutput, &mirror); if ok && mirror.Structured != nil:
//        for each field in [ToolTargets, FilePaths, PlanEntries]:
//            for each value: matches := boundary.InspectHandoffContent(value)
//            on first hit: warn+block with reason "<class> pattern in <field> of peer output from <driver> (<role>)"
//        also scan mirror.Body via InspectHandoffContent (human text is still untrusted)
//   4. else: existing whole-blob scan (unchanged)
```

### What This Leaf Consumes

- C4 JSON contract (03-01) — via the local mirror (pinned tags).
- C7 scan seam (this leaf defines the behavior).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Structured branch: field-keyed scan + block reason names field | Modify: `internal/agent/collaboration_handoff.go` | `go test ./internal/agent/ -run 'TestHandoff_StructuredScan' -v` | FAIL: no structured branch | PASS: structured file_path with deny pattern blocks in block mode, reason names "file_paths" | Given block-mode boundary + deny pattern; When handoff with structured file_path matching; Then blocked, reason contains field name |
| T2 | Legacy free text scan unchanged | Modify: `internal/agent/collaboration_handoff.go` | `go test ./internal/agent/ -run 'TestHandoff' -v` (existing) | (existing tests stay green) | PASS: all pre-existing handoff tests pass | Given legacy text; When handoff; Then same block/pass as before |
| T3 | Structured clean body passes; body field still scanned | Modify: `internal/agent/collaboration_handoff.go` | `go test ./internal/agent/ -run 'TestHandoff_StructuredScan' -v` | FAIL: no branch | PASS: clean structured passes; deny pattern in body text blocks | Given structured with clean fields but deny pattern in body; When handoff; Then blocked (body scan) |

**T1 expansion:** mirror struct + branch. Field iteration order: ToolTargets, FilePaths, PlanEntries, then Body. On hit in warn mode: log with `field` attr. In block mode: return `(true, "<class> pattern in <field> of peer output from <driver> (<role>)")`. In off mode: return early as today.

**T2:** run the full existing handoff test set (`go test ./internal/agent/ -run 'Handoff|Collab' -v`) — all must pass without modification. If any pre-existing test breaks, the branch is wrong: fix the branch, not the test.

## Self-Verification Checklist

- [ ] `go build ./internal/agent/...` clean; no import of internal/employee (`go list -deps ./internal/agent | grep internal/employee` — must be empty)
- [ ] `go test ./internal/agent/ -run 'Handoff' -v` — all PASS (new + existing)
- [ ] Legacy path byte-identical (existing tests green)
- [ ] Gate tests untouched; `go fmt`; only collaboration_handoff.go modified

**DO NOT COMMIT.**

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Local mirror struct matches C4 JSON tags exactly
- [ ] Field-keyed scan + reason; body still scanned
- [ ] Legacy scan unchanged (existing tests green)
- [ ] No employee import; gate tests untouched; no line-number corruption

Output: APPROVED or specific gaps.