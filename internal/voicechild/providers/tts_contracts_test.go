package providers

// TTS branch gate tests (plan 20261118-voice-tts-stt-optimizations,
// 03-tts-path/00T-tts-tests.md T2 + F6/F8/F9).
//
//   TestCloudKeepAlive        — F6: cloud OpenAI must be constructed with a
//     keep-alive HTTP transport (DisableKeepAlives=false) bounded by an
//     IdleConnTimeout, while qwen3 keeps the no-keepalive transport. Since
//     the impl splits one shared transport var into two, the gate asserts
//     the CONTRACT SHAPE through reflection (a keep-alive *http.Transport
//     symbol must exist in the package and be used by the cloud path).
//     Today only the no-keepalive transport exists -> RED.
//   TestQwen3NoKeepAlive      — F6 legacy pin (GREEN today): the qwen3
//     provider's transport still has DisableKeepAlives=true.
//   TestResamplerReuseByteIdentical — F8: repeated Reads produce identical
//     bytes regardless of the internal scratch-buffer strategy; the gate
//     pins byte identity and the reuse seam (a scratch field on the
//     resamplingReader) via reflection -> RED today for the missing field.
//   TestQwen3ChunkSizeOverride — F9: the qwen3 pcm-stream request body's
//     chunk_size comes from a per-call override (ProviderConfig field),
//     defaulting to 4 when the override is 0. Missing field -> RED.
//
// All new symbols are probed by reflection so the package compiles today;
// failures are assertion failures (missing symbol/behavior), not syntax or
// build errors. No implementation file is touched. ASCII only.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/voicechild"
)

// haveKeepAliveTransport reports whether the package defines a package-level
// *http.Transport-shaped var (other than the known no-keepalive one) whose
// DisableKeepAlives is false and whose IdleConnTimeout is > 0. We discover
// candidates by scanning package test-visible symbols through the provider
// constructor's behavior instead: the cloud provider must end up with a
// RoundTripper that is NOT the shared no-keepalive ttsTransport.
//
// Because package-level vars are not directly enumerable via reflection, the
// check inspects the concrete *http.Transport referenced by the cloud
// provider after construction. That value is unexported, so we read it via
// reflection off the provider struct (field name "transport", already the
// test seam used by setTTSTransport).
func transportOfProvider(t *testing.T, p voicechild.TTSProvider) *http.Transport {
	t.Helper()
	oa, ok := p.(*OpenAITTS)
	if !ok {
		t.Fatalf("provider is %T, want *OpenAITTS", p)
	}
	rt := oa.transport
	if rt == nil {
		t.Fatalf("cloud provider transport is nil")
	}
	tr, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", rt)
	}
	return tr
}

// TestCloudKeepAlive is RED today: the cloud provider is constructed with
// ttsTransport (DisableKeepAlives=true). After F6 lands it must use a
// distinct keep-alive transport with a bounded idle timeout.
func TestCloudKeepAlive(t *testing.T) {
	p, err := NewTTS(ProviderConfig{
		Vendor:     TTSVendorOpenAI,
		Model:      "gpt-4o-mini-tts",
		BaseURL:    "https://api.openai.test/v1",
		SampleRate: 16000,
	}, "test-only-key") //nolint:gosec // G101: TEST-only fake vendor key
	if err != nil {
		t.Fatalf("NewTTS(cloud openai): %v", err)
	}
	tr := transportOfProvider(t, p)
	if tr.DisableKeepAlives {
		t.Errorf("F6 missing: cloud OpenAI transport has DisableKeepAlives=true (want keep-alive enabled)")
	}
	if tr.IdleConnTimeout <= 0 {
		t.Errorf("F6 missing: cloud keep-alive transport IdleConnTimeout=%v (want a bounded positive timeout)", tr.IdleConnTimeout)
	}
}

// TestQwen3NoKeepAlive is the F6 legacy pin (GREEN today and after): the
// qwen3 provider must keep the no-keepalive transport byte-identically so
// the stale-uvicorn workaround is not regressed.
func TestQwen3NoKeepAlive(t *testing.T) {
	p, err := NewTTS(ProviderConfig{
		Vendor:  TTSVendorQwen3,
		BaseURL: "http://127.0.0.1:8880",
		Voice:   "vivian",
	}, "")
	if err != nil {
		t.Fatalf("NewTTS(qwen3): %v", err)
	}
	tr := transportOfProvider(t, p)
	if !tr.DisableKeepAlives {
		t.Errorf("F6 regression: qwen3 transport has DisableKeepAlives=false (must stay true, legacy no-keepalive)")
	}
}

// chunkOverrideSeen records the request body received by the fake qwen3
// server in TestQwen3ChunkSizeOverride.
type chunkOverrideSeen struct {
	body string
}

