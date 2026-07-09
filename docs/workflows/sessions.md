
## Project Binding

Sessions can be bound to a project directory. When bound, all agent operations in that session execute within the project context.

### Automatic Binding

When a session is created, the current working directory (CWD) is automatically sent as part of the `detection_context`:

- **TUI**: Uses `os.Getwd()` on startup; passed to session.create RPC
- **CLI**: `meept /path/to/project` or `meept chat --cwd /path` starts session bound to path
- **Flutter**: Uses platform CWD detection via `PlatformService`

### Session Schema

The `DetectionContext` struct captures client context:

```go
type DetectionContext struct {
    CWD               string   `json:"cwd,omitempty"`
    DetectedProjectID string   `json:"detected_project_id,omitempty"`
    CLIArgs           []string `json:"cli_args,omitempty"`
}
```

### Legacy Sessions

Sessions created before project binding (no `ProjectPath` or `DetectionContext`) are auto-migrated:
- The `GetSession`, `GetMostRecent`, and `List` RPC methods backfill `DetectionContext` from `ProjectPath`
- TUI shows a project prompt modal for sessions with neither `ProjectPath` nor `DetectionContext`
- Flutter shows `ProjectPromptDialog` for unbound sessions

User options in prompt:
- **Yes**: Use current directory (CWD) as project path
- **No**: Run without project context  
- **Pick**: Select from registered projects (future: open project picker)

### Architecture

Per-session project isolation is implemented via:
1. `WorkerPool` - Goroutine pool with 1:5 multiplexing (one worker serves up to 5 AgentLoops)
2. `AgentLoopManager` - Tracks per-session `AgentLoop` instances
3. Explicit `workingDir` in `NewAgentLoop(sessionID, workingDir, opts...)`
4. `DetectionContext` flow from client → session.create RPC → session store

See `docs/superpowers/specs/2026-07-07-session-worker-architecture-design.md` for full design.
