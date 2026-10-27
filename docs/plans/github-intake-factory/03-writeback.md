# Outcome Write-Back — Implementation Leaf (03)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed — make them pass; do NOT
> weaken them. Do NOT commit. Explore with search_files or terminal cat,
> not read_file. After writing a file, do NOT read it back.

## Meta

- **Parent:** master.md
- **Scope:** `internal/intake/writeback.go`: effects-wrapped outcome
  comments + done-label on the ticket.
- **Dependencies:** 01-poller.md (Ticket), 02-linkage.md (Linker
  conventions — same effects patterns)
- **Estimated Context:** 55K
- **Concurrency Group:** B

## Goal

Close the loop outward: goal outcomes surface on the ticket as comments,
with a done label on healthy. Comments are effects-wrapped so a poll
cycle can never storm a ticket with duplicates.

## Context

The #64 tree's `github_issue_comment` pattern is the template:
`internal/tools/builtin/github_issue.go:151-205` (claim ->
CreateComment -> receipt -> Complete; `github.IssueCommentRequest{Body:
body}`; add-label is `client.Issues.AddLabelsToIssue`). Effect key
composition precedent: github_pr.go C4.

Key files:
- internal/tools/builtin/github_issue.go — comment + receipt pattern
- internal/tools/builtin/github_pr.go — effects key + isClientAPIError
- internal/githubsvc/pool.go — ClientPool.For

## Interface Contracts (From Parent)

### What This Leaf Exposes

Contract 3 verbatim: `Outcome`, `WriteBack`, `NewWriteBack`, `Report`.
Concrete details frozen here:

```go
// internal/intake/writeback.go

// Comment body template (plain text; Detail is truncated to 280 runes):
//   [meept factory] goal <GoalID>: <State>
//   <Detail>
const commentTemplate = "[meept factory] goal %s: %s\n%s"
```

### What This Leaf Consumes

```
githubsvc.ClientPool.For                    (from #64)
effects.Run / EffectKey / EffectMeta        (existing)
```

## Tasks

### Task 1: Report comment path (F5)

**Objective:** effects-wrapped outcome comment.

**Files:**
- Create: `internal/intake/writeback.go`
- Test: gate `TestWriteBack_Report_Comment`

Key: `effects.EffectKey("intake_writeback", t.Repo, t.ID,
o.State+"-"+o.GoalID)` — one comment per (goal, state) transition; a
repeated poll with the same outcome collides on the completed key and
returns the prior receipt (id/url from the stored receipt JSON
`{"id":..., "html_url":...}`). Meta: Tool "intake_writeback",
ProviderIdempotent: false, Payload: marshaled Outcome. 4xx provider
rejections follow the merge/review recorded-outcome pattern
(github_pr.go isClientAPIError): receipt + Complete + surface the
error; 5xx/transport stays claimed for reconcile.

Run: `go test ./internal/intake/ -run TestWriteBack_Report -v` -> PASS.

### Task 2: done label on healthy (F5)

**Objective:** label-based completion, never close.

**Files:**
- Modify: `internal/intake/writeback.go`
- Test: gate `TestWriteBack_HealthyAddsLabel`, `TestWriteBack_NoStorm`

`o.State == "healthy"` -> after the comment lands, call
`client.Issues.AddLabelsToIssue(ctx, owner, repo, number, []string{doneLabel})`.
The label call is idempotent upstream (GitHub dedups labels) and rides
the same effects record (it is part of the same logical effect — a
crash between comment and label leaves the comment, and the next poll
re-adds the label harmlessly; document this in the type doc). doneLabel
comes from a setter: `func (w *WriteBack) SetDoneLabel(label string)`
(default "factory:done") — config wiring (leaf 04/05) calls it.
`TestWriteBack_NoStorm`: second Report, same (goalID, state) -> fake
comment counter unchanged.

## Self-Verification Checklist

- [ ] Gate tests F5 PASS with `-race`
- [ ] Key derivation byte-identical to Contract 3
- [ ] Detail truncated to 280 runes (multi-byte safe: cut on runes)
- [ ] Read-only: neither Report path mutates local state; all external
      writes are effects-claimed
- [ ] gofmt/vet clean; no unused code

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Gate tests green; no gate test modified
- [ ] Comment storm impossible (key includes goalID+state; receipt reuse)
- [ ] No ticket close call anywhere (grep `Edit\b|state.*closed` in the file)
- [ ] 4xx recorded-outcome pattern present; 5xx stays claimed
- [ ] No body/secret material in logs

## Notes

- If `AddLabelsToIssue` has a different v93 name, adapt to the real
  surface and record the deviation (verify with `go doc
  github.com/google/go-github/v93/github.IssuesService` before writing).
