# Chat harness regression checks

Run `make e2e-sweep-selftest` before a live chat harness run.
This command uses Python standard-library tests and checks shell syntax.
No model or daemon starts. A separate sweep test uses a local HTTP fixture.

## Reply checks

`scripts/test_e2e_chat_harness.py` extracts `assert_reply_shape` from the actual
shell script. The test runs the function with temporary reply files.
Empty replies fail A0. JSON-shaped replies fail A3 even with leading spaces,
trailing newlines, or tabs. Ordinary prose remains eligible for the other checks.

## Tool protocol checks

The shell script has two embedded Python clients: warmup and transcript turns.
The tests execute both clients with controlled process streams.
The tool protocol is MCP, which carries tool calls and responses as JSON messages.

Both clients reject initialization errors and `result.isError` tool failures.
A text block inside a failed tool response cannot become a successful answer.
The clients kill and wait for their child process in `finally`, including error exits.
The tests check cleanup after successful calls, failed calls, and initialization errors.
These controlled streams test the harness, not an actual MCP server implementation.

A failed warmup now fails the run. A reachable daemon does not prove a provider outage.
Later transcript paths still have broad provider-error matching and retry behavior.
Those paths need separate negative controls before treating SKIP as a precise diagnosis.

## Verification scope

The offline tests pass and `bash -n scripts/e2e-naive-user-chat.sh` passes.
No live model or daemon run accompanies these changes.
The legacy sync harness does not establish async terminal-event coverage.
See `tools/e2e-sweep/README.md` for sweep completion contracts and remaining limits.
