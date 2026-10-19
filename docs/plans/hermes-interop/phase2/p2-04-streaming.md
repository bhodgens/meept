# Leaf p2-04 — streaming in the tool

Contract P2-D in p2-master.md. OWN: internal/tools/builtin/hermes_agent.go
(+test) — SERIAL after p2-03 (same file; read its landed state first).
Depends on p2-02 SSE API (StreamRunEvents/StreamChat) — read sse.go.

Tasks:
1. `runs` gains wait + collect_events bools: wait blocks on
   StreamRunEvents to terminal (ctx-bounded), returns status + tool
   timeline (tool.started/completed names, run.* terminal). No wait ->
   existing poll path byte-identical.
2. `chat` gains stream bool: StreamChat, aggregate assistant.delta into
   final text + progress array; result shape = sync chat + progress.
   stream=false -> existing path byte-identical.
3. Terminal mapping per contract (completed/failed/cancelled).
4. Tests: stub SSE servers (reuse p2-02's httptest patterns) for both
   verbs incl. cancel + failed terminals; legacy-path regression tests
   (stream/wait absent -> unchanged behavior).
5. Sweep: go test -p 2 ./internal/tools/... ; both binaries build.
   Explicit-path commit:
   'feat(tools): hermes streaming for runs and chat (hermes-interop p2-04)'.

Report: RED/GREEN, arg shapes, terminal-mapping tests, files.
