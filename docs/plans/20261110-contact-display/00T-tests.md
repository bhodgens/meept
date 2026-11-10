# 00T — Display Tests (test gate)

## DISPATCH INSTRUCTION

Write TESTS ONLY; no production files; **Do NOT commit** — no
`git commit`, no `git add`. RED = undefined symbols in your files only;
existing suites stay GREEN.

- Parent: orchestrator.md (tree root: docs/plans/20261110-contact-display/)
- Scope: resolver unit tests, payload-shape tests, relay header test, RPC fill test.
- Dependencies: none (assume O5: run in the post-caller-authz tree; if
  contact.LookupByPhone is absent at dispatch time, tests target the
  vendored-local-signature fallback declared in master O5).
- Estimated context: 40K

## Interface Contracts (FROZEN)

Per ../master.md §Interface Contracts (DisplayInfo fields, JSON tags
caller_name/caller_display/target_name/target_display, Egress no-digit
rule, last4 form, raw Number removal, relay header rule).

## Tasks

1. §1 resolver: known -> Name=DisplayName, Display="Jane Doe (•••5567)",
   Egress="Jane Doe"; unknown -> "Unknown caller" forms; error -> same
   unknown path, WARN logged; empty/'' number -> "Unknown caller" with no
   parenthesis artifact.
2. §2 no-digit rules: Egress matches ^[^0-9]*$ for both known and unknown;
   Display contains ONLY the last-4 digit run (assert full digits absent).
3. §3 ended-event shape: publish through the real wiring with a seeded
   contact; decode payload; caller_name/caller_display present, number
   field ABSENT; assert via json key set.
4. §4 RPC status: row JSON has target_name + target_display, no raw
   target digits beyond the last-4 in display.
5. §5 relay: renderCallRelay header line regex-asserted free of \\d{7,};
   transcript body untouched (digits in spoken content allowed).
6. Use the existing fake contact store pattern from the caller-authz
   tree's tests if present; else sqlite fixture per contact package tests.

## What This Leaf Exposes to Siblings

Gates leaves 01-03.

## Self-Verification Checklist

- [ ] Tests only; RED for missing symbols/fields only.

## Review Checklist

- [ ] Control agent verifies §3 key-set assertions (absence, not just mismatch).
