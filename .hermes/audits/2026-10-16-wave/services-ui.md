# internal/services, internal/tui, cmd/meept -- bughunt 2026-10-16

## Findings

1. **File: `cmd/meept/chat.go:225-226, 295-296`**
   - **Mechanism:** `os.Getwd()` is called in the CLI's `getOrCreateOneshotSession()` and `createFlaggedSession()` to set the session's `detection_context.cwd` when `chatCwd` is empty.
   - **Severity:** LOW (design note, not a bug)
   - **Test/Probe:** AGENTS.md explicitly permits `os.Getwd()` in TUI/CLI contexts because they run in the user's shell. The invariant "never use `os.Getwd()` as a fallback" applies to `internal/` daemon code only.
   - **Evidence:** SOURCE (read code + AGENTS.md invariant)
   - **Not covered:** None -- this is a deliberate design trade-off.

2. **File: `internal/tui/app.go:299, 1129`**
   - **Mechanism:** `os.Getwd()` is called in the TUI's `NewApp()` and in the project-prompt handler to resolve the working directory for sessions.
   - **Severity:** LOW (design note, not a bug)
   - **Test/Probe:** Same as above -- TUI is exempt from the no-`os.Getwd()` rule.
   - **Evidence:** SOURCE
   - **Not covered:** None.

3. **File: `internal/services/push_channels.go:344`**
   - **Mechanism:** `generatePushNotificationID()` uses `id.Generate("")` (crypto/rand-based) instead of `time.Now().UnixNano()`.
   - **Severity:** CLEAN -- no bug found.
   - **Test/Probe:** Verified against the known lead: UnixNano usage. The code uses `pkg/id.Generate` correctly.
   - **Evidence:** SOURCE

4. **File: `internal/services/terminal_service.go:59`**
   - **Mechanism:** Comment explicitly warns "Do NOT use os.Getwd()" and the code uses the passed `workingDir` parameter instead.
   - **Severity:** CLEAN -- no bug found.
   - **Test/Probe:** Verified the invariant is respected.
   - **Evidence:** SOURCE

5. **File: `internal/pty/session.go:324-370` (Close method)**
   - **Mechanism:** `Close()` kills the process (`Process.Kill()`) and `waitLoop()` calls `Wait()`. The design avoids concurrent `Wait()` calls by having `waitLoop` as the single owner of reaping.
   - **Severity:** MEDIUM (process leak risk in timeout scenario)
   - **Test/Probe:** If a timeout fires (`startTimeout`) and calls `Close()`, the process is killed. `waitLoop` then reaps it via `Wait()`. However, if the process has already exited naturally before `Close()` is called, `Wait()` returns immediately. No obvious leak pattern.
   - **Evidence:** SOURCE
   - **Not covered:** Race between timeout-triggered `Close()` and natural process exit in `waitLoop`.

6. **File: `cmd/meept/main.go:201-211` (rootCmd.Execute error handling)**
   - **Mechanism:** Errors from CLI commands are caught and printed to stderr with exit code 1.
   - **Severity:** CLEAN -- proper error handling.
   - **Test/Probe:** Verified that errors are not swallowed.
   - **Evidence:** SOURCE

7. **File: `internal/tui/tableutil/tableutil.go`**
   - **Mechanism:** `Size()` sets both width and height; `SetRows()` normalizes row lengths to column count.
   - **Severity:** CLEAN -- implements the table sizing invariant correctly.
   - **Test/Probe:** The code enforces the TUI table sizing rule (width AND height via `tableutil.Size`).
   - **Evidence:** SOURCE

## Checked and clean

- `internal/services/push_channels.go` -- ID generation uses `id.Generate`, no `UnixNano`.
- `internal/services/terminal_service.go` -- no `os.Getwd()` usage; uses passed `workingDir`.
- `internal/pty/manager.go` -- (checked via grep) no obvious process leak patterns.
- `cmd/meept/chat.go` -- error handling on file I/O and RPC calls is checked (no swallowed errors).
- `internal/tui/models/` -- (checked via grep) no obvious nil model dereferences in handlers.
- `internal/configui/` -- not audited (out of scope for this wave's priority).
- `internal/stt/`, `internal/tts/` -- not audited (out of scope for this wave's priority).

## Residual / not covered

- `internal/calendar/` -- not audited.
- `internal/skills/` -- not audited.
- `internal/selfimprove/` -- not audited.
- Deep dive into TUI handler nil checks -- only superficial grep performed.
- `cmd/meept/` subcommands beyond `chat.go` and `main.go` -- not fully audited.

gates_run: go vet ./internal/services/... (passed); go vet ./internal/tui/... (passed); go vet ./cmd/meept/... (passed)
