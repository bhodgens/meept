# Seatbelt confinement (macOS local backend)

Meept's local execution backend can confine the shell tool's children on
macOS using Apple's seatbelt mechanism (`/usr/bin/sandbox-exec`). This is
macOS's own per-process sandbox: one profile string passed at launch denies
file reads, confines file writes, blocks process inspection, and blocks
exec of chosen binaries. It shares the host kernel (like bwrap on Linux),
so it is a strong guard against ordinary mistakes and tool misuse, not a
hardware boundary.

Related: `docs/concepts/runtime.md` (backend selection), `docs/workflows/sms.md`
unrelated. Linux gets bwrap / network namespaces; the resolver prefers
docker > bwrap > local on every host.

## Configuration (opt-in, default OFF)

`[runtime]` master switch plus `[runtime.seatbelt]` in `~/.meept/meept.json5`.
Both keys are required: the daemon builds the runtime manager only when
`runtime.enabled` is true, so enabling seatbelt alone leaves shell children
unconfined with no warning.

```json5
runtime: {
  enabled: true,                          // master switch: REQUIRED for seatbelt
  seatbelt: {
    enabled: true,
    deny_read: [ "~/.ssh", "~/.aws" ],      // subtrees the child cannot read
    allow_write: [ "~/.meept/workspaces" ], // writes confined to these roots
    deny_write: [ "~/.meept/workspaces/protected" ], // holes inside a root
    block_process_info: true,               // deny ps / process listing
    deny_exec: [ "/usr/bin/sudo" ],         // binaries the child cannot exec
  },
}
```

- `enabled: false` (default) keeps the legacy posture byte-identical: only
  the network class (`deny network*`) is applied, exactly as before.
- All paths must be absolute (`~` is NOT expanded here; use `$HOME` via env
  expansion, which meept applies to config content before parse).
- A bad policy fails at startup and the shell tool lands on the REFUSING
  backend — no command runs unconfined. On Linux/Windows `enabled: true`
  is a startup error for the same reason.
- `deny_read` scopes reads to everything EXCEPT the listed subtrees. A
  global read-deny plus allow-list is not offered: it aborts the child at
  dyld time on macOS 26 (verified).
- `allow_write` implies a full write-deny outside the list. Device nodes a
  shell needs (`/dev/null` et al.) stay writable.
- `deny_exec` must not cover `/bin/sh` or the launch itself; meept refuses
  such a config with a clear error.
- `deny_exec` is enforced BELOW the shell tool's own risk gate. The gate
  (`internal/tools/builtin/shell.go` `blockedCommands`) refuses `kill`,
  `killall`, `pkill` and `sudo` before any confinement is consulted, so a
  `deny_exec` entry naming one of those binaries can never be observed
  through the shell tool. Tests and docs that want to prove an exec deny
  must pick a target the gate lets through, and that binary must exist at
  that exact path (`/usr/bin/true`, not `/bin/true`, on macOS).

## How it applies

The local backend wraps every child:

```
/usr/bin/sandbox-exec -p <profile> /bin/sh -c <command>
```

Profile rule order matters: seatbelt rules are last-match-wins, so meept
emits global denies first, then allow-lists, then hole denies, then process
rules. The builder and its ordering are pinned by OS-independent unit tests in
`internal/runtime/seatbelt_test.go` — `TestBuildSeatbeltProfile_RuleOrderAndShape`
asserts the exact rule sequence by byte offset (global write-deny before the
allow-list, every `deny_write` hole after it),
`TestBuildSeatbeltProfile_BadRuleFailsClosed` pins that each malformed rule
class returns a wrapped error and an empty profile, and
`TestBuildSeatbeltProfile_InactiveIsLegacy` / `TestValidateSeatbeltPath` /
`TestSeatbeltActive` pin fail-closed behavior for inactive or invalid config.
These unit tests are the ONLY tier CI executes: the CI runner is linux
(`ci.yml` job `e2e-fast`, no macOS runner), where the darwin-only e2e tests
skip, so the emitted rule shape must stay covered here.
The same policy is exercised end-to-end by `e2e/suites/seatbelt/` (marker
files prove which writes landed, which reads failed with `Operation not
permitted`, and that an invalid policy refuses commands entirely).

## What it does NOT do

- No memory or CPU caps (seatbelt has no resource controls; bwrap/nsjail on
  Linux do).
- No network allow-lists; restrictive classes stay fail-closed to no egress
  (issue 71 posture unchanged).
- `sandbox-exec` is undocumented Apple API. It has been stable for years
  and lives at a pinned path; if it ever disappears, launches are refused
  rather than run unconfined.

## Verifying confinement

```
meept daemon  # log line at INFO: "seatbelt confinement armed for local backend"
```

Then from an agent shell: `ps aux` should fail with "Operation not
permitted" when `block_process_info` is true, and any write outside
`allow_write` roots should fail the same way.
