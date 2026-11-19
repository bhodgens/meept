//go:build e2e

package voicelifecycle

// F1 gate: a keys line with stt_vendor=rumble constructs the real Rumble
// adapter and a Final transcript reaches the talk loop. F4 decision record:
// Rumble is batch-only; vllm stream=True emits output tokens only after the
// full audio arrives (docs/research/rumble2flash-review.md:45). The
// streaming probe therefore records that decision with an honest skip.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild/providers"
)

// TestVoiceKeysRumble01 drives the real voicechild keys-frame seam:
// a daemon-style keys line carrying stt_vendor=rumble must select the
// Rumble adapter through providers.NewSTT, with no key material in the
// frame (Rumble is keyless). A fake loopback vLLM transcription server
// answers the adapter's POST with one transcript.
func TestVoiceKeysRumble01(t *testing.T) {
	vllm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/audio/transcriptions") {
			t.Errorf("request path = %q, want /v1/audio/transcriptions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("rumble request carried an Authorization header (keyless vendor)")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if _, ok := body["file"]; !ok {
			t.Errorf("request body missing base64 audio field 'file': %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"text":"hello there"}`)
	}))
	defer vllm.Close()

	// The keys line is the contract under test: build the provider from
	// its decoded fields through the REAL NewSTT factory (the same seam
	// the C1 keys frame feeds in cmd/meept-voice).
	stt, err := providers.NewSTT(providers.ProviderConfig{
		Vendor:     "rumble",
		Model:      "Reson8/Rumble-2-Flash",
		BaseURL:    vllm.URL,
		SampleRate: 16000,
	}, "" /* keyless */)
	if err != nil {
		t.Fatalf("NewSTT(rumble) from keys-line fields: %v", err)
	}

	// Pump one voiced window plus trailing silence and close, so the
	// adapter's segmentation flushes exactly one utterance.
	pcm := make(chan []byte)
	go func() {
		for range 40 {
			pcm <- make([]byte, 640)
		}
		close(pcm)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got, ok := <-out
	if !ok {
		t.Fatal("stream closed with no transcript")
	}
	if got.Text != "hello there" || !got.Final {
		t.Fatalf("transcript = %+v, want {hello there true}", got)
	}
}

// TestRumbleStreamingProbe is F4's decision record, not a hard requirement.
// Rumble has no supported incremental-input API: vLLM stream=True streams
// output tokens only after the full audio is received. Skip with the cited
// research so the tree never claims partial transcripts for this adapter.
func TestRumbleStreamingProbe(t *testing.T) {
	t.Skip("Rumble batch-only: vllm stream=True emits tokens after full audio; see docs/research/rumble2flash-review.md:45")
}
