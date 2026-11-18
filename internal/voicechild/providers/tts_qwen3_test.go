package providers

// Gate tests T3 (feature F3, master.md C2 + leaf 04) plus the qwen3
// token-level pcm-stream route change:
//   - keyless construction (D2)
//   - request goes to /v1/audio/speech/pcm-stream with the route's own
//     body shape (text, voice, chunk_size — NO stream/response_format)
//   - length-prefixed response frames are deframed, then resampled
//     24 kHz -> negotiated-rate s16le mono
//   - incremental first chunk arrives before the body ends

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// qwen3Seen records what the fake local server received.
type qwen3Seen struct {
	mu   sync.Mutex
	auth string
	path string
	body string
}

func (s *qwen3Seen) snapshot() (string, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth, s.path, s.body
}

// framePCM wraps raw 24 kHz PCM in the pcm-stream route's envelope:
// [4-byte BE uint32 length][length bytes PCM] ... [0-length frame].
func framePCM(pcm []byte, frameBytes int) []byte {
	var out []byte
	for len(pcm) > 0 {
		n := min(frameBytes, len(pcm))
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(n))
		out = append(out, hdr[:]...)
		out = append(out, pcm[:n]...)
		pcm = pcm[n:]
	}
	var zero [4]byte
	return append(out, zero[:]...)
}

