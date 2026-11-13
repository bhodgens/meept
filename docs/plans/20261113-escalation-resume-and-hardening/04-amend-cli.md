# 04 Agents Amend CLI Fix - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test. Do NOT commit - the orchestrator handles all
> git operations after review. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. After writing a file,
> do NOT read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Fix `meept agents amend` to send {id, fields, reason} per the handler contract; add a --reason flag.
- **Dependencies:** 00T gate tests
- **Estimated Context:** 35K
- **Concurrency Group:** A
- **Audit references:** CLI/handler shape mismatch (agents_cmd.go:853-857 vs handler.go:256-260)

## Goal

Make `meept agents amend <id> --field=<key> <value>` actually work: build the `fields` map the RPC handler requires, decode JSON-looking values, support `--reason`, and print the handler's response shape. The handler is CORRECT and stays untouched — this is a cmd-side fix.

## Context

- Bug: cmd/meept/agents_cmd.go:853-857 sends `{"id", "field", "value"}` (singular). Handler amendRequest (internal/employee/handler.go:256-260) requires `{"id", "fields" map, "reason" omitempty}` and rejects empty Fields with "at least one field is required" (handler.go:538-540). So the CLI ALWAYS fails today.
- Reference correct caller: internal/comm/http/api_handlers.go handleConstitutionAmend dispatches `{"id","fields","reason"}` — the GUI path already works.
- Response the handler returns: {id, status:"amendment_proposed", plan_id, approval_url} (handler.go:550-555) — print it readably. On installs without a PlanCreator the amendment applies directly (status may differ — print whatever comes back; do not hard-code expectations).
- Parity check: cmd/meept-lite/handlers.go:51/:131 has a case "amend" — verify whether it shares the broken shape and fix for parity if so (repo rule: TUI/GUI/CLI parity).
- Docs: docs/reference CLI page for agents amend must match the new flags (repo doc policy: same commit).

## Verified Anchors

- cmd/meept/agents_cmd.go:822-878 - amend subcommand (Use: "amend <id> --field=<key> <value>", cobra.ExactArgs(2))
- cmd/meept/agents_cmd.go:853-857 - the broken client.Call payload
- internal/employee/handler.go:256-260 - amendRequest{ID, Fields map[string]any, Reason omitempty}
- internal/employee/handler.go:538-540 - empty-Fields rejection ("at least one field is required")
- internal/employee/handler_test.go:31 - handler-side shape pin (keep green)
- internal/comm/http/api_handlers.go:358-378 - correct-shape reference caller

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// cmd/meept/agents_cmd.go amend payload (Contract C4):
client.Call("agents.amend", map[string]any{
    "id": agentID,
    "fields": map[string]any{field: parsedValue},
    // "reason" key present only when --reason non-empty
})
// parsedValue: try json.Unmarshal(value) -> any; on success send decoded
// (arrays/objects/numbers/bools); else send the raw string.
```

### What This Leaf Consumes

- agents.amend RPC — existing handler, unchanged

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Build Fields map + omit-empty reason; replace the singular payload | Modify: cmd/meept/agents_cmd.go:822-878 | `go test -p 2 ./cmd/meept/ -run TestAmendCmd_SendsFieldsShape` | gate RED (payload has "field" key, no "fields") | GREEN | Given --field=role planner; When payload built; Then fields.role=="planner", no top-level "field"/"value" keys |
| T2 | JSON value decode: value `["a","b"]` sends []any; value `42` sends float64(42); value `planner` sends "planner" | same | `go test -p 2 ./cmd/meept/ -run TestAmendCmd_ValueJSONDecoded` | RED | GREEN | Given --field=constraints.tools_allowed '["a","b"]'; When sent; Then handler receives []any{"a","b"} |
| T3 | --reason flag: present -> payload carries reason; absent -> key omitted (omitempty semantics at the map level) | same | `go test -p 2 ./cmd/meept/ -run TestAmendCmd` (covers) | RED | GREEN | Given --reason "hardening"; When sent; Then reason=="hardening"; without flag, key absent |
| T4 | meept-lite parity: inspect cmd/meept-lite/handlers.go amend case; fix if it shares the singular shape | Modify (conditional): cmd/meept-lite/handlers.go | `go build ./...` + grep verification | n/a (survey-first) | either already correct (report) or fixed + builds | Given lite client amend; When used; Then same fields shape as main CLI |
| T5 | CLI reference doc updated (flags: --field repeatable? keep single per current UX; --reason) | Modify: docs/reference/<agents-cli-page>.md | grep "--reason" docs/reference/ | doc silent | doc matches behavior | Given new flag; When doc read; Then amend usage documented |

## Self-Verification Checklist

- [ ] Handler-side files untouched (internal/employee/handler.go, handler_test.go green)
- [ ] Payload contract C4 exact; JSON decode attempted before raw-string fallback
- [ ] `go build ./...` clean; gate tests GREEN
- [ ] Output printing does not hard-code status strings (prints returned shape)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Contract C4 exact; reason omitted when empty
- [ ] JSON decode edge cases covered (array/object/number/bool/plain string)
- [ ] No handler changes; parity with HTTP surface payload confirmed
- [ ] No debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- Optional repeatable --field (multiple amendments in one call): the handler supports a fields MAP, but keep scope to the existing one-pair UX plus --reason unless the flag change is trivial — report as a follow-up if skipped.
- Never test against a live daemon; unit-test the payload construction (factor it into a small testable func if the cobra binding makes that cleaner).