// serveChunkOverride serves a minimal valid empty pcm-stream response and
// records the POST body. No real vendor is contacted.
func serveChunkOverride(t *testing.T, seen *chunkOverrideSeen) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen.body = string(b)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(nil)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestQwen3ChunkSizeOverride is RED today for the missing ProviderConfig
// override: with the selector set to N the pcm-stream body must carry
// chunk_size=N; with it unset (0) the legacy default 4 must be sent.
func TestQwen3ChunkSizeOverride(t *testing.T) {
	cfgType := reflect.TypeFor[voicechild.ProviderConfig]()
	f, ok := cfgType.FieldByName("Qwen3ChunkSize")
	if !ok || (f.Type.Kind() != reflect.Int && f.Type.Kind() != reflect.Ptr) {
		t.Errorf("F9 missing: ProviderConfig.Qwen3ChunkSize int (0 => default 4, positive => override); got present=%v", ok)
	}

	run := func(override int, want int) {
		t.Helper()
		seen := &chunkOverrideSeen{}
		base := serveChunkOverride(t, seen)

		cfg := ProviderConfig{
			Vendor:     TTSVendorQwen3,
			BaseURL:    base,
			Voice:      "vivian",
			SampleRate: 0, // no resample: keep the response path minimal
		}
		// Set the override reflectively so this gate file COMPILES today
		// (a direct field reference would break the whole package build).
		v := reflect.ValueOf(&cfg).Elem().FieldByName("Qwen3ChunkSize")
		if v.IsValid() && v.CanSet() {
			switch v.Kind() {
			case reflect.Int:
				v.SetInt(int64(override))
			case reflect.Ptr:
				if override == 0 {
					v.Set(reflect.Zero(v.Type()))
				} else {
					p := reflect.New(v.Type().Elem())
					p.Elem().SetInt(int64(override))
					v.Set(p)
				}
			}
		}

		tts, err := NewTTS(cfg, "")
		if err != nil {
			t.Fatalf("NewTTS(qwen3): %v", err)
		}
		rc, err := tts.Speak(context.Background(), "hi")
		if err != nil {
			t.Fatalf("Speak: %v", err)
		}
		defer func() { _ = rc.Close() }()

		var req map[string]any
		if err := json.Unmarshal([]byte(seen.body), &req); err != nil {
			t.Fatalf("request body not JSON: %v: %q", err, seen.body)
		}
		cs, present := req["chunk_size"].(float64)
		if !present {
			t.Fatalf("F9: request body missing chunk_size: %q", seen.body)
		}
		if int(cs) != want {
			t.Errorf("F9: chunk_size = %v, want %d (override=%d): %q", cs, want, override, seen.body)
		}
		if _, hasText := req["text"]; !hasText {
			t.Errorf("qwen3 request body lost text field: %q", seen.body)
		}
	}

	// Unset => legacy default 4 (GREEN today).
	run(0, 4)
	// Explicit override 1 => body must carry 1 (RED today: hardcoded 4).
	run(1, 1)
}

// TestResamplerReuseByteIdentical pins F8's byte-identity contract: feeding
// the same PCM through the resampler in many short reads yields the same
// bytes as one big read, and the impl exposes a reuse seam (a scratch byte
// slice field on the resamplingReader) instead of allocating per Read.
func TestResamplerReuseByteIdentical(t *testing.T) {
	rrType := reflect.TypeFor[resamplingReader]()
	hasScratch := false
	for i := 0; i < rrType.NumField(); i++ {
		f := rrType.Field(i)
		if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Uint8 {
			name := strings.ToLower(f.Name)
			if strings.Contains(name, "scratch") || strings.Contains(name, "buf") || strings.Contains(name, "reuse") {
				hasScratch = true
			}
		}
	}
	if !hasScratch {
		t.Errorf("F8 missing: resamplingReader has no reusable []byte scratch/buffer field (today Read allocates make([]byte,640) per call)")
	}

	// Byte identity: odd-byte reads must produce exactly the same stream
	// regardless of how the source chunks it. This pin must stay GREEN.
	const inRate, outRate = 24000, 16000
	src := pcm24k(1200) // even sample count
	want := resampleAll(t, bytes.NewReader(src), inRate, outRate)
	got := resampleAll(t, &oddReader{data: src}, inRate, outRate)
	if !bytes.Equal(got, want) {
		t.Errorf("F8: chunked/odd reads changed output bytes: got %d bytes, want %d", len(got), len(want))
	}
}

// resampleAll drains a resamplingReader fully.
func resampleAll(t *testing.T, src io.Reader, inRate, outRate int) []byte {
	t.Helper()
	r, err := ResampleS16Mono(src, inRate, outRate)
	if err != nil {
		t.Fatalf("ResampleS16Mono: %v", err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read resampled: %v", err)
	}
	return b
}

// oddReader returns at most one byte per Read, exercising convertAligned's
// carry across many boundaries.
type oddReader struct {
	data []byte
}

func (o *oddReader) Read(p []byte) (int, error) {
	if len(o.data) == 0 {
		return 0, io.EOF
	}
	n := 1
	if len(p) < n {
		n = len(p)
	}
	copy(p, o.data[:n])
	o.data = o.data[n:]
	return n, nil
}
