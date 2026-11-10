# 01 — callerid package + contact lookup + config key

## DISPATCH INSTRUCTION

Implement in the worktree path given in your dispatch context. Make gate
§1, §2, §5 GREEN. Read gate tests first; they are the executable spec.
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: 01-identity/orchestrator.md
- Scope: create internal/callerid; extend internal/contact; extend [voice] config.
- Dependencies: 00T-tests.
- Estimated context: 40K

## Interface Contracts (FROZEN)

Per ../master.md §Interface Contracts — the callerid block, contacts
LookupByPhone/voice_approved/index, and the config key are owned by this
leaf, verbatim.

## Tasks

1. internal/callerid/callerid.go: Info, ctxKey, With, From; package doc
   states the neutral-seam rationale; imports stdlib only.
2. internal/contact/types.go: VoiceApproved on Record.
   internal/contact/store.go: migrate adds guarded ALTER (duplicate-column
   tolerated, copy the callstore.go:118-130 idiom) + phone index; new
   LookupByPhone(ctx, e164) scanning all Record columns; parameterized.
3. internal/config/schema.go [voice]: UnapprovedCallerAllowedTools []string
   toml/json tags per sibling keys; DefaultConfig entry
   ["ledger_append", <REAL message tool name found in gate report — grep
   internal/tools/builtin yourself and confirm the same>]; normalize empty
   -> default at the load boundary where other voice lists normalize.

## What This Leaf Exposes to Siblings

callerid API; contact.LookupByPhone; Record.VoiceApproved;
cfg.Voice.UnapprovedCallerAllowedTools.

## Self-Verification Checklist

- [ ] Gate §1 §2 §5 GREEN; contact + config suites unchanged GREEN.
- [ ] gofmt; 'contact:' error prefix on new errors.

## Review Checklist

- [ ] No migration wipes data (guarded ALTER only).
- [ ] Config normalization does not reorder user lists.
