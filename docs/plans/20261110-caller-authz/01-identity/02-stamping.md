# 02 — resolver + Instance.Caller + turn stamping + agentID fix

## DISPATCH INSTRUCTION

Implement in the worktree given in your dispatch context; make gate §3 §4
GREEN. Read gate tests + the current voice_wiring/binding/employee code
before editing.
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: 01-identity/orchestrator.md
- Scope: internal/voice Instance/seam; internal/daemon resolve-once, ctx
  stamp, voice-loop executor agentID.
- Dependencies: 01-callerid-contact.
- Estimated context: 50K

## Interface Contracts (FROZEN)

Per ../master.md §Interface Contracts: Instance.Caller, CallerResolver,
daemonCallerResolver (fail-soft, 2s ctx, resolve at the inbound answer
site voice_wiring.go ~:291-310 BEFORE holdPhoneCall), callerid.With in
bindingTurnRunner.Run alongside employeeIDContextKey, executor agent id =
employee id for VOICE clones only (WithExecutorAgentID precedent,
internal/agent/executor.go:1034; survey sites: components.go:2369 stamp
'default', loop.go:9677 clone inheritance, voice_employee.go:240-276
override pattern).

## Tasks

1. internal/voice: Instance.Caller callerid.Info (zero legal);
   CallerResolver seam in the package's existing style; nil-safe default.
2. internal/daemon/voice_wiring.go: daemonCallerResolver over the contact
   store (reuse store handle wiring); resolve once per inbound answer;
   WARN+fail-soft on any error; NEVER block answering.
3. bindingTurnRunner.Run: add callerid.With stamp (keep employee stamp).
4. agentID fix: where voice loops are built/cloned for the call, set the
   loop/executor agent id to inst.EmployeeID so PreExecChecker routing
   (agent_id in details) resolves the employee constitution. Voice path
   ONLY; global template loop stays 'default'.
5. If the stamp site is leg-shared, resolve+stamp the signal leg too
   (master Q2); else note it.

## What This Leaf Exposes to Siblings

Populated Instance.Caller + ctx-carried callerid on every voice turn;
employee-routed constitution gating on voice turns.

## Self-Verification Checklist

- [ ] Gate §3 §4 GREEN; ./internal/voice full suite + daemon voice tests GREEN.
- [ ] Fail-soft proof: resolver error still answers + runs the call.

## Review Checklist

- [ ] No behavior change for outbound/local legs beyond optional signal stamp.
- [ ] employeeID and callerid stamps coexist on one ctx.
