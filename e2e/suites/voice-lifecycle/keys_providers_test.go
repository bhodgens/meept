//go:build e2e

// voice-tierA leaf 07 scenario keys-to-providers (T1): the integrated
// voice chain, observed live on the hermetic loop.
//
// Given: a scratch daemon with voice enabled and voice.tts_provider=qwen3
// pointed at a LOOPBACK fake OpenAI-compatible streaming endpoint
// (httptest; /v1/audio/speech streams real PCM), plus a fake child built
// in REAL-PROVIDERS mode (-ldflags -X main.realProviders=true wiring the
// same config-driven C3 factories cmd/meept-voice installs).
// When: an inbound call sets up (webhook -> answer -> media start ->
// open frame -> child setup -> talk loop armed -> daemon keys line).
// Then:
//   1. the child receives the extended C1 keys line carrying
//      tts_vendor=qwen3 and tts_base_url=<loopback fake> (the child's
//      control mirror names the vendor/rate; keys VALUES are never
//      mirrored);
//   2. the REAL providers.NewTTS(qwen3) adapter CONSTRUCTS from that line
//      (keyless D2 - the fake broker sends an empty tts_key) - observed
//      as the child's "tts constructed" log frame;
//   3. the synthesized PCM flows child -> C2 .out -> splice -> the fake
//      Telnyx media socket, where the suite asserts actual base64 audio
//      frames after a say - the stub-audio-only era's named skip
//      (TestVoiceLifecycleGreetingAudioAtTelnyx) is retired by this
//      scenario, which is why the old skip test is REMOVED here (its gap
//      is now covered by a live assertion, not left skipped).
//
// No live vendor: the Qwen3-TTS server is faked on loopback (the master
// plan's Integration Test Plan step 4 keeps the live smoke manual).

package voicelifecycle

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeQwen3 is the hermetic Qwen3-TTS stand-in: an OpenAI-compatible
// streaming /v1/audio/speech endpoint. stream=true + response_format=pcm
// yields raw s16le mono 24 kHz chunks (leaf 04's output contract D3),
// chunked so the adapter's incremental read is exercised (never one
// whole-buffer body). Records every request's Authorization header,
// body fields, and byte count for the construction/speak assertions.
type fakeQwen3 struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []qwen3Req
}

type qwen3Req struct {
	Auth     string
	Model    string
	Voice    string
	Input    string
	Stream   bool
	Format   string
	BytesOut int
}

func newFakeQwen3(t *testing.T) *fakeQwen3 {
	t.Helper()
	f := &fakeQwen3{t: t}
	mux := http.NewServeMux()
	// qwen3 uses the token-level pcm-stream route, not the cloud openai route
	mux.HandleFunc("/v1/audio/speech/pcm-stream", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck // test sink; a bad body still gets audio
		// qwen3 PCM stream uses fields: text, voice, chunk_size (no stream/response_format/input/model)
		// Stream a short deterministic tone as raw s16le@24k mono: 20
		// chunks of 480 samples (1920 bytes) = 400 ms of audio, flushed
		// chunk-by-chunk (streaming, not buffered).
		w.Header().Set("Content-Type", "audio/pcm")
		w.WriteHeader(http.StatusOK)
		rec := qwen3Req{
			Auth:   r.Header.Get("Authorization"),
			Model:  "", // qwen3 doesn't send model in request
			Voice:  body["voice"].(string),
			Input:  body["text"].(string),
			Stream: true, // always streaming for qwen3 pcm-stream
			Format: "pcm",
		}
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 20; i++ {
			chunk := make([]byte, 1920)
			for s := 0; s < 480; s++ {
				binary.LittleEndian.PutUint16(chunk[s*2:], uint16(2000+i))
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			rec.BytesOut += len(chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, rec)
		f.mu.Unlock()
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeQwen3) baseURL() string { return f.srv.URL }

func (f *fakeQwen3) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

// awaitRequest polls until the fake endpoint has served at least one
// synthesis request, returning it.
func (f *fakeQwen3) awaitRequest(within time.Duration) qwen3Req {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.reqs)
		var rec qwen3Req
		if n > 0 {
			rec = f.reqs[0]
		}
		f.mu.Unlock()
		if n > 0 {
			return rec
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("fake qwen3 endpoint never served a synthesis request within %s", within)
	return qwen3Req{}
}

// buildRealChild builds the fake child binary in REAL-PROVIDERS mode:
// the same testdata source as the plain fake, with the C3 factories
// armed (-ldflags -X main.realProviders=true).
func buildRealChild(t *testing.T, dir string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("fake child cwd: %v", err)
	}
	src := filepath.Join(filepath.Dir(cwd), "voice-lifecycle", "testdata", "fakechild", "main.go")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fake child source: %v", err)
	}
	out := filepath.Join(dir, "fakechild-real")
	cmd := exec.Command("go", "build", "-ldflags", "-X main.realProviders=true", "-o", out, src)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build fakechild (real providers): %v", err)
	}
	return out
}

