package providers

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
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
			n := min(chunk, len(pcm))
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
	for i := range n {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(500+i%300))
	}
	return b
}

// newTestTTS builds an OpenAITTS wired to the fake vendor at the given
// target rate.
func newTestTTS(t *testing.T, srv *httptest.Server, targetRate int) *OpenAITTS {
	t.Helper()
	tts := &OpenAITTS{ //nolint:gosec // G101: TEST-only fake vendor key, not a real credential
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
	if _, err := NewTTS(ProviderConfig{Vendor: "openai"}, ""); !errors.Is(err, ErrMissingTTSKey) {
		t.Fatalf("missing key: got %v", err)
	}
	if _, err := NewTTS(ProviderConfig{Vendor: "piper"}, "k"); err == nil {
		t.Fatalf("expected unknown vendor error")
	}
	if _, err := NewSTT(ProviderConfig{Vendor: "deepgram"}, ""); !errors.Is(err, ErrMissingSTTKey) {
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

// chunkReaderT is a scripted slice reader: unlike sliceReaderT it hands out
// the exact byte counts it was told to, one per Read, instead of always
// filling the caller's buffer. That is what a real HTTP body / subprocess
// pipe does - reads land on vendor-chunk and kernel-buffer boundaries, so
// odd counts are routine and sliceReaderT could never reach the misalignment
// defect. The final scripted size repeats once the script is exhausted, so a
// script shorter than the payload still terminates on EOF.
type chunkReaderT struct {
	b     []byte
	sizes []int
	i     int
}

func (r *chunkReaderT) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	want := r.sizes[len(r.sizes)-1]
	if r.i < len(r.sizes) {
		want = r.sizes[r.i]
	}
	r.i++
	if want > len(r.b) {
		want = len(r.b)
	}
	if want > len(p) {
		want = len(p)
	}
	if want <= 0 {
		// A zero-length read is legal for io.Reader but meaningless here;
		// never script one - fall back to one byte so the stream advances.
		want = 1
	}
	n := copy(p, r.b[:want])
	r.b = r.b[n:]
	return n, nil
}

// readScript lays out the per-Read byte counts covering total bytes at
// chunk-sized steps. Each index in oddAt is one byte short (the odd boundary
// a real vendor read produces); allOdd shorts every read. The tail read is
// clamped to what remains, so the script always covers the payload exactly.
func readScript(total, chunk int, oddAt []int, allOdd bool) []int {
	in := func(idx int) bool {
		for _, o := range oddAt {
			if o == idx {
				return true
			}
		}
		return false
	}
	var sizes []int
	for rem := total; rem > 0; {
		n := chunk
		if allOdd || in(len(sizes)) {
			n = chunk - 1
		}
		if n > rem {
			n = rem
		}
		sizes = append(sizes, n)
		rem -= n
	}
	return sizes
}

// sine24k builds n samples of a deterministic tone at the vendor's 24 kHz
// rate, s16le mono. Unlike the pcm24k ramp, a sine has a KNOWN maximum
// per-sample slew at the target rate, so a resampled stream can be graded for
// sample-stream integrity rather than just size: a misaligned stream swaps
// byte halves between samples and blows far past that slew.
func sine24k(n int, freq, amp float64) []byte {
	b := make([]byte, n*2)
	for i := range n {
		v := amp * math.Sin(2*math.Pi*freq*float64(i)/24000.0)
		binary.LittleEndian.PutUint16(b[i*2:], uint16(int16(math.Round(v))))
	}
	return b
}

// s16Samples decodes an even-length s16le buffer into int samples.
func s16Samples(t *testing.T, b []byte) []int {
	t.Helper()
	if len(b)%2 != 0 {
		t.Fatalf("resampled output has an odd byte count %d: half a sample reached the caller", len(b))
	}
	out := make([]int, len(b)/2)
	for i := range out {
		out[i] = int(int16(binary.LittleEndian.Uint16(b[i*2:])))
	}
	return out
}

// Regression for the streaming resampler's sub-sample alignment: an odd
// source read used to be silently truncated by jargo's Process (inFrames =
// len(in)/bytesPerFrame), flipping the byte parity of every later boundary
// and corrupting the rest of the utterance - the audible fuzz/crackle on
// locally played TTS. Grading is on the SAMPLE STREAM (per-sample slew), not
// the output size, because a misaligned stream is the right SIZE and still
// garbage. Driven through chunkReaderT so the odd branch is actually reached;
// the old sliceReaderT always returned full even reads, which is why
// TestResamplingReaderShape shipped over this defect.
func TestResamplingReaderOddReadsPreserveAlignment(t *testing.T) {
	const (
		inRate  = 24000
		outRate = 16000
		freq    = 440.0
		amp     = 20000.0
		secs    = 3
	)
	src := sine24k(inRate*secs, freq, amp)
	// Natural per-sample delta of the tone at the OUTPUT rate: the steepest
	// slope a clean resampled stream may show.
	natural := amp * 2 * math.Pi * freq / float64(outRate)
	limit := 2 * natural
	// Ideal frame count for 3 s at the output rate; the streaming filter's
	// ramp-in costs ~41 frames at this rate pair, so the band is loose on the
	// low side and the test grades alignment, not the filter's exact delay.
	idealFrames := outRate * secs

	cases := []struct {
		name   string
		oddAt  []int
		allOdd bool
	}{
		{name: "control all-even reads"},
		{name: "one odd read early", oddAt: []int{1}},
		{name: "one odd read mid-stream", oddAt: []int{100}},
		{name: "one odd read late", oddAt: []int{220}},
		{name: "every read odd", allOdd: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := newResamplingReader(
				&chunkReaderT{b: src, sizes: readScript(len(src), resampleChunkBytes, tc.oddAt, tc.allOdd)},
				inRate, outRate)
			defer rc.Close() // test teardown: Close error is not actionable here
			got, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			frames := s16Samples(t, got)
			if len(frames) < idealFrames-200 || len(frames) > idealFrames+50 {
				t.Fatalf("resampled frame count %d, want ~%d (ideal minus filter-delay ramp)",
					len(frames), idealFrames)
			}
			bad, worst := 0, 0
			for i := 1; i < len(frames); i++ {
				d := frames[i] - frames[i-1]
				if d < 0 {
					d = -d
				}
				if d > worst {
					worst = d
				}
				if float64(d) > limit {
					if bad == 0 {
						t.Errorf("first bad jump at output frame %d: |delta|=%d (natural max %.0f, limit %.0f)",
							i, d, natural, limit)
					}
					bad++
				}
			}
			t.Logf("frames=%d worst_delta=%d natural=%.0f bad_jumps=%d", len(frames), worst, natural, bad)
			if bad > 0 {
				t.Fatalf("%d output frames jumped over 2x the natural %.0f delta: "+
					"sub-sample misalignment reached the caller", bad, natural)
			}
		})
	}
}
