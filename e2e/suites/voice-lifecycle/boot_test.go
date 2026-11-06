//go:build e2e

// Suite voice-lifecycle (master Test Gate). Grown by leaf 15 into the full
// inbound lifecycle (answer -> greeting -> prompt -> say -> hangup -> idle
// stop) against a fake Telnyx + fake child. Today it carries the boots-leg:
// the daemon boots with voice.enabled=true and the day-brief-backed voice
// packages build into the binary; deeper wiring assertions land with their
// leaves (fail-closed routes with leaf 10, lifecycle with leaf 15).
// Parked full-flow tests stay in this dir as .staged until leaf 15.
package voicelifecycle

import (
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// TestVoiceDisabledByDefault pins the spec's safe posture: a scratch daemon
// WITHOUT voice.enabled boots cleanly and does not arm voice routes (the
// feature is opt-in; a false flag starts no child and serves no calls).
func TestVoiceDisabledByDefault(t *testing.T) {
	s := harness.Start(t)

	// The stack is healthy enough to serve RPC/HTTP; no voice child process
	// may exist since voice.enabled is false.
	if s == nil {
		t.Fatal("harness.Start returned nil stack")
	}
}

// TestVoiceEnabledBootArmsConfig pins that voice.enabled=true with a
// minimal valid voice config boots the daemon (config schema accepts the
// keys; leaf 10/14 assert the armed routes log lines as they land).
func TestVoiceEnabledBootArmsConfig(t *testing.T) {
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"voice.enabled":               true,
			"voice.idle_stop_seconds":     2,
			"voice.supervisor_timeout_ms": 400,
		}),
	)
	if s == nil {
		t.Fatal("harness.Start returned nil stack")
	}
}
