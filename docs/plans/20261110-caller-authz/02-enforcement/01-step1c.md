# 01 — PreExecChecker step 1c + details injection

## DISPATCH INSTRUCTION

Implement in the worktree given in your dispatch context; gate §1-§6
GREEN. Read internal/employee/enforcement.go around steps 1a/1b (585-616)
and the order comment (540s) first; then the checker constructor path
(manager.RegisterPreExecCheckers, manager.go:980) and how config reaches
it (components.go:6129).
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: 02-enforcement/orchestrator.md
- Scope: enforcement.go step 1c + audit row; agent/executor.go details
  injection; config threading.
- Dependencies: 00T-tests + branch 01.
- Estimated context: 40K

## Interface Contracts (FROZEN)

Per ../master.md §Interface Contracts (this branch's block verbatim:
caller_not_approved, pinned deny text, voice.unapproved_action audit,
no-pause, presence-only details keys).

## Tasks

1. Step 1c after 1b: fetch caller from ctx (preferred, carried through the
   check call — verify Check's actual ctx parameter; fall back to
   details["caller_approved"]=="false") ; if present && !Approved &&
   tool not in allowed list -> deny step with reason
   `caller_not_approved`; the tool-name compare is case-sensitive against
   the registry name like 1a/1b.
2. Allowed list: checker gains the list (constructor param or option;
   keep RegisterPreExecCheckers signature churn minimal); daemon threads
   cfg.Voice.UnapprovedCallerAllowedTools.
3. No-pause: return deny WITHOUT the pause flag the Never step sets
   (read how pause is signaled and skip it).
4. Audit: reuse the checker's existing audit seam; one row per deny;
   nil-safe.
5. executor.go ~:1981: callerid.From(ctx) -> details["caller_approved"]
   ("true"/"false") + ["caller_known"]; absent -> untouched.
6. Update the step-order doc comment (540s block) in the same change.

## What This Leaf Exposes to Siblings

Shipped caller gate; docs leaf pins it in the behavior matrix test.

## Self-Verification Checklist

- [ ] Gate §1-§6 GREEN; full ./internal/employee + ./internal/agent suites GREEN.
- [ ] No-caller path provably inert (§2).

## Review Checklist

- [ ] Deny text byte-identical to contract.
- [ ] Pause semantics untouched.
