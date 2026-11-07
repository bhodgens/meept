package providers

// Gate tests T3 (feature F3, master.md C2 + leaf 04): the qwen3 vendor
// speaks the OpenAI-compatible streaming speech contract - keyless
// construction (D2), streaming request body, incremental first chunk,
// 24 kHz -> negotiated-rate resample.
//
// RED style: this file COMPILES (existing symbols only) and fails on the
// NEW behavior. Missing symbols named in failure messages (leaf 04):
//   NewTTS vendor case "qwen3" (today: ErrUnknownVendor)
//   ErrMissingTTSBaseURL sentinel (qwen3 with empty BaseURL)
//   stream:true request field (openAICompatibleTTSRequest.Stream)

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

// qwen3Seen records what the fake local server received.
type qwen3Seen struct {
	mu       sync.Mutex
	auth     string
	path     string
	body     string
	clientIP string
}

func (s *qwen3Seen) snapshot() (string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth, s.path, s.body
}

// qwen3Fake serves an OpenAI-compatible /v1/audio/speech that streams
// s16le 24 kHz PCM slowly (many small flushes) so the first-chunk
// contract is observable before the body ends.
func qwen3Fake(t *testing.T, pcm []byte, seen *qwen3Seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen.mu.Lock()
		seen.auth = r.Header.Get("Authorization")
		seen.path = r.URL.Path
		seen.body = string(body)
		seen.mu.Unlock()
		w.Header().Set("Content-Type", "application/octet-stream")
		flusher := w.(http.Flusher)
		const chunk = 480 // 240 samples per write: many writes for 24k samples
		for len(pcm) > 0 {
			n := chunk
			if n > len(pcm) {
				n = len(pcm)
			}
			if _, err := w.Write(pcm[:n]); err != nil {
				return
			}
			flusher.Flush()
			// Slow stream: a full-body-buffer adapter cannot produce a
			// first chunk before the handler returns.
			time.Sleep(2 * time.Millisecond)
			pcm = pcm[n:]
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newQwen3TTS builds the adapter the way leaf 04's contract shape
// requires: ProviderConfig carries Vendor/BaseURL/Model/Voice/SampleRate.
// The BaseURL field does not exist until leaf 02 lands; reflect keeps this
// file compilable, and the missing field is named in the failure.
func setQwen3Fields(t *testing.T, cfg *ProviderConfig, baseURL, model, voice string) {
	t.Helper()
	v := reflectFieldByName(*cfg, "BaseURL")
	if !v.IsValid() {
		t.Fatalf("missing symbol ProviderConfig.BaseURL (leaf 02): qwen3 adapter cannot be configured")
	}
	v.SetString(baseURL)
	reflectFieldByName(*cfg, "Model").SetString(model)
	reflectFieldByName(*cfg, "Voice").SetString(voice)
}

// reflectFieldByName returns the settable field of cfg (value copy's
// addressable equivalent) by name; invalid when leaf 02 has not added it.
func reflectFieldByName(cfg ProviderConfig, name string) reflect.Value {
	v := reflect.ValueOf(&cfg).Elem().FieldByName(name)
	return v
}

// TestQwen3TTSConstruction pins the constructor matrix (D2 keyless rule).
func TestQwen3TTSConstruction(t *testing.T) {
	var cfg ProviderConfig
	cfg.Vendor = "qwen3"
	setQwen3Fields(t, &cfg, "http://127.0.0.1:8880", "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice", "vivian")

	// Keyless construction MUST succeed (contract C2: qwen3 speaks with
	// key ""; ErrMissingTTSKey only for cloud vendors).
	tts, err := NewTTS(cfg, "")
	if err != nil {
		t.Fatalf("missing behavior: NewTTS(vendor=qwen3, key=\"\") = %v, must construct (leaf 04 D2)", err)
	}
	if tts == nil {
		t.Fatal("NewTTS returned nil provider without error")
	}
	var _ voicechild.TTSProvider = tts

	// openai with key "" still fails ErrMissingTTSKey (unchanged).
	if _, err := NewTTS(ProviderConfig{Vendor: "openai", Model: "gpt-4o-mini-tts"}, ""); err != ErrMissingTTSKey {
		t.Fatalf("NewTTS(openai, key=\"\") err = %v, want ErrMissingTTSKey", err)
	}

	// qwen3 with an empty BaseURL is a named constructor error
	// (ErrMissingTTSBaseURL; a local server has no meaningful default).
	var noURL ProviderConfig
	noURL.Vendor = "qwen3"
	setQwen3Fields(t, &noURL, "", "m", "v")
	_, err = NewTTS(noURL, "")
	if err == nil {
		t.Fatal("missing behavior: NewTTS(qwen3, BaseURL=\"\") must error (leaf 04 ErrMissingTTSBaseURL)")
	}
	if !strings.Contains(err.Error(), "base_url") && !strings.Contains(err.Error(), "base URL") {
		t.Errorf("qwen3 empty-BaseURL error should name the missing base URL, got: %v", err)
	}
}

// TestQwen3TTSSpeakStreaming pins the wire shape and the streaming
// first-chunk + resample contract end to end.
func TestQwen3TTSSpeakStreaming(t *testing.T) {
	seen := &qwen3Seen{}
	// 24000 samples = 1 s of audio at 24 kHz => ~16000 samples (32 KB)
	// after the 24000->16000 resample (ratio ~1.5 on byte count).
	src := pcm24k(24000)
	srv := qwen3Fake(t, src, seen)

	var cfg ProviderConfig
	cfg.Vendor = "qwen3"
	cfg.SampleRate = 16000
	setQwen3Fields(t, &cfg, srv.URL, "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice", "vivian")
	tts, err := NewTTS(cfg, "")
	if err != nil {
		t.Fatalf("NewTTS(qwen3): %v (leaf 04: vendor case missing)", err)
	}

	// The adapter's base URL comes from cfg.BaseURL; the constructor
	// stores it per-instance. Rewrite the transport only if the adapter
	// still targets the compile-time const (leaf 02/04 make it a field);
	// if BaseURL is honored, the fake sees the request directly.
	rc, err := tts.Speak(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Speak: %v (leaf 04: qwen3 dispatch missing)", err)
	}
	defer func() { _ = rc.Close() }() //nolint:mutexio // deferred close, no lock held

	// First read must succeed while the fake is still streaming.
	buf := make([]byte, 640)
	if n, err := io.ReadFull(rc, buf); err != nil || n != len(buf) {
		t.Fatalf("first read: n=%d err=%v (streaming first-chunk contract)", n, err)
	}

	rest, derr := io.ReadAll(rc)
	if derr != nil {
		t.Fatalf("drain: %v", derr)
	}
	total := len(buf) + len(rest)
	want := 24000 * 2 * 16000 / 24000 * 2 // source bytes scaled by 16000/24000
	want = 24000 * 2 * 16000 / 24000
	if total < want*9/10 || total > want*11/10 {
		t.Fatalf("resampled output %d bytes, want ~%d (24k -> 16k, ratio ~1.5)", total, want)
	}

	// Request shape: keyless vendor sends no Authorization header.
	auth, path, body := seen.snapshot()
	if auth != "" {
		t.Errorf("qwen3 sent Authorization header %q; keyless vendors send none (leaf 04)", auth)
	}
	if !strings.HasSuffix(path, "/v1/audio/speech") {
		t.Errorf("request path %q, want /v1/audio/speech suffix", path)
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("request body not JSON: %v: %s", err, body)
	}
	if req["response_format"] != "pcm" {
		t.Errorf("response_format = %v, want pcm (contract D3)", req["response_format"])
	}
	if stream, ok := req["stream"].(bool); !ok || !stream {
		t.Errorf("missing behavior: request body lacks \"stream\":true (leaf 04 T1): %s", body)
	}
	if m, _ := req["model"].(string); m != "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice" {
		t.Errorf("model passthrough = %v, want cfg.Model", req["model"])
	}
	if v, _ := req["voice"].(string); v != "vivian" {
		t.Errorf("voice passthrough = %v, want cfg.Voice", req["voice"])
	}
}
