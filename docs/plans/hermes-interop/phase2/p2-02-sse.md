# Leaf p2-02 — SSE reader (sse.go)

Contract P2-B in p2-master.md. OWN: internal/hermes/sse.go +
internal/hermes/sse_test.go ONLY. p2-01 is editing client.go
CONCURRENTLY — do not touch client.go, schema.go, or any config file.
You may READ client.go for field names (baseURL/apiKey/http client are
unexported fields on Client; same-package access is fine).

Tasks:
1. Event type + StreamRunEvents + StreamChat per contract. Newline-
   delimited SSE parse: 'event:' name lines, 'data:' payload lines
   (multi-line data joined with \n per SSE spec), blank line dispatches,
   ':'-prefixed lines SKIPPED (upstream keepalive every 10s). Bearer
   header. ctx cancel closes the channel cleanly.
2. Terminal detection: run.completed / run.failed / run.cancelled close
   the channel after delivering the event.
3. Tests: httptest servers emitting (a) keepalive-only then terminal,
   (b) multi-line data frames, (c) assistant.delta aggregation shape,
   (d) mid-stream ctx cancel, (e) non-2xx -> error return before chan,
   (f) 401 -> ErrHermesAuth.
4. Sweep: go test -p 2 ./internal/hermes/ (expect p2-01's in-flight
   edits may make the package briefly unbuildable — if client.go fails
   to compile due to a SIBLING's half-landed state, report it, do not
   fix their file). Explicit-path commit:
   'feat(hermes): SSE event streaming for runs and chat (hermes-interop p2-02)'.

Report: RED/GREEN, test names, parse-rule notes, files.
