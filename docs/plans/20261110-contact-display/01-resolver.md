# 01 — boundary resolver

## DISPATCH INSTRUCTION

Implement in the worktree path given in your dispatch context; make gate
§1 §2 GREEN. Read the gate tests and internal/contact (store/resolve/types)
+ the caller-authz additions (LookupByPhone) first.
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: docs/plans/20261110-contact-display/master.md
- Scope: internal/daemon/caller_display.go (new).
- Dependencies: 00T-tests; contact.LookupByPhone per O5 (authz tree merged,
  or vendored-local same-signature fallback with TODO-merge note).
- Estimated context: 35K

## Interface Contracts (FROZEN)

Per master §Interface Contracts: DisplayInfo{Name, Display, Egress},
resolveDisplay via contact lookup, 2s ctx, fail-soft unknown path,
last4 rule, no-digit egress rule.

## Tasks

1. caller_display.go exactly per contract; doc comment states the O1
   disclosure policy (operator views name+last4; egress names only).
2. last4 helper: non-digit-tolerant (strip non-digits, take last 4,
   fewer than 4 digits -> whole tail is fine, '' -> no parens).
3. If O5 fallback used: local resolveContact over contact.SQLiteStore's
   existing read API; comment names the merge dependency.

## What This Leaf Exposes to Siblings

resolveDisplay consumed by leaves 02 (server fills) and 03 (none - client).

## Self-Verification Checklist

- [ ] Gate §1 §2 GREEN; gofmt; go build ./internal/daemon/.

## Review Checklist

- [ ] Fail-soft on store error is WARN-only, returns unknown DisplayInfo.
