# leaf 01-dependency-visibility/03 — DaemonPath + launchd + kardianos + manager warning

## DISPATCH INSTRUCTION

You are an implementation agent. Implement every task with TDD. **Do NOT
commit. Do NOT run `git add`.** Write code, run tests, report results.

- Parent: docs/plans/20260905-dependency-visibility/01-dependency-visibility/orchestrator.md
- Scope STRICTLY: internal/daemon/daemonpath.go (new) + daemonpath_test.go
  (new), internal/daemon/launchd.go (two edits), internal/tools/mcp/manager.go
  (one log line), internal/tools/mcp/manager_test.go (one test). No other
  files. NOTE: manager.go carries ServerConfig.InstallHint from leaf 01 —
  do not touch that field; if leaf 01 has not landed when you start,
  coordinate via the orchestrator (do not implement it yourself).
- Dependencies: none (parallel with 01 and 04). With leaf 04, closes
  issue #32.
- Estimated context: ~50K.

## Contract C (verbatim from parent master)

```go
// internal/daemon/daemonpath.go (package daemon):
// DaemonPath returns the PATH value the daemon guarantees for itself and
// its subprocesses (launchd plists, MCP server launches). Order: the
// inherited PATH first (preserves shell-launched setups), then the
// guaranteed dirs appended if absent: /opt/homebrew/bin, /usr/local/bin,
// $HOME/.local/bin, $HOME/go/bin, $HOME/.cargo/bin, /usr/bin:/bin:/usr/sbin:/sbin.
func DaemonPath() string
```

Consumers:
1. launchd.go:329 plist EnvironmentVariables PATH → `DaemonPath()`.
2. kardianos/service install: pass env PATH via svc.Config (verify the
   exact field — kardianos/service.Config has an `EnvVariables` or
   `Env` map depending on version; go doc github.com/kardianos/service
   FIRST, trust go doc over this brief, and report what you found).
3. MCP launch failure log (manager.go StartServer error path): one line
   — `slog.Warn("mcp server launch failed", "server", cfg.Name, "error", err, "daemon_path", <the PATH the server will see>)`.

## Tasks

### Task 1: TDD daemonpath_test.go first

Table-driven cases for `DaemonPath()`:
1. inherited PATH preserved: set PATH env (t.Setenv) to "/custom/dir" →
   result starts with "/custom/dir:".
2. dedup: inherited PATH already contains /opt/homebrew/bin → not
   duplicated.
3. guarantees appended when absent: all six guaranteed dirs present in
   order after the inherited segment.
4. empty inherited PATH → exactly the six guaranteed dirs joined.
5. `$HOME` expansion: set HOME via t.Setenv → contains <HOME>/.local/bin.
Also a dedup-order stability case: run twice, same string (pure function
of env — assert determinism).

### Task 2: implement daemonpath.go

- Read PATH once via os.Getenv("PATH"); split ":"; append missing
  guaranteed dirs; join ":". No I/O, no LookPath (pure string assembly),
  doc comment citing Contract C and issue #32.

### Task 3: wire launchd.go

- :329 EnvironmentVariables PATH string → call DaemonPath(). Keep the
  Sprintf template; add nothing else.

### Task 4: wire kardianos/service

- go doc github.com/kardianos/service — find how the Config carries env
  (Config.EnvVariables map[string]string in current versions). Set PATH
  from DaemonPath() in the install path (launchd.go where svc.New/svc.Install
  is called — find the Config construction). If the field does not exist
  in the vendored version, implement by writing our own plist for that
  path too and SAY SO in the report.

### Task 5: manager.go failure warning

- In StartServer's error return path (manager.go ~:287-405, the stdio
  branch), wrap the launch failure with one slog.Warn including
  "daemon_path" = the PATH a subprocess would see. For the subprocess
  PATH use the transport's env handling truth: stdio.go:104 sets
  cmd.Env = os.Environ() — so the subprocess PATH IS the daemon process
  PATH; log os.Getenv("PATH") verbatim. (DaemonPath() is the plist-time
  guarantee; at runtime log what is real. Note this distinction in a
  comment.)
- manager_test.go: assert the warning fires on a command that cannot
  exist ("/nonexistent-binary-xyz") — capture via a slog handler test
  or refactor the log call behind a small var hook; choose the minimal
  testable seam and report which.

### Task 6: verify

```
go build ./internal/... ./cmd/meept-daemon
go vet ./internal/daemon/ ./internal/tools/mcp/
go test -p 2 ./internal/daemon/ -run 'DaemonPath|Launchd|Service' -count=1 -timeout 180s
go test -p 2 ./internal/tools/mcp/ -run 'Manager|Start' -count=1 -timeout 180s
gofmt -l internal/daemon/ internal/tools/mcp/
```

## Self-Verification Checklist

- [ ] DaemonPath signature/doc match Contract C; all 5+1 table cases pass
- [ ] launchd.go:329 uses DaemonPath(); template otherwise untouched
- [ ] kardianos env field verified via go doc; name reported
- [ ] manager warning one line with effective PATH; test proves firing
- [ ] gofmt/vet clean; no TODOs; no os.Getwd introduced

## Review Checklist (for orchestrator)

- [ ] All three #32 failure sites addressed in Go (plist, kardianos, log)
- [ ] Tests would fail if DaemonPath dropped a guaranteed dir
- [ ] No changes outside the five files in scope

Do NOT commit.
