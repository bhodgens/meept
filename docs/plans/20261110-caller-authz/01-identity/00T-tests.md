# 00T — Identity Tests (test gate)

## DISPATCH INSTRUCTION

Write TESTS ONLY per §1-§5 below; no production files. RED must be
undefined-symbol failures for the frozen API ONLY, in your files; all
pre-existing suites stay GREEN unchanged.
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: 01-identity/orchestrator.md
- Scope: callerid tests, contact lookup tests, resolver + stamping tests, config tests.
- Dependencies: none.
- Estimated context: 45K

## Interface Contracts (FROZEN)

See ../master.md §Interface Contracts (verbatim copy is embedded there;
key symbols: callerid.Info/With/From, contact.LookupByPhone,
Record.VoiceApproved, voice.Instance.Caller, CallerResolver,
[voice] unapproved_caller_allowed_tools, bindingTurnRunner stamp site,
executor agentID = employee id for voice loops).

## Tasks

1. §1 internal/callerid/callerid_test.go: With/From roundtrip; absence
   (Info,false); nested ctx values don't bleed between goroutines' ctxs.
2. §2 internal/contact lookup tests (extend the package's test style,
   stdlib): LookupByPhone found/not-found; fresh db has voice_approved +
   idx_contacts_phone; open the SAME db twice (migrate re-run) tolerates
   the duplicate ALTER; Record roundtrip keeps VoiceApproved.
3. §3 internal/daemon resolver tests: known+approved; known+unapproved;
   unknown -> fail-soft Number-only; store error -> WARN + call proceeds
   (assert no error returned upward); nil store -> fail-soft.
4. §4 stamping: construct the turn-runner path with an Instance carrying
   Caller; assert the ctx passed onward has callerid.From == Caller
   (capture via a fake TurnRunner/Turns seam like existing voice tests);
   agentID fix: assert the voice-built executor details carry agent_id ==
   employee id (mirror internal/agent executor test posture for details
   capture).
5. §5 config: DefaultConfig value; TOML with omitted table normalizes to
   default; explicit list overrides.
6. Locate the REAL message tool name (grep internal/tools/builtin) and use
   it in §5's expected default — record it in the report.

## What This Leaf Exposes to Siblings

The §1-§5 suite gates leaves 01 and 02.

## Self-Verification Checklist

- [ ] go vet passes structurally where symbols exist; RED = undefined for
      callerid/LookupByPhone/VoiceApproved/Instance.Caller/resolver/config key.
- [ ] Zero production files created; existing tests untouched.

## Review Checklist

- [ ] Control agent diffs every assertion against master contracts.
- [ ] §4 captures ctx at the RIGHT layer (turn ctx, not call ctx).
