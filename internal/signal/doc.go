// Package signal is the signal-cli json-rpc client + subprocess
// supervisor (contact-channels plan, contract C3, owner leaf 10).
//
// Layout:
//   - types.go: wire/domain types (CallEvent, DataMessage,
//     CallAudioMode, CallInfo) + call-state constants.
//   - rpc.go: Client — JSON-RPC 2.0 over the signal-cli unix socket,
//     request-id mutex, demux goroutine (sole conn reader), response
//     correlation by id, callEvent/envelope notification fan-outs,
//     (sourceUuid, timestamp) envelope dedupe.
//   - supervisor.go: Supervisor — wraps llm.RuntimeProcess (repo rule:
//     never hand-roll child lifecycle), bounded socket wait,
//     crash-restart with capped backoff, optional idle stop.
//
// Leg isolation: NO telnyx or voice package imports are allowed here
// (mirrors internal/voice/local.go's pin posture). Credentials: the
// daemon never handles Signal account credentials — signal-cli holds
// its own linked-device state; the supervisor only spawns the
// configured binary with the configured account handle.
package signal
