//go:build e2e

package voicelifecycle

// F1 gate: a keys line with stt_vendor=rumble constructs the real native
// streaming adapter and a Final transcript reaches the talk loop. This is a
// keys-frame decision gate: the vendor is keyless, so the fake server also
// asserts no Authorization header is sent on the WS upgrade.
//
// The old fake spoke the stale batch chat.completions audio_url shape; it is
// rewritten here to the native WS protocol (binary PCM in, partial/final/end
// out) that the live server exposes at /v1/audio/transcriptions/stream.

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild/providers"
)

// TestVoiceKeysRumble01 drives the keys-frame seam: a daemon-style keys line
// with stt_vendor=rumble must select the native Rumble adapter through the
// REAL providers.NewSTT factory, and a server-emitted final must reach the
// caller. RED today: the batch adapter never upgrades the WS.
func TestVoiceKeysRumble01(t *testing.T) {
	srv, f := newRumbleWSFake(t, rumbleFakeCfg{
		emitAfter: 2,
		events: []string{
			`{"type":"partial","text":"hello"}`,
			`{"type":"final","text":"hello there"}`,
			`{"type":"end","text":"hello there"}`,
		},
	})
	defer srv.Close()

	stt, err := providers.NewSTT(providers.ProviderConfig{
		Vendor:     "rumble",
		Model:      "Reson8/Rumble-2-Flash",
		BaseURL:    srv.URL,
		SampleRate: 16000,
	}, "" /* keyless */)
	if err != nil {
		t.Fatalf("NewSTT(rumble) from keys-line fields: %v", err)
	}

	pcm := make(chan []byte)
	feedRumblePCM(pcm, 40, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := collectRumble(t, ctx, out)

	var sawFinal bool
	for _, tr := range got {
		if tr.Text == "hello there" && tr.Final {
			sawFinal = true
		}
	}
	if !sawFinal {
		t.Fatalf("no final {hello there true} in transcripts %+v", got)
	}
	select {
	case <-f.frames:
	default:
		t.Error("fake WS server saw no binary PCM frames (no native WS dial)")
	}
}

// TestRumbleStreamingProbe is a decision record, not a hard requirement.
//
// UPDATE (2026-11-19): the old "batch-only / vLLM stream=True emits tokens
// only after the full audio arrives" claim is SUPERSEDED. The live Rumble
// server on socrates:8000 is FastAPI + transformers v0.2.0 (not vLLM) and
// exposes WS /v1/audio/transcriptions/stream with genuine incremental
// streaming, verified by an in-session probe:
//   - client streams raw s16le mono 16 kHz PCM as binary frames;
//   - server decodes a growing window (>=1.0 s signal, >=0.8 s since growth);
//   - self-segmented {"type":"final","text":...} events for stable leading
//     words, a {"type":"partial","text":...} for the changing tail, and a
//     terminal {"type":"end","text":...} on flush (empty on silence).
//
// See docs/plans/20261119-rumble-native-streaming/master.md C2 and the gate
// assertions in TestRumbleNativeStreamE2E / TestRumbleNativeFlushE2E above.
// Consequence: stt_partial_ms is set to 0 for rumble and no client-side
// PseudoStreamSTT wrapper is used. This skip records the decision; the
// protocol itself is covered by the real hermetic gates, not deleted.
func TestRumbleStreamingProbe(t *testing.T) {
	t.Skip(`decision record: live Rumble server (socrates:8000, FastAPI+transformers v0.2.0) SUPPORTS native streaming via WS /v1/audio/transcriptions/stream — binary s16le 16 kHz PCM in, then partial/final/end JSON events out; stt_partial_ms=0, no PseudoStreamSTT wrapper. Old batch-only claim superseded 2026-11-19.`)
}
