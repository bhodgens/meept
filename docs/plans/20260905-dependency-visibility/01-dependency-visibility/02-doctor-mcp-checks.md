# leaf 01-dependency-visibility/02 — doctor mcp-dependencies checks

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: docs/plans/20260905-dependency-visibility/01-dependency-visibility/orchestrator.md
- Scope STRICTLY: cmd/meept/doctor.go + doctor_test.go. If doctor needs
  the catalog path, load it via the existing config loading in cmd/meept
  (mirror how doctor.go already reaches stateDir/config; read the file
  first). No other files.
- Dependencies: leaf 01 (ServerConfig.InstallHint) must be COMPLETE.
- Estimated context: ~40K.

## Contract B (verbatim from parent master)

One doctorCheck line per ENABLED stdio catalog entry:

- present:  `[ok]   mcp:<name>         <binary> found in path`
- missing:  `[fail] mcp:<name>         <binary> not found — install: <install_hint>`

Name prefix `mcp:` distinguishes from core checks. Missing binaries do
NOT abort doctor; they count in the failed summary. Runtime lookup is
`exec.LookPath(cmd[0])` only for bare names; absolute-path commands are
stat()'d instead. (Final rendered strings are lowercase per meept UI
convention — the print layer already lowercases; keep detail strings
lowercase at construction.)

## Tasks

### Task 1: read doctor.go fully first

Understand doctorCheck, runDoctor flow, where client-side checks end.
The new check block goes after the existing client-side checks and only
runs when the MCP catalog loads; if catalog loading fails, emit ONE
`[warn] mcp:catalog could not load — <err>` line instead of failing.

### Task 2: TDD (doctor_test.go first)

Table-driven. Factor the per-server check into a pure helper so tests
don't need the real catalog:

```go
// mcpDependencyCheck builds the doctorCheck for one enabled stdio server.
func mcpDependencyCheck(name, command0, installHint string) doctorCheck
```

Cases:
1. bare name, binary present (use a binary guaranteed present: "go" —
   LookPath finds it in test env) → ok form, detail contains "found".
2. bare name, absent (use a name like "definitely-not-a-binary-xyz") →
   fail form, detail contains the install hint.
3. absolute path exists (t.TempDir()+executable file, chmod 0755) → ok.
4. absolute path missing → fail.
5. empty installHint + missing → fail detail without "install:" suffix.
6. disabled server (Enabled=false) → NO check emitted (filter at the
   caller loop; assert count).
7. http-type server → NO check.

Also: end-to-end — runDoctor against a temp HOME with a crafted minimal
catalog containing one present + one missing server; assert output
contains both `mcp:` lines (capture via printDoctorChecks refactor or
by testing the checks slice builder — prefer extracting
`mcpDependencyChecks(cfg) []doctorCheck` and testing THAT).

### Task 3: implement

- New check function appended in runDoctor after existing client checks.
- Keep doctor's existing output format; no new flags (Contract B:
  --json out of scope).
- Errors from LookPath are data (detail), never returned as aborts.

### Task 4: verify

```
go build ./cmd/meept
go vet ./cmd/meept
go test -p 2 ./cmd/meept/ -run 'Doctor|Mcp' -count=1 -timeout 120s
TEST_PACKAGE_PARALLELISM=2 go test -p 2 ./cmd/meept/ -count=1 -timeout 300s
```

## Self-Verification Checklist

- [ ] Contract B shapes exact; mcp: prefix on every new line
- [ ] Disabled + http entries produce zero checks
- [ ] Doctor aborts on nothing new; catalog-load failure = warn line
- [ ] Existing doctor tests still green; lowercase details
- [ ] gofmt/vet clean; no TODOs; no line-number prefixes

## Review Checklist (for orchestrator)

- [ ] Helper is pure and table-tested (7 cases from Task 2)
- [ ] No new flags; no JSON output
- [ ] Install hint rendering matches Contract B punctuation

Do NOT commit.
