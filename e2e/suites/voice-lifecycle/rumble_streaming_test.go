//go:build e2e

package voicelifecycle

// RED gate for the Rumble streaming + chat_template_kwargs tree (leaf 00Te).
// F1: corrected chat.completions request shape (audio_url data URI).
// F2: provider-agnostic client-side pseudo-stream partials (opt-in).
// F3: chat_template_kwargs reach the request body.
// F5: keys-frame selectors carry the additive C2/C4 fields.
//
// These tests are gates only: they do NOT change implementation. They are RED
// today (compile-RED on the C2 fields; runtime-RED on the old JSON body path)
// and turn GREEN when leaves 01 (transport + fields) and 02 (decorator) land.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/caimlas/meept/internal/voicechild/providers"
)

// rumbleChatRequest is the frozen C3 request shape decoded off the wire.
type rumbleChatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text,omitempty"`
			AudioURL struct {
				URL string `json:"url"`
			} `json:"audio_url"`
		} `json:"content"`
	} `json:"messages"`
	Temperature        float64        `json:"temperature"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs"`
}

// newRumbleChatServer is a hermetic fake vLLM chat.completions endpoint. It
// asserts the transport contract on every POST and returns one transcript in
// the C3 response shape. No credentials, no network beyond loopback.
func newRumbleChatServer(t *testing.T, transcript string, check func(rumbleChatRequest)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("rumble request carried an Authorization header (keyless vendor)")
		}
		var body rumbleChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		} else if check != nil {
			check(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, transcript)
	}))
}

// assertC3Envelope pins the model-card body shape shared by all three gates.
func assertC3Envelope(t *testing.T, body rumbleChatRequest) {
	t.Helper()
	if body.Model == "" {
		t.Errorf("request body missing model: %+v", body)
	}
	if len(body.Messages) != 1 || len(body.Messages[0].Content) != 1 {
		t.Errorf("messages shape = %+v, want one message with one content part", body.Messages)
		return
	}
	part := body.Messages[0].Content[0]
	if part.Type != "audio_url" {
		t.Errorf("content part type = %q, want audio_url", part.Type)
	}
	if !strings.HasPrefix(part.AudioURL.URL, "data:audio/wav;base64,") {
		t.Errorf("audio_url.url = %q, want data:audio/wav;base64,<WAV>", part.AudioURL.URL)
	}
	if body.Temperature != 0 {
		t.Errorf("temperature = %v, want 0", body.Temperature)
	}
}

// newRumbleSTT constructs the real adapter through the C1 factory seam using
// the decoded C2 selectors. The C2 kwargs/partial fields are intentionally
// referenced directly: they do not exist on ProviderConfig yet, which is the
// compile-RED reason for this gate until leaf 01 lands.
func newRumbleSTT(t *testing.T, baseURL string, cfg providers.ProviderConfig) voicechild.STTProvider {
	t.Helper()
	cfg.Vendor = "rumble"
	if cfg.Model == "" {
		cfg.Model = "Reson8/Rumble-2-Flash"
	}
	cfg.BaseURL = baseURL
	cfg.SampleRate = 16000
	stt, err := providers.NewSTT(cfg, "" /* keyless */)
	if err != nil {
		t.Fatalf("NewSTT(rumble) from selectors: %v", err)
	}
	return stt
}

const rumbleTranscript = "hello rumble"

// testRumbleBool is a helper for the *bool C2 kwargs selectors.
func testRumbleBool(b bool) *bool { return &b }

// TestRumbleChatCompletionsE2E gates F1: with selectors absent the corrected
// adapter POSTs a chat.completions audio_url body to the fake vLLM and exactly
// one Final transcript reaches the caller. RED until leaf 01 fixes the
// transport (old code POSTs a JSON {model,file} body to /v1/audio/transcriptions).
func TestRumbleChatCompletionsE2E(t *testing.T) {
	vllm := newRumbleChatServer(t, rumbleTranscript, func(body rumbleChatRequest) {
		assertC3Envelope(t, body)
	})
	defer vllm.Close()

	stt := newRumbleSTT(t, vllm.URL, providers.ProviderConfig{})

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

	var finals int
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case tr, ok := <-out:
			if !ok {
				goto done
			}
			if tr.Text != rumbleTranscript || !tr.Final {
				t.Errorf("transcript = %+v, want {%s true}", tr, rumbleTranscript)
			}
			finals++
		case <-deadline.C:
			t.Fatal("timed out waiting for the stream to close after one Final")
		}
	}