// qwen3Fake serves the pcm-stream route: length-prefixed PCM frames
// flushed one at a time so the first-chunk contract is observable before
// the body ends.
func qwen3Fake(t *testing.T, framed []byte, seen *qwen3Seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Read the header BEFORE the lock: mutexio flags any `.Get(`
		// call under a held mutex, and http.Header.Get is a map read
		// it cannot distinguish from an HTTP client call.
		auth := r.Header.Get("Authorization")
		seen.mu.Lock()
		seen.auth = auth
		seen.path = r.URL.Path
		seen.body = string(body)
		seen.mu.Unlock()
		w.Header().Set("Content-Type", "application/octet-stream")
		flusher := w.(http.Flusher)
		// Write in small pieces, flushing after each, so a whole-body
		// buffering adapter cannot produce a first chunk early.
		const chunk = 480
		for len(framed) > 0 {
			n := min(chunk, len(framed))
			if _, err := w.Write(framed[:n]); err != nil {
				return
			}
			flusher.Flush()
			time.Sleep(2 * time.Millisecond)
			framed = framed[n:]
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestQwen3TTSConstruction pins the constructor matrix (D2 keyless rule).
func TestQwen3TTSConstruction(t *testing.T) {
	var cfg ProviderConfig
	cfg.Vendor = "qwen3"
	cfg.BaseURL = "http://127.0.0.1:8880"
	cfg.Model = "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice"
	cfg.Voice = "vivian"

	// Keyless construction MUST succeed (contract C2: qwen3 speaks with
	// key ""; ErrMissingTTSKey only for cloud vendors).
	tts, err := NewTTS(cfg, "")
	if err != nil {
		t.Fatalf("missing behavior: NewTTS(vendor=qwen3, key=\"\") = %v, must construct (leaf 04 D2)", err)
	}
	if tts == nil {
		t.Fatal("NewTTS returned nil provider without error")
	}
	var _ = tts

	// openai with key "" still fails ErrMissingTTSKey (unchanged).
	if _, err := NewTTS(ProviderConfig{Vendor: "openai", Model: "gpt-4o-mini-tts"}, ""); !errors.Is(err, ErrMissingTTSKey) {
		t.Fatalf("NewTTS(openai, key=\"\") err = %v, want ErrMissingTTSKey", err)
	}

	// qwen3 with an empty BaseURL is a named constructor error
	// (ErrMissingTTSBaseURL; a local server has no meaningful default).
	var noURL ProviderConfig
	noURL.Vendor = "qwen3"
	noURL.Model = "m"
	noURL.Voice = "v"
	_, err = NewTTS(noURL, "")
	if err == nil {
		t.Fatal("missing behavior: NewTTS(qwen3, BaseURL=\"\") must error (leaf 04 ErrMissingTTSBaseURL)")
	}
	if !strings.Contains(err.Error(), "base_url") && !strings.Contains(err.Error(), "base URL") {
		t.Errorf("qwen3 empty-BaseURL error should name the missing base URL, got: %v", err)
	}
}

// TestQwen3TTSSpeakStreaming pins the token-level route, wire body shape,
// deframe + resample behavior, and the streaming first-chunk contract.
func TestQwen3TTSSpeakStreaming(t *testing.T) {
	seen := &qwen3Seen{}
	// 24000 samples = 1 s at 24 kHz => ~16000 samples after the 24k->16k
	// resample. Frame it at 4800 B (100 ms/frame) like the real route.
	src := pcm24k(24000)
	framed := framePCM(src, 4800)
	srv := qwen3Fake(t, framed, seen)

	var cfg ProviderConfig
	cfg.Vendor = "qwen3"
	cfg.SampleRate = 16000
	cfg.BaseURL = srv.URL
	cfg.Model = "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice"
	cfg.Voice = "vivian"
	tts, err := NewTTS(cfg, "")
	if err != nil {
		t.Fatalf("NewTTS(qwen3): %v", err)
	}

	rc, err := tts.Speak(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Speak: %v", err)
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
	want := 24000 * 2 * 16000 / 24000 // source bytes scaled by 16000/24000
	if total < want*9/10 || total > want*11/10 {
		t.Fatalf("deframed+resampled output %d bytes, want ~%d (24k -> 16k, ratio ~1.5)", total, want)
	}

	// Request shape: keyless vendor, token-level route, pcm-stream body.
	auth, path, body := seen.snapshot()
	if auth != "" {
		t.Errorf("qwen3 sent Authorization header %q; keyless vendors send none", auth)
	}
	if !strings.HasSuffix(path, "/v1/audio/speech/pcm-stream") {
		t.Errorf("request path %q, want /v1/audio/speech/pcm-stream suffix", path)
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("request body not JSON: %v: %s", err, body)
	}
	if req["text"] != "hi" {
		t.Errorf("text = %v, want \"hi\" (pcm-stream field name): %s", req["text"], body)
	}
	if req["voice"] != "vivian" {
		t.Errorf("voice = %v, want \"vivian\": %s", req["voice"], body)
	}
	if cs, ok := req["chunk_size"].(float64); !ok || cs != 4 {
		t.Errorf("chunk_size = %v, want 4: %s", req["chunk_size"], body)
	}
	// The pcm-stream route must NOT carry the OpenAI speech fields.
	for _, forbidden := range []string{"stream", "response_format", "input", "model"} {
		if _, present := req[forbidden]; present {
			t.Errorf("pcm-stream body must not carry %q: %s", forbidden, body)
		}
	}
}

// TestQwen3SpeechURLSeparatesVendors pins that only qwen3 is routed to
// pcm-stream; cloud openai stays byte-identical on /v1/audio/speech.
func TestQwen3SpeechURLSeparatesVendors(t *testing.T) {
	q := &OpenAITTS{cfg: ProviderConfig{Vendor: TTSVendorQwen3, BaseURL: "http://h:8001/"}}
	if got := q.speechURL(); got != "http://h:8001/v1/audio/speech/pcm-stream" {
		t.Errorf("qwen3 speechURL = %q", got)
	}
	if !q.usesPCMStream() {
		t.Error("qwen3 must use the pcm-stream route")
	}
	for _, v := range []string{"", TTSVendorOpenAI} {
		o := &OpenAITTS{cfg: ProviderConfig{Vendor: v, BaseURL: "http://h:8001/"}}
		if got := o.speechURL(); got != "http://h:8001/v1/audio/speech" {
			t.Errorf("vendor %q speechURL = %q, want cloud route", v, got)
		}
		if o.usesPCMStream() {
			t.Errorf("vendor %q must not use pcm-stream", v)
		}
	}
}
