# leaf 02-doctor-install/01 — --install-missing executor

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: docs/plans/20260905-dependency-visibility/02-doctor-install/orchestrator.md
- Scope STRICTLY: cmd/meept/doctor.go + doctor_test.go (extend the files
  branch 01 created). No other files.
- Dependencies: branch 01 leaves 01 (InstallHint) and 02
  (mcpDependencyChecks) both COMPLETE and committed.
- Estimated context: ~45K.

## Contract E (verbatim from parent — the spec)

New flag on doctor: `--install-missing` (bool, REQUIRES --fix).
Behavior:
1. Collect missing servers (reuse mcpDependencyChecks data — if the
   branch-01 helper returns doctorCheck values only, factor/refactor a
   variant that returns (name, binary, hint) triples so you don't
   string-parse; a small refactor of branch-01 code is in scope).
2. If none: print `no missing mcp dependencies.` exit 0.
3. Per missing server, in order:
   a. print `install for <name>: <install_hint>`
   b. consent via `confirmInstall(r io.Reader, w io.Writer, hint string) bool`
      — prompt `run this command? [y/N] `; accept exactly y/yes
      case-insensitive; everything else skips.
   c. consent → `runInstallHint(ctx, hint, stdout, stderr io.Writer) error`
      — `exec.CommandContext(ctx, "sh", "-c", hint)`, wired streams,
      10m timeout via context.
   d. result line `installed <name>: ok` / `installed <name>: failed (exit N)`.
4. stdin not a TTY → refuse: `--install-missing requires an interactive
   terminal` (check os.Stdin stat CharDevice; injectable for tests via
   a bool param on the run function).
5. Hints run verbatim from config; meept never constructs commands.

Command failure does not abort the loop; end summary lists per-server
results.

## Tasks

### Task 1: TDD the seams first

confirmInstall table tests (in-memory reader/writer):
- "y", "Y", "yes", "YES" → true
- "", "n", "no", "garbage", EOF → false (EOF must not hang)
- prompt text written to w before read (assert w contents prefix)

runInstallHint tests:
- hint `true` → nil error
- hint `exit 3` → error whose message contains "exit status 3"
- hint writing to stdout/stderr → captured (assert via buffers)
- (do NOT test the 10m timeout by waiting; assert context wiring by
  passing a pre-cancelled ctx → error mentions context)

### Task 2: TDD the flag flow

runDoctor gains `installMissing bool` parameter (wire the cobra flag).
Flow test with injected TTY=true and a stubbed runner seam (make
runInstallHint a package var `runInstallHintFn = runInstallHint` so tests
swap it — the standard seam; report the seam you chose):
- no missing → step-2 message, runner never called
- one missing + consent y → runner called once with the hint; ok line
- one missing + consent n → runner never called
- runner error → failed line, loop continues to second server
- installMissing without fix → flag validation error at cobra level
  (`MarkFlagsRequiredTogether` or explicit check; report which)
- TTY=false → refusal message, no prompts

### Task 3: implement + doc

- Cobra flag with help text: `run install_hint commands for missing mcp
  server dependencies (requires --fix; prompts before each; hints come
  from your mcp_servers.json5 and are executed verbatim)`.
- docs/workflows/tool-routing.md gets a short "installing missing mcp
  dependencies" paragraph referencing the doctor flow (the only
  non-doctor file allowed in this leaf).

### Task 4: verify

```
go build ./cmd/meept
go vet ./cmd/meept
go test -p 2 ./cmd/meept/ -run 'Doctor|Install|Confirm' -count=1 -timeout 180s
TEST_PACKAGE_PARALLELISM=2 go test -p 2 ./cmd/meept/ -count=1 -timeout 300s
```

## Self-Verification Checklist

- [ ] Contract E items 1-5 each covered by a test that would fail without it
- [ ] EOF on consent reads as skip, never hangs
- [ ] TTY refusal before any prompt
- [ ] Existing doctor behavior unchanged when flag absent
- [ ] gofmt/vet clean; lowercase strings; no TODOs

## Review Checklist (for orchestrator)

- [ ] Consent boundary: only config hints execute, verbatim
- [ ] Non-interactive refusal tested (not just implemented)
- [ ] Runner var-seam documented in report
- [ ] tool-routing.md paragraph present

Do NOT commit.
