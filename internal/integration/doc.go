// Package integration holds cross-leaf integration tests for the
// containment workstream (plans/containment-and-computer-use). It contains
// no production code; it consumes ONLY the public APIs of internal/runtime,
// internal/secrets, internal/tools/builtin, internal/comm/http, and
// internal/tui/modals to prove the seams between leaves hold end-to-end:
//
//   - env isolation under real LocalBackend execution,
//   - secret placeholder round-trip through the egress proxy,
//   - fail-closed sandbox refusal (ErrSandboxRequired),
//   - the stage -> accept -> journal -> revert chain,
//   - surface parity (agent, HTTP, and TUI-facing views see the same state).
//
// These tests guard the seams individual leaf packages cannot see. They run
// as ordinary `go test ./...` tests (no build tag), use t.TempDir for all
// state, and contain no sleeps — waits are bounded readiness polls or
// synchronous request/response pairs.
package integration
