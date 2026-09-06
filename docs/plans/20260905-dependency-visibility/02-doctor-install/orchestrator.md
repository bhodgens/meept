# orchestrator.md — 02-doctor-install branch

## Goal

Deliver Contract E's executor: `meept doctor --fix --install-missing`
runs the leaf-01 install hints after displaying each one, requiring
explicit consent per command. Implements workstream 2 from the parent
master.

## Architecture Overview

Single leaf: 01-install-missing.md. It consumes branch 01's outputs
(ServerConfig.InstallHint + the doctor mcpDependencyChecks helper). No
sub-branching.

## Interface Contracts

### Contract E: --install-missing executor (frozen)

```
New flag on doctor:  --install-missing (bool, REQUIRES --fix)
Behavior:
  1. Collect missing servers (reuse branch 01's mcpDependencyChecks data).
  2. If none: print "no missing mcp dependencies." and exit 0.
  3. For each missing server, IN ORDER:
     a. print the hint: "install for <name>: <install_hint>"
     b. require explicit consent: prompt "run this command? [y/N] " on
        stdin; anything but exactly "y" or "yes" (case-insensitive) skips.
     c. on consent: execute via `sh -c <hint>` (hints are shell one-liners
        by design), streaming output to stdout/stderr; timeout 10m.
     d. record result line: "installed <name>: ok" / "installed <name>: failed (exit N)"
  4. Never proceed past a prompt without stdin input; if stdin is not a
     TTY, refuse with "--install-missing requires an interactive terminal"
     (no hidden automation).
  5. meept NEVER constructs install commands itself; it only runs the
     config's install_hint strings verbatim. This is the consent boundary.
Errors:  command failure does NOT abort the loop; summary at end.
Tests:   prompt+consent path testable via injected reader/writer seam —
         factor consent into `confirmInstall(r io.Reader, w io.Writer, hint string) bool`
         and the runner into
         `runInstallHint(ctx, hint string, stdout, stderr io.Writer) error`
         (both pure-ish; exec.CommandContext inside runInstallHint).
```

## Child Index

| Doc | Scope | Est. context | Dependencies |
|-----|-------|--------------|--------------|
| 01-install-missing.md | flag + confirm + runner + tests | ~45K | branch 01 leaves 01+02 COMPLETE |

## Dispatch Protocol

Per parent master.md. The consent boundary (item 5, no TTY refusal) is a
USER RULE — a review gap on it is a re-dispatch blocker, not a nit.

## Coding Conventions

Per parent master.md. Extra: no new top-level command; this is a doctor
flag. Help text for the flag must state that hints come from the config
file and are run verbatim.

## Completion Tracking Table

| Doc | Status | Notes |
|-----|--------|-------|
| 01-install-missing.md | PENDING | |

## Review Checklist (branch)

- [ ] Contract E items 1-5 all present; TTY refusal tested
- [ ] Non-y answers skip without executing (test proves zero exec on "n")
- [ ] Failure of one hint does not abort remaining servers
- [ ] Doctor's existing --fix repairs unchanged
- [ ] go vet clean; table tests green; lowercase user-facing strings

## Integration Test Plan

Manual smoke (orchestrator): craft a scratch catalog whose install_hint
is `true`; run `meept doctor --fix --install-missing --config <scratch>`;
verify prompt, y-path executes, n-path skips, summary correct. Then mark
branch COMPLETE in parent master.md.
