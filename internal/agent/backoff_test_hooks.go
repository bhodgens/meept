package agent

// Exported test-support shims over the package-private per-operation backoff
// override store. The public SetPerOperationBackoffOverride has no exported
// clear, so cross-package tests (internal/daemon's epistemic wiring test)
// that need hermetic pacing — install an override, assert, then restore —
// would otherwise leak global state into the rest of the suite. Keep this
// file limited to test-support surface.
//
// Production code must not call these.
//
// This file MUST be committed: internal/daemon/epistemic_wiring_test.go
// (commit fda25177) references both symbols, and an untracked copy made
// the daemon test package unbuildable on any tree without it.

// SetPerOperationBackoffOverrideForTest installs a per-operation backoff
// override under the given key (e.g. "http", "llm").
func SetPerOperationBackoffOverrideForTest(key string, cfg BackoffConfig) {
	SetPerOperationBackoffOverride(key, cfg)
}

// ClearPerOperationBackoffOverrideForTest removes the per-operation backoff
// override under the given key.
func ClearPerOperationBackoffOverrideForTest(key string) {
	perOperationOverrides.Delete(key)
}