done:
	if finals != 1 {
		t.Fatalf("got %d transcripts, want exactly one Final", finals)
	}
}

// TestRumblePartialStreamE2E gates F2: with PartialIntervalMs > 0 the
// provider-agnostic decorator emits Final=false partials while PCM is still
// arriving, then exactly one Final=true after the PCM channel closes. RED
// until leaf 02 lands (today the field is ignored, so no partials appear).
func TestRumblePartialStreamE2E(t *testing.T) {
	vllm := newRumbleChatServer(t, rumbleTranscript, func(body rumbleChatRequest) {
		assertC3Envelope(t, body)
	})
	defer vllm.Close()

	stt := newRumbleSTT(t, vllm.URL, providers.ProviderConfig{
		PartialIntervalMs: 150,
	})

	pcm := make(chan []byte)
	feedDone := make(chan struct{})
	go func() {
		// ~500ms of paced audio gives the 150ms decorator several intermediate
		// snapshot windows before the channel is closed.
		for range 10 {
			pcm <- make([]byte, 640)
			time.Sleep(50 * time.Millisecond)
		}
		close(feedDone)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// A partial must arrive BEFORE pcm is closed.
	sawPartial := false
	partialDeadline := time.NewTimer(4 * time.Second)
	defer partialDeadline.Stop()
	for !sawPartial {
		select {
		case tr, ok := <-out:
			if !ok {
				t.Fatal("stream closed with no partial before pcm close")
			}
			if tr.Final {
				t.Errorf("Final transcript arrived before pcm close: %+v", tr)
			} else {
				sawPartial = true
			}
		case <-partialDeadline.C:
			t.Fatal("timed out waiting for a non-final partial before pcm close (decorator missing)")
		}
	}

	// Do not close the channel while the feeder may still send.
	<-feedDone
	close(pcm)

	// Exactly one Final AFTER pcm close.
	var finals int
	finalDeadline := time.NewTimer(4 * time.Second)
	defer finalDeadline.Stop()
	for {
		select {
		case tr, ok := <-out:
			if !ok {
				goto drained
			}
			if tr.Final {
				finals++
			}
		case <-finalDeadline.C:
			t.Fatal("timed out waiting for the Final transcript after pcm close")
		}
	}
drained:
	if finals != 1 {
		t.Fatalf("got %d Final transcripts after close, want exactly 1", finals)
	}
}

// TestRumbleTemplateKwargsE2E gates F3: C2 kwargs selectors are serialized as
// chat_template_kwargs in the POST body. RED until leaf 01 adds the fields to
// ProviderConfig and emits them in the Rumble request.
func TestRumbleTemplateKwargsE2E(t *testing.T) {
	vllm := newRumbleChatServer(t, rumbleTranscript, func(body rumbleChatRequest) {
		assertC3Envelope(t, body)
		got := body.ChatTemplateKwargs
		if got["language"] != "English" {
			t.Errorf("chat_template_kwargs.language = %v, want English", got["language"])
		}
		if got["digits"] != false {
			t.Errorf("chat_template_kwargs.digits = %v, want false", got["digits"])
		}
		if got["filler"] != true {
			t.Errorf("chat_template_kwargs.filler = %v, want true", got["filler"])
		}
		if got["verbatim"] != false {
			t.Errorf("chat_template_kwargs.verbatim = %v, want false", got["verbatim"])
		}
	})
	defer vllm.Close()

	stt := newRumbleSTT(t, vllm.URL, providers.ProviderConfig{
		ChatTemplateLanguage: "English",
		ChatTemplateDigits:   testRumbleBool(false),
		ChatTemplateFiller:   testRumbleBool(true),
		ChatTemplateVerbatim: testRumbleBool(false),
	})

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
	tr, ok := <-out
	if !ok || !tr.Final || tr.Text != rumbleTranscript {
		t.Fatalf("transcript = %+v (ok=%v), want {%s true}", tr, ok, rumbleTranscript)
	}
}
