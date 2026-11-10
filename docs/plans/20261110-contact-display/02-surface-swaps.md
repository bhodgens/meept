# 02 — server surface swaps (bus payload, RPC fill, relay)

## DISPATCH INSTRUCTION

Implement in the worktree given in your dispatch context; make gate §3 §4
§5 GREEN. Read each site first: voice_wiring.go ended-publish (~:866-897),
internal/rpc/voice_local.go + daemon voice_local_rpc.go fill,
voice_employee.go renderCallRelay (:556-565).
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: docs/plans/20261109-contact-display/master.md (sic: 20261110)
- Scope: the three daemon/rpc sites; payload struct fields.
- Dependencies: 01-resolver.
- Estimated context: 45K

## Interface Contracts (FROZEN)

Per master §Interface Contracts: caller_name/caller_display on
voice.call.ended with raw Number REMOVED (O4 — first grep-verify no
consumer reads Number for routing; list the grep hits in your report);
target_name/target_display on voice.local.status rows; relay header
Egress form, header regex \\d{7,} free, transcript untouched.

## Tasks

1. ended publish: resolveDisplay(inst.Target) -> new fields; drop Number;
   keep CallID/EmployeeID/DurationMs unchanged.
2. status fill: same swap at the fill site; RPC wire struct gains the two
   fields, raw target kept ONLY if an existing consumer needs routing —
   verify by grep (TUI/Flutter call sites) and state the verdict in the
   report (expected: raw REMOVED per O4; if a consumer needs it, keep it
   and flag for orchestrator decision BEFORE committing the shape).
3. relay header: Egress + display per the pinned test.
4. Update the two structs' doc comments with the disclosure rule.

## What This Leaf Exposes to Siblings

Final payload shapes leaf 03 decodes.

## Self-Verification Checklist

- [ ] Gate §3 §4 §5 GREEN; ./internal/daemon ./internal/rpc suites GREEN.

## Review Checklist

- [ ] O4 raw-removal verdict recorded with grep evidence in the report.
