package providers

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ttsFake issues OpenAI-shaped /audio/speech responses: it records the
// request and streams raw PCM (24 kHz s16le) in small chunks so the
// incremental-consumption contract is observable.
func ttsFake(t *testing.T, pcm []byte, chunk int, status int, seen *ttsSeen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.auth = r.Header.Get("Authorization")
		seen.model = r.URL.Path
		seen.body = make([]byte, r.ContentLength)
		if _, err := io.ReadFull(r.Body, seen.body); err != nil {
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		flusher := w.(http.Flusher)
		for len(pcm) > 0 {
			n := chunk
			if n > len(pcm) {
				n = len(pcm)
			}
			if _, err := w.Write(pcm[:n]); err != nil {
				return
			}
			flusher.Flush()
			pcm = pcm[n:]
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ttsSeen records what the fake vendor received.
type ttsSeen struct {
	auth  string
	model string
	body  []byte
}

// pcm24k builds n samples of a ramp at the vendor's fixed 24 kHz rate.
func pcm24k(n int) []byte {
	b := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(500+i%300))
	}
	return b
}

// newTestTTS builds an OpenAITTS wired to the fake vendor at the given
// target rate.
func newTestTTS(t *testing.T, srv *httptest.Server, targetRate int) *OpenAITTS {
	t.Helper()
	tts := &OpenAITTS{
		cfg:    ProviderConfig{Vendor: "openai", Model: "gpt-4o-mini-tts", SampleRate: targetRate},
		apiKey: "test-tts-key",
	}
	tts.setTTSTransport(&http.Transport{})
	// Point the request at the fake: the path constant is package-level,
	// so the transport rewrites the URL (test-only RoundTripper).
	base := srv.URL
	tts.transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(base, "http://")
		return http.DefaultTransport.RoundTrip(req)
	})
	return tts
}

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// T3: text -> PCM stream with the vendor auth and request shape; audio
// flows incrementally (reader yields before the body is fully drained).
func TestTTSSpeakStreams(t *testing.T) {
	seen := &ttsSeen{}
	// 48000 samples = 2 s of audio at 24 kHz; chunked at 960 bytes (480
	// samples) so incremental delivery is visible.
	srv := ttsFake(t, pcm24k(48000), 960, http.StatusOK, seen)
	tts := newTestTTS(t, srv, 16000)

	rc, err := tts.Speak(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	defer rc.Close() // test teardown: Close error is not actionable here

	if seen.auth != "Bearer test-tts-key" {
		t.Fatalf("Authorization header wrong: %q", seen.auth)
	}
	if seen.model != "/v1/audio/speech" {
		t.Fatalf("path wrong: %q", seen.model)
	}
	if !strings.Contains(string(seen.body), `"response_format":"pcm"`) ||
		!strings.Contains(string(seen.body), `"input":"hello world"`) {
		t.Fatalf("request body wrong: %s", seen.body)
	}

	// First read must succeed while the fake is still streaming (the
	// full 96 KB body takes several chunk writes; reading 640 bytes
	// immediately proves no full-buffer-then-play).
	buf := make([]byte, 640)
	n, err := io.ReadFull(rc, buf)
	if err != nil || n != len(buf) {
		t.Fatalf("first read: n=%d err=%v", n, err)
	}

	// Drain the rest: the adapter resamples 24k -> 16k inside the read
	// path, so total bytes are about 2/3 of the source.
	rest, derr := io.ReadAll(rc)
	if derr != nil {
		t.Fatalf("drain: %v", derr)
	}
	total := n + len(rest)
	want := 48000 * 2 * 16000 / 24000
	if total < want*9/10 || total > want*11/10 {
		t.Fatalf("resampled output size %d, want ~%d (24k->16k)", total, want)
	}
}

// T3: rate-matched path passes the vendor stream through unresampled.
func TestTTSPassthroughAtVendorRate(t *testing.T) {
	seen := &ttsSeen{}
	src := pcm24k(2400) // 100 ms
	srv := ttsFake(t, src, 960, http.StatusOK, seen)
	tts := newTestTTS(t, srv, 24000) // target == vendor rate: no resampler

	rc, err := tts.Speak(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	defer rc.Close() // test teardown: Close error is not actionable here
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != len(src) {
		t.Fatalf("passthrough size %d, want %d", len(got), len(src))
	}
}

// T3 negative: vendor error status fails Speak with a named shape.
func TestTTSBadStatus(t *testing.T) {
	seen := &ttsSeen{}
	srv := ttsFake(t, nil, 960, http.StatusUnauthorized, seen)
	tts := newTestTTS(t, srv, 16000)
	if _, err := tts.Speak(context.Background(), "hi"); err == nil {
		t.Fatalf("expected error on 401")
	}
}

// Constructor guards: missing key and unknown vendor.
func TestTTSConstructorGuards(t *testing.T) {
	if _, err := NewTTS(ProviderConfig{Vendor: "openai"}, ""); err != ErrMissingTTSKey {
		t.Fatalf("missing key: got %v", err)
	}
	if _, err := NewTTS(ProviderConfig{Vendor: "piper"}, "k"); err == nil {
		t.Fatalf("expected unknown vendor error")
	}
	if _, err := NewSTT(ProviderConfig{Vendor: "deepgram"}, ""); err != ErrMissingSTTKey {
		t.Fatalf("missing key: got %v", err)
	}
	if _, err := NewSTT(ProviderConfig{Vendor: "whispercpp"}, "k"); err == nil {
		t.Fatalf("expected unknown vendor error")
	}
}

// T3: the resampler produces a monotonic ramp at the target rate (spot
// check the converted stream's shape via jargo's Resample on a buffer).
func TestResamplingReaderShape(t *testing.T) {
	src := pcm24k(4800) // 200 ms at 24 kHz
	rc := newResamplingReader(&sliceReaderT{b: src}, 24000, 16000)
	defer rc.Close() // test teardown: Close error is not actionable here
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// 200 ms at 16 kHz = 3200 samples = 6400 bytes, within filter-delay
	// tolerance.
	if len(got) < 6000 || len(got) > 6800 {
		t.Fatalf("resampled size %d, want ~6400", len(got))
	}
	// s16le non-silence: the ramp keeps nonzero samples.
	sawNonZero := false
	for i := 0; i+1 < len(got); i += 2 {
		if binary.LittleEndian.Uint16(got[i:]) != 0 {
			sawNonZero = true
			break
		}
	}
	if !sawNonZero {
		t.Fatalf("resampled audio is all silence")
	}
}

// newSliceReader is a tiny io.Reader over a byte slice.
type sliceReaderT struct{ b []byte }

func (r *sliceReaderT) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}
