//go:build e2e

// Package sessionteardown covers the session-teardown e2e contract
// (session-affinity master Contract 6): the scratch llama-server stub
// endpoint receives EXACTLY ONE slot-erase per teardown moment.
//
// The proofs run for real (the leaf-07 gaps are fixed — see
// internal/llm/AGENTS.md "Teardown identity and wiring"):
//
//   - LlamaServerTeardown.Handles accepts the resolver-snapshot key shape
//     (EndpointKey(cfg) = "host|credential") when the runtime manager
//     resolves the host to a live llama runtime, alongside the runtime
//     "llama-cpp:<host>:<port>" prefix;
//   - the erase route strips the provider baseURL's /v1 version prefix
//     (llama-server's slot route lives at the server root);
//   - the coordinator resolves the store's PRIMARY session id to the
//     accounting (conversation) id on a snapshot miss and dispatches with
//     the snapshot id, so reference counting sees the session's own row;
//   - the shutdown sweep erases per session WITHOUT a live-session
//     reference count (every session ends together at daemon stop).
//
// IDLE COVERAGE NOTE (leaf-07 decision, per the leaf's escape hatch): the
// idle path is NOT covered here. The daemon's IdleReaper ticker is
// hard-coded to 1 minute (internal/session/lifecycle.go
// defaultIdleCheckInterval) with no config knob, so a seconds-scale
// idle_release still waits up to a minute per tick — outside the hermetic
// budget. Idle once-per-episode semantics are covered by the
// internal/session lifecycle unit tests.
package sessionteardown
