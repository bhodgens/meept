//go:build e2e

// Suite signal-sidecar (contact-channels plan leaf 00T, T8; gate matrix
// rows F3, F4, F5). RED until leaf 10/11 land: skip-guarded with the
// named RED reason so the tree build/vet/e2e-fast stays green while the
// suite remains registered.
//
// Coverage:
//
//	signal-sidecar-01  F3  a REAL signal-cli-format stub process (scripted
//	                        json-rpc responses + callEvent stream) through
//	                        the real signal.Client - never an in-process
//	                        mock object (leaf note: the stub process is
//	                        what makes the suite survive real-client
//	                        changes).
//	signal-sidecar-02  F4  the real signal voice-leg adapter bridges the
//	                        stub's socket audio to a FAKE child PCM pair
//	                        paced 640 B / 20 ms (L16 s16le@16k, child chunk
//	                        rule); STT-partial barge-in posture.
//	signal-sidecar-03  F4  hangup release fan-out empties the registry
//	                        slot exactly once (one-live-per-target freed).
//
// Hermetic: loopback/unix sockets only; the stub binary is a scripted
// sh script speaking json-rpc 2.0 over a unix socket, built into the
// suite's temp dir.
package signalsidecar

import (
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// redReason is the named skip reason: this suite activates when leaf 10
// (signal sidecar) and leaf 11 (signal legs) land.
const redReason = "RED: signal sidecar/leg implementation not landed (leaf 10/11)"

// writeSignalCLIStub lays down a signal-cli-shaped stub: a sh script
// that answers json-rpc requests line-by-line on a unix socket and
// emits scripted callEvent notifications. Activated with the suite.
func writeSignalCLIStub(t *testing.T, dir string) string { //nolint:unused // activated post-RED
	t.Helper()
	script := "#!/bin/sh\n" +
		"# signal-cli stub: unix-socket json-rpc 2.0 (nc-based; scripted)\n" +
		"SOCK=\"$1\"; shift\n" +
		"rm -f \"$SOCK\"\n" +
		"while read -r line; do\n" +
		"  # echo the request id, attach a canned result per method\n" +
		"  case \"$line\" in\n" +
		"    *startCall*)  printf '%s\\n' \"{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":1,\\\"result\\\":{\\\"callId\\\":\\\"call-stub-1\\\"}}\" ;;\n" +
		"    *subscribe*)  printf '%s\\n' \"{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":2,\\\"result\\\":{}}\" ;;\n" +
		"    *)            printf '%s\\n' \"{\\\"jsonrpc\\\":\\\"2.0\\\",\\\"id\\\":0,\\\"result\\\":{}}\" ;;\n" +
		"  esac\n" +
		"done\n"
	path := dir + "/signal-cli-stub"
	t.Setenv("SIGNAL_CLI_STUB", path)
	_ = t.TempDir() //nolint:errcheck // body activated post-RED
	_ = script
	return path
}

// TestSignalSidecarCallEventStream drives the real Client against the
// stub process and pins the callEvent demux states. Skip-guarded (T8 RED).
func TestSignalSidecarCallEventStream(t *testing.T) {
	t.Skip(redReason)

	// --- assertions pinned for GREEN (do not weaken when activating) ---
	//
	// binDir := t.TempDir()
	// stub := writeSignalCLIStub(t, binDir)
	//
	// s := harness.Start(t,
	//     harness.WithConfigOverlay(map[string]any{
	//         "signal.enabled":         true, // leaf 10 knob
	//         "signal.cli_path":        stub,
	//         "signal.socket_audio":    true,
	//     }),
	// )
	//
	// // The daemon supervises the stub (supervisor.go pattern); the real
	// // Client dials the stub's unix socket (C3 Dial).
	// // StartCall -> RINGING_OUTGOING -> CONNECTED -> ENDED events surface
	// // through the daemon's leg adapter in order with the stub's callID.
	// // PCM: the FAKE child pair is fed 640-byte / 20 ms L16 chunks by the
	// // real adapter (F4 - resample IN the adapter; child chunks stay L16).
	// // Hangup: the release fan-out empties the registry slot exactly once
	// // (F4) - a second live call to the same signal:<uuid> would then be
	// // allowed, and the calls.db/audit rows carry the signal- ccid.

	_ = harness.Start // keep the harness import honest in the guarded body
}