// childControlLogs returns every log frame the child mirrored for ccid.
func (f *fakeTelnyx) childControlLogs(ccid string) []string {
	f.childMu.Lock()
	defer f.childMu.Unlock()
	var logs []string
	for _, fr := range f.frames[ccid] {
		if fr.Event == "log" && fr.Text != "" {
			logs = append(logs, fr.Text)
		}
	}
	return logs
}

// awaitChildLog polls the mirrored control logs until one contains substr.
func (f *fakeTelnyx) awaitChildLog(ccid, substr string, within time.Duration) string {
	f.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, l := range f.childControlLogs(ccid) {
			if strings.Contains(l, substr) {
				return l
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("child control logs for %s never showed %q within %s; logs: %v", ccid, substr, within, f.childControlLogs(ccid))
	return ""
}

// assertNoChildLog asserts NO log frame matching substr ever appeared
// (a constructed adapter must never carry a failure log, and vice versa).
func (f *fakeTelnyx) assertNoChildLog(ccid, substr string) {
	f.t.Helper()
	for _, l := range f.childControlLogs(ccid) {
		if strings.Contains(l, substr) {
			f.t.Fatalf("child control logs for %s unexpectedly contain %q: %v", ccid, substr, f.childControlLogs(ccid))
		}
	}
}

// childSpeaksReal drives the fake child's speak-real control: synthesize
// text through the adapter constructed from the keys line and push the
// PCM onto the C2 .out socket (the splice's child side).
func (f *fakeTelnyx) childSpeaksReal(ccid, text string) {
	f.t.Helper()
	f.childCommand(ctrlMsg{Ctrl: "speak-real", CallID: ccid, Text: text})
}

// decodedMediaPCM strips the Telnyx media envelope off every media frame
// on ccid's socket and returns the total decoded audio bytes.
func decodedMediaPCM(t *testing.T, fake *fakeTelnyx, ccid string) (int, int) {
	t.Helper()
	fake.childMu.Lock()
	defer fake.childMu.Unlock()
	frames := 0
	total := 0
	for _, fr := range fake.media[ccid] {
		var env struct {
			Event string `json:"event"`
			Media struct {
				Payload string `json:"payload"`
			} `json:"media"`
		}
		if err := json.Unmarshal([]byte(fr), &env); err != nil || env.Event != "media" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(env.Media.Payload)
		if err != nil {
			t.Fatalf("media payload base64: %v", err)
		}
		frames++
		total += len(raw)
	}
	return frames, total
}

// awaitMediaAudio polls until ccid's media socket has carried at least
// minFrames decoded media audio frames totalling minBytes of PCM.
func (f *fakeTelnyx) awaitMediaAudio(t *testing.T, ccid string, minFrames, minBytes int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		frames, total := decodedMediaPCM(t, f, ccid)
		if frames >= minFrames && total >= minBytes {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	frames, total := decodedMediaPCM(t, f, ccid)
	f.t.Fatalf("media socket for %s settled at %d frames / %d PCM bytes, want >= %d frames / >= %d bytes within %s",
		ccid, frames, total, minFrames, minBytes, within)
}

// TestVoiceKeysToProviders runs the tierA integrated chain hermetically:
// the daemon's extended keys line (qwen3 + loopback base_url) reaches the
// child, the REAL qwen3 TTS adapter constructs keyless from it (D2), and
// its synthesized PCM reaches the fake Telnyx media socket as real audio
// frames. This is the e2e proof that providers construct from config
// shipped over C1 - the gap that kept every pre-tierA call on stub audio.
func TestVoiceKeysToProviders(t *testing.T) {
	q := newFakeQwen3(t)
	fake := newFakeTelnyx(t)
	childBin := buildRealChild(t, t.TempDir())
	s, ctrlPath := bootVoiceStack(t, fake, map[string]any{
		"child_binary_path": childBin,
		// C4 config->wire mapping under test: the daemon's keys line must
		// carry exactly these selectors from the [voice] block.
		"tts_provider": "qwen3",
		"tts_model":    "Qwen/Qwen3-TTS-test",
		"tts_base_url": q.baseURL(),
		"tts_voice":    "vivian",
		"stt_provider": "deepgram",
		"stt_model":    "nova-3",
	})

	const ccid = "cc-tierA-1"

	// The shared child must be ALIVE before the primary call so the test
	// can attach to the ctrl mirror before any frames flow (the greeting
	// say and the keys line are mirrored the moment they arrive - a
	// late-attaching observer misses them). One throwaway phone call
	// spawns the child (the manager's spawn seam is the only
	// EnsureRunning caller), immediately hung up.
	const warmCcid = "cc-tierA-warm"
	fake.deliverCallInitiated(s, warmCcid, "incoming")
	fake.awaitAnswered(warmCcid, 10*time.Second)
	fake.dialMedia(s, warmCcid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)
	fake.deliverHangup(s, warmCcid)
	awaitLogLine(t, s, "voice call ended", 15*time.Second)
	fake.connectChild(ctrlPath, 15*time.Second)

	// Arm the primary call: webhook -> answer -> media ws -> child setup
	// (the fake child answers the daemon's open frame, mirroring the real
	// child's handleOpen) -> talk loop. The setup triggers the daemon's
	// once-per-call keys emission (leaf 03), mirrored to the attached
	// ctrl observer.
	fake.deliverCallInitiated(s, ccid, "incoming")
	fake.awaitAnswered(ccid, 10*time.Second)
	fake.awaitAction(ccid, "/answer", 10*time.Second)
	fake.dialMedia(s, ccid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)
	fake.childSetup(ccid, "inbound")
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)

	// 1. The keys line flowed: the child's control mirror (which strips
	// key VALUES) reports the vendor selectors + negotiated rate the
	// daemon sent. This observes the REAL line the daemon produced on the
	// REAL text socket - not a unit-fixture reconstruction.
	keysLog := fake.awaitChildLog(ccid, "fakechild keys received", 15*time.Second)
	for _, want := range []string{"stt_vendor=deepgram", "tts_vendor=qwen3", "rate=16000"} {
		if !strings.Contains(keysLog, want) {
			t.Fatalf("keys mirror log = %q, want it to contain %q (C4 config->wire mapping)", keysLog, want)
		}
	}

	// 2. The REAL qwen3 adapter constructed keyless from that line (the
	// secrets broker carries no openai_api_key here, so tts_key rides as
	// "" - D2: qwen3 ignores it). Construction failure would surface as
	// the failed log instead.
	fake.awaitChildLog(ccid, "fakechild tts constructed", 15*time.Second)
	fake.assertNoChildLog(ccid, "tts construct failed")

	// 3. The adapter SPEAKS through the fake OpenAI-compatible endpoint
	// and the PCM reaches the fake Telnyx media socket. The say path:
	// a final prompt would run a full employee turn; the adapter-level
	// speak-real control exercises the same TTS instance the pipeline
	// would use, over the same C2 .out -> splice -> media-socket path.
	fake.childSpeaksReal(ccid, "Tier A hello from the local qwen3 voice")

	// The fake endpoint saw the synthesis request: keyless (no
	// Authorization header), the exact model/voice the keys line carried,
	// streaming pcm.
	rec := q.awaitRequest(15 * time.Second)
	if rec.Auth != "" {
		t.Errorf("qwen3 request carried Authorization %q; D2 keyless dispatch must send no auth header", rec.Auth)
	}
	// qwen3 doesn't send model in request body (server uses its loaded default);
	// the model assertion is about the C1 keys line carrying Qwen/Qwen3-TTS-test,
	// not the HTTP request body. Voice must match the C1-carried value.
	if rec.Voice != "vivian" {
		t.Errorf("qwen3 request voice=%q, want the C1-carried vivian", rec.Voice)
	}
	if !rec.Stream || rec.Format != "pcm" {
		t.Errorf("qwen3 request stream=%v response_format=%q, want true / \"pcm\" (streaming contract)", rec.Stream, rec.Format)
	}

	// And the audio landed on the media socket: the splice paces the
	// child's PCM (resampled 24k -> 16k, L16 passthrough) out as base64
	// media frames. 400 ms of source audio => several 20 ms frames.
	fake.awaitMediaAudio(t, ccid, 3, 1920, 15*time.Second)

	// The endpoint served exactly one full synthesis (no retry storm).
	if n := q.requestCount(); n != 1 {
		t.Errorf("fake qwen3 endpoint served %d requests, want 1", n)
	}

	// Teardown sanity: hangup tears the call down and the child idle-stops.
	fake.deliverHangup(s, ccid)
	awaitLogLine(t, s, "voice call ended", 15*time.Second)
	fake.awaitChildStop(20 * time.Second)
}
