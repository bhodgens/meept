//go:build e2e

package voicelifecycle

// RED e2e gates for native Rumble WebSocket streaming (plan leaf 00Te).
//
// These are gates only: they do NOT change implementation. They exercise the
// real providers.NewSTT(Vendor="rumble") factory against a hermetic, loopback
// WS fake that speaks the live server's native protocol
// (WS /v1/audio/transcriptions/stream):
//   - binary frames: raw s16le mono 16 kHz PCM
//   - empty binary frame: flush
//   - text frames: {"type":"partial"|"final"|"end","text":...}
//
// RED today because the Rumble adapter is still the batch HTTP
// (chat.completions) implementation: it never upgrades the socket, never
// observes the binary frames, and never maps partial/final/end events.
// No real socrates, no credentials, no network beyond httptest loopback.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/caimlas/meept/internal/voicechild/providers"
	"github.com/coder/websocket"
)

const rumbleWSPath = "/v1/audio/transcriptions/stream"

// rumbleFakeCfg controls when the fake answers and which events it sends.
type rumbleFakeCfg struct {
	// emitAfter: number of non-empty binary frames after which the fake
	// emits cfg.events. Use a huge value to emit only on the empty flush
	// frame.
	emitAfter int
	events    []string
}

// rumbleFake holds what the fake observed off the wire.
type rumbleFake struct {
	frames   chan []byte // every binary frame received, including the flush
	httpHits chan string // any non-WS request (legacy HTTP path), if any
}

// newRumbleWSFake starts a hermetic WS vendor on an httptest loopback server.
// http://<srv.URL> is upgraded to ws:// by the adapter's dialURL helper; the
// fake accepts only the native stream path so a legacy HTTP POST is visible
// as an httpHits entry and cannot accidentally satisfy a gate.
func newRumbleWSFake(t *testing.T, cfg rumbleFakeCfg) (*httptest.Server, *rumbleFake) {
	t.Helper()
	f := &rumbleFake{
		frames:   make(chan []byte, 256),
		httpHits: make(chan string, 32),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != rumbleWSPath {
			select {
			case f.httpHits <- r.Method + " " + r.URL.Path:
			default:
			}
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("rumble WS upgrade carried Authorization %q (keyless vendor)", got)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()
		binaryCount, emitted := 0, false
		for {
			mt, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if mt != websocket.MessageBinary {
				continue
			}
			frame := append([]byte(nil), data...)
			select {
			case f.frames <- frame:
			default:
			}
			binaryCount++
			// An empty binary frame is the native flush signal; the live
			// server replies final(s) then end on flush.
			if !emitted && (binaryCount >= cfg.emitAfter || len(frame) == 0) {
				emitted = true
				for _, ev := range cfg.events {
					if werr := conn.Write(ctx, websocket.MessageText, []byte(ev)); werr != nil {
						return
					}
				}
			}
		}
	}))
	return srv, f
}

// newRumbleNativeSTT builds the real adapter through the C1 factory seam,
// exactly as the keys-frame path does in cmd/meept-voice. Keyless: "".
func newRumbleNativeSTT(t *testing.T, baseURL string, cfg providers.ProviderConfig) voicechild.STTProvider {
	t.Helper()
	cfg.Vendor = "rumble"
	if cfg.Model == "" {
		cfg.Model = "Reson8/Rumble-2-Flash"
	}
	cfg.BaseURL = baseURL
	cfg.SampleRate = 16000
	stt, err := providers.NewSTT(cfg, "")
	if err != nil {
		t.Fatalf("NewSTT(rumble): %v", err)
	}
	return stt
}

// feedRumblePCM pumps paced 640-byte/20 ms L16 chunks and closes the channel.
func feedRumblePCM(pcm chan<- []byte, chunks int, gap time.Duration) {
	go func() {
		for i := 0; i < chunks; i++ {
			pcm <- make([]byte, 640)
			if gap > 0 {
				time.Sleep(gap)
			}
		}
		close(pcm)
	}()
}

// collectRumble drains out until it closes or ctx expires.
func collectRumble(t *testing.T, ctx context.Context, out <-chan voicechild.Transcript) []voicechild.Transcript {
	t.Helper()
	var got []voicechild.Transcript
	for {
		select {
		case tr, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, tr)
		case <-ctx.Done():
			t.Fatalf("timed out waiting for the Rumble stream to close (got %d transcripts)", len(got))
			return got
		}
	}
}

