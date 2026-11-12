package voice

// Leaf-06 recording wiring on the manager (plan
// 20261110-uptractor-ports, leaf 06 T2/T3): the answer path starts a
// dual-channel mp3 recording when the consent-gated policy is armed, and
// the recording.saved webhook stores its URL on the call row. Everything
// here is ADDITIVE — the releaseOnce funnel, the answer hook, and the
// observer lanes are untouched; an unwired Manager (nil policy/Armed
// recorder) never issues a carrier record_start (default-off is
// structural, pinned by TestRecordingDefaultOffNoStartOnAnswer).

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// RecordingStarter is the carrier record_start seam. *telnyx.Client
// satisfies it; the narrow interface keeps the manager testable without
// the concrete client.
type RecordingStarter interface {
	StartRecording(ctx context.Context, callControlID string, opts telnyx.RecordOptions) error
}

// ArmRecording wires the consent-gated recording seam onto the manager.
// policy gates every start (StartOnAnswer consults Enabled AND
// ConsentAnnounced); starter is the carrier client; sink receives the
// recording.saved URL (the wiring adapts CallStore.SetRecordingURL).
// A nil starter or sink leaves recording armed-but-inert: no carrier
// call is ever made and URL events no-op. Call once at wiring time,
// before the first webhook can arrive; the seam is mutex-guarded like
// every other manager lane.
func (m *Manager) ArmRecording(policy RecordingPolicy, starter RecordingStarter, sink func(ctx context.Context, ccid, url string) error) {
	m.recordingMu.Lock()
	m.policy = policy
	m.recordingStarter = starter
	m.urlSink = sink
	m.recordingMu.Unlock()
}

// startRecordingIfArmed starts the carrier recording for ccid when the
// armed policy says so. Best-effort: errors are logged, never returned —
// the answered webhook must not 500 over a failed record_start on an
// otherwise-live call. The frozen default options ride inside the client
// (mp3, dual-channel).
func (m *Manager) startRecordingIfArmed(ctx context.Context, ccid string) {
	m.recordingMu.Lock()
	policy, starter := m.policy, m.recordingStarter
	m.recordingMu.Unlock()
	if !policy.StartOnAnswer(ccid) || starter == nil {
		return
	}
	if err := starter.StartRecording(ctx, ccid, telnyx.RecordOptions{}); err != nil {
		slog.Error("voice: carrier record_start failed",
			"call_control_id", ccid, "error", err)
	}
}

// HandleRecordingSaved stores ev's recording URL through the wired sink
// (CallStore.SetRecordingURL). Unknown call ids are the sink's no-op
// (idempotent webhook posture); an unwired sink or an event without a
// URL is a clean no-op so the webhook ACKs 200 and Telnyx never
// redelivers.
func (m *Manager) HandleRecordingSaved(ctx context.Context, ev telnyx.CallEvent) error {
	m.recordingMu.Lock()
	sink := m.urlSink
	m.recordingMu.Unlock()
	if sink == nil || ev.RecordingURL == "" {
		return nil
	}
	return sink(ctx, ev.CallControlID, ev.RecordingURL)
}
