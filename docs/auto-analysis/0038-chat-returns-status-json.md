# Chat Response Returns Status JSON Instead of Chat Response
**Date**: 2026-05-15
**Phase**: 2
**Severity**: high
**Component**: `internal/rpc/proxy.go`, `internal/bus`
**Evaluation Dimension**: correctness

## Description
Under certain conditions (notably when the daemon has just started), the `chat` RPC method returns a status-like JSON response instead of a chat response. The response `{"status":"running","uptime_seconds":8.6,"version":"0.2.0-go"}` has the format of a status handler response, not a chat response.

## Reproduction
1. Restart the daemon freshly
2. Immediately send a chat message: `~/git/meept/bin/meept chat "say hello"`
3. Observe response: `{"status":"running","uptime_seconds":8.606535417,"version":"0.2.0-go"}`

This was observed once during testing. Subsequent calls returned either empty responses (budget error) or task acknowledgments (async dispatch).

## Evidence
```
$ timeout 60 ~/git/meept/bin/meept chat "say hello"
{"status":"running","uptime_seconds":8.606535417,"version":"0.2.0-go"}
```

The response fields `status`, `uptime_seconds`, and `version` match the status handler at `internal/rpc/server.go` line 319-331, but are a subset of the full status response (missing `default_model`, `registered_methods`, etc.).

## Root Cause
Hypothesis: The bus proxy's response matcher subscribes to `chat.response` with subscriber ID `msgID`. If the ChatHandler hasn't fully initialized yet (no subscriber on `chat.request`), the bus proxy waits. Meanwhile, if another request (like `status`) publishes a response that happens to match the subscriber channel, it could be delivered incorrectly.

Alternatively, this could be a bus message routing issue where messages from one topic are delivered to subscribers of another topic during startup race conditions.

A more likely explanation: the response IS from the ChatHandler, which received the message but the agent loop returned an error that was formatted as a status-like structure. However, the ChatResponse struct has `conversation_id`, `reply`, and `error` fields -- not `status`, `uptime_seconds`, etc.

The most likely explanation is a race in the RPC connection multiplexing where a previous response was still in the socket buffer.

## Impact
- Returns incorrect response type to the user
- Could cause client-side parsing errors (the CLI expects a `reply` field)
- Intermittent, timing-dependent

## Proposed Fix
1. Add request-response ID correlation logging to track which request produced which response
2. Verify that the RPC connection handler properly flushes buffers between requests
3. Add a guard in the chat CLI to detect and report unexpected response formats
4. Investigate whether the bus delivers messages to subscribers across topic boundaries

## Classification
- Race condition or message routing bug
- Intermittent but reproducible on fresh daemon start
- May be related to concurrent RPC handlers sharing bus subscriptions