// TestRumbleNativeStreamE2E (rumble-native-stream-01, F1): with a fake native
// WS server, feeding PCM must surface the server's partial (Final=false) and
// final (Final=true) events through the real NewSTT factory. RED today: the
// batch adapter POSTs chat.completions and never upgrades the socket.
func TestRumbleNativeStreamE2E(t *testing.T) {
	const (
		partialText = "hello"
		finalText   = "hello rumble"
	)
	srv, f := newRumbleWSFake(t, rumbleFakeCfg{
		emitAfter: 3,
		events: []string{
			`{"type":"partial","text":"` + partialText + `"}`,
			`{"type":"final","text":"` + finalText + `"}`,
			`{"type":"end","text":"` + finalText + `"}`,
		},
	})
	defer srv.Close()

	stt := newRumbleNativeSTT(t, srv.URL, providers.ProviderConfig{})
	pcm := make(chan []byte)
	feedRumblePCM(pcm, 40, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := collectRumble(t, ctx, out)

	var sawPartial, sawFinal bool
	for _, tr := range got {
		if tr.Text == partialText && !tr.Final {
			sawPartial = true
		}
		if tr.Text == finalText && tr.Final {
			sawFinal = true
		}
	}
	if !sawPartial {
		t.Errorf("no non-final partial %q in transcripts %+v (adapter still batch chat.completions?)", partialText, got)
	}
	if !sawFinal {
		t.Errorf("no final %q in transcripts %+v", finalText, got)
	}
	select {
	case <-f.frames:
	default:
		t.Error("fake WS server saw no binary PCM frames (no native WS dial)")
	}
}

// TestRumbleNativeFlushE2E (rumble-native-flush-01, F2): closing the PCM
// channel must send exactly ONE empty binary flush frame; the server's
// final+end reply must then be honored. RED today: no WS, no flush frame.
func TestRumbleNativeFlushE2E(t *testing.T) {
	const finalText = "hello rumble"
	srv, f := newRumbleWSFake(t, rumbleFakeCfg{
		emitAfter: 1 << 30, // events fire only on the empty flush frame
		events: []string{
			`{"type":"final","text":"` + finalText + `"}`,
			`{"type":"end","text":"` + finalText + `"}`,
		},
	})
	defer srv.Close()

	stt := newRumbleNativeSTT(t, srv.URL, providers.ProviderConfig{})
	pcm := make(chan []byte)
	feedRumblePCM(pcm, 8, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := collectRumble(t, ctx, out)

	var sawFinal bool
	for _, tr := range got {
		if tr.Text == finalText && tr.Final {
			sawFinal = true
		}
	}
	if !sawFinal {
		t.Errorf("no final after flush in %+v", got)
	}

	empty := 0
	for {
		select {
		case fr := <-f.frames:
			if len(fr) == 0 {
				empty++
			}
		default:
			goto checked
		}
	}
checked:
	if empty != 1 {
		t.Errorf("empty binary flush frames = %d, want exactly 1 (observed %d frames total)", empty, len(f.frames))
	}
}

// TestRumbleNativeFactoryE2E (rumble-native-factory-01, F3): a native vendor
// must never be wrapped in the client-side PseudoStreamSTT decorator. A
// deliberately large stt_partial_ms would suppress/delay synthetic snapshots,
// yet the server's OWN partial must still arrive promptly, and no legacy
// HTTP chat.completions POST may be made. RED today: batch adapter emits no
// partial and POSTs to the old path.
func TestRumbleNativeFactoryE2E(t *testing.T) {
	const finalText = "native rumble"
	srv, f := newRumbleWSFake(t, rumbleFakeCfg{
		emitAfter: 2,
		events: []string{
			`{"type":"partial","text":"native"}`,
			`{"type":"final","text":"` + finalText + `"}`,
			`{"type":"end","text":"` + finalText + `"}`,
		},
	})
	defer srv.Close()

	stt := newRumbleNativeSTT(t, srv.URL, providers.ProviderConfig{
		PartialIntervalMs: 5000,
	})
	pcm := make(chan []byte)
	feedRumblePCM(pcm, 30, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := collectRumble(t, ctx, out)

	var sawPartial bool
	for _, tr := range got {
		if tr.Text == "native" && !tr.Final {
			sawPartial = true
		}
	}
	if !sawPartial {
		t.Errorf("native partial not surfaced with stt_partial_ms=5000; transcripts=%+v (rumble wrapped in pseudo-stream?)", got)
	}
	select {
	case hit := <-f.httpHits:
		t.Errorf("legacy HTTP request to fake WS server: %s (native vendor must not use chat.completions)", hit)
	default:
	}
}
