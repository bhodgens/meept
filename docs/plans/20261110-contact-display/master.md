# Contact-Named Display Surfaces (issue #126) — Master Plan

Date: 2026-11-10. Repo: meept. Tree: docs/plans/20261110-contact-display/
Worktree: .hermes/worktrees/contact-display (branch plan/contact-display)

## Goal

Appropriate disclosure per surface: resolve caller numbers to contact
NAMES at the daemon boundary; operator views show name (+ last-4 or full
number), egress surfaces (bus payloads, push relay) carry names and never
digits; nothing raw leaves the machine. The prompt-side leak is already
closed by the employee-ledger merge; this tree finishes the observer side.

## Architecture Overview

One boundary resolver, then five call-site swaps. Survey anchors
(file:line, 2026-11-10): raw number today at voice_wiring.go:873
(voice.call.ended Number field), rpc/voice_local.go:17,69-77 (target),
daemon/voice_local_rpc.go:73 (fill), tui/models/voice.go:264,275 (print),
flutter voice_page.dart:629 (render), daemon/voice_employee.go:556-565
(renderCallRelay header). contacts.db already has Resolve(id->phone) and
DisplayName (contact/resolve.go:12-24, types.go:28-36); the sibling tree
20261110-caller-authz adds contact.LookupByPhone + callerid.Info — THIS
tree must not edit contact/store.go (merge-order dependency, see Decisions).

## Owner-Ratified Decisions

- O1 Knowing who calls is a FEATURE: operator views keep identifying
  detail; only EGRESS is name-only.
- O2 Resolve at the daemon boundary, never in clients.
- O3 Unknown number -> display "Unknown caller (•••1234)" — last 4 of
  digits is allowed (not a full number); name when known.
- O4 Add resolved fields to the bus/RPC payloads; keep the raw Number
  field ONLY where routing consumers need it — survey found none do
  (consumers: TUI, Flutter, relay) -> DROP the raw field from
  voice.call.ended and replace target with a display struct.
- O5 Merge order: caller-authz tree lands first (provides
  contact.LookupByPhone + callerid.Info + voice_approved); this tree
  consumes them. If run before that merge, vendor a LOCAL resolver in
  internal/voice/redact.go with the same signature and a TODO-merge note.

## Interface Contracts (FROZEN)

New file internal/daemon/caller_display.go (daemon owns resolution; voice
stays store-facing):

```go
// DisplayInfo is the boundary-resolved caller identity.
type DisplayInfo struct {
    Name    string // contact DisplayName or "Unknown caller"
    Display string // operator-safe: "Jane Doe (•••5567)" / "Unknown caller (•••1234)"
    Egress  string // name only: "Jane Doe" / "Unknown caller" — NEVER digits
}
func (d *VoiceWiring/daemon site) resolveDisplay(ctx, e164 string) DisplayInfo
```

Resolution via contact.LookupByPhone (authz tree) or the vendored
same-signature local lookup; 2s ctx; store error -> DisplayInfo zero ->
render Unknown caller (+last4 from the number itself).

payload swaps (frozen shapes):
- voice.call.ended (voice_wiring.go publish site ~:866-897): replace
  Number: inst.Target with CallerName + CallerDisplay fields (JSON tags
  caller_name, caller_display); DELETE raw number (O4 — no consumer
  routes on it; verified during leaf 02).
- rpc voice.local.status rows: target field becomes
  {name, display} — pin exact JSON: "target_name", "target_display";
  raw removed. (RPC layer maps from the daemon fill; keep seam types.)
- renderCallRelay header (voice_employee.go:556-565): uses Egress form;
  the push title contains NO digit runs (allow goal/cost digits already
  present in summaries — regex assertion scoped to the header line only).
- TUI (tui/models/voice.go:264,275) renders display; Flutter
  (voice_page.dart:629) renders row.targetName ?? row.targetDisplay.

last4 helper (shared, internal/voice or daemon-local): digits[-4:] of the
E.164, no other digits leak into Display when unknown.

## Child Index

| Child | Type | Deps |
|---|---|---|
| 00T-tests.md | test-gate | — |
| 01-resolver.md | leaf | 00T |
| 02-surface-swaps.md | leaf | 01 |
| 03-client-render.md | leaf | 02 |

## Dispatch Protocol

1. 00T first (tests only, RED for missing symbols, Do NOT commit);
   validate IN-SESSION; commit gate.
2. 01, then 02, then 03 (serial: shared payload-shape contract).
3. Orchestrator-only commits, per-file pathspec.

## Coding Conventions

Per repo norms; Go stdlib tests; Flutter changes minimal to the two
display sites; dart analyze if available (client is out of tree scope if
toolchain missing -> report, orchestrator verifies manually).

## Test Gate

| Feature | Unit tests | e2e | Status |
|---|---|---|---|
| resolver known/unknown/error/empty | 00T §1 | — | PENDING |
| last4 + no-digit egress rule | 00T §2 | — | PENDING |
| voice.call.ended shape (no raw) | 00T §3 | — | PENDING |
| status RPC display fields | 00T §4 | — | PENDING |
| relay header no digits | 00T §5 | — | PENDING |
| client decode | review | manual | PENDING |

## Completion Tracking Table

| Child | Status | Commit | Notes |
|---|---|---|---|
| 00T | PENDING | | |
| 01 | PENDING | | |
| 02 | PENDING | | |
| 03 | PENDING | | |

## Integration Test Plan

1. go test ./internal/daemon/ -run 'Display|Caller|Relay' -count=1
2. go test ./internal/rpc/ ./internal/voice/ ./internal/tui/... -count=1
3. e2e voice-lifecycle: inbound call -> status RPC + ended event contain
   name/display, no full digit runs (regex on payload strings minus
   last-4 form); relay title check.
4. Manual: TUI /voice during a fake call shows "Jane Doe (•••5567)" for a
   seeded contact, "Unknown caller (•••0111)" otherwise.
5. go build ./... + merge plan/contact-display to main AFTER
   plan/caller-authz is merged (O5) — orchestrator checks merge order.

## Review Checklist

- [ ] No surface renders a full number except operator display's last-4 form.
- [ ] Raw Number removal proven consumerless (grep hits documented in 02 report).
- [ ] Client changes decode-only; no client-side resolution logic.
- [ ] O5 merge-order respected at final commit.

## Open Questions

- Q1: config toggle for full-number operator mode (show_last4 vs full) —
      deferred; current contract is name+last4 only.
- Q2: Signal uuid display mapping (contacts have uuid) — out of scope,
      same pattern later.
