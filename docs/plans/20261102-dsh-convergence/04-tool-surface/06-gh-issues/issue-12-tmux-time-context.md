# tmux/time-context: persistent terminal session context + explicit time context in the model surface

## Problem

The 2026-11-02 dsh-vs-meept convergence audit closed this item as a **GitHub
issue, not an implementation leaf** (user decision D2/D9). Two related gaps:

1. **tmux-style session context** — meept has a PTY manager
   (`internal/tools/interface.go` `PTYTool` / `PTYSessionConfig`, driven by
   `internal/pty`) and a rich TUI with a PTY session surface, but there is no
   model-facing persistent-multiwindow terminal session tool. The model cannot
   create a named, resumable terminal session that it (and the user) can call
   back into across turns the way tmux windows persist.
2. **time-context** — the model-facing session context has no explicit
   current-time / elapsed / schedule-awareness block. An agent reasoning about
   "how long has this run?", "what is today/local time?", or a schedule that
   just fired cannot see that without querying an external tool.

## Why it is deferred to a GitHub issue

The items 11/12 of the audit closeout were explicitly scoped OUT of the
implementation tree by user decision. This issue captures the gap and
acceptance target without prescribing an engine — it is a backlog item.

## Expected behavior

- A model-facing tool creates and resumes a named persistent terminal session
  (tmux-like), reusing the existing PTY manager as the foundation rather than
  spawning a new one.
- The session context gained by the model at turn start includes a bounded
  time-context block: current local time, elapsed wall-clock, and any
  recently-fired schedule/occurrence label when relevant.

## Acceptance criteria

- [ ] A `tmux`-analog tool lets the model start, attach to, and read a named
      terminal session that persists across turns.
- [ ] The existing PTY manager (`PTYSessionConfig`) is the reuse point; no
      second terminal backend is introduced.
- [ ] The model-facing context includes an explicit time block (local tz,
      elapsed) whose values are machine-checkable.
- [ ] Honest degradation: if hostile-for-tmux constraints apply (e.g. no PTY in
      a subprocess), the tool returns a detectable error, never a silent
      fallback to a non-persistent session.

## Notes / grounding

- Present-absent anchored against the meept repo at the audit date: `PTYTool`
  and `PTYSessionConfig` exist (interface.go:216-244), the TUI has a PTY
  session surface, but there is NO model-facing multiwindow persistent-session
  tool and NO explicit time-context injection into the model prompt. This
  issue frames a surface gap over an existing engine — the engine is a
  foundation, not a re-build.
