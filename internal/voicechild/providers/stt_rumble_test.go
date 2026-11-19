package providers

// Unit gate tests (leaf 00T) for the NATIVE Rumble WebSocket streaming
// protocol. These REPLACE the stale chat.completions / base64-WAV tests.
//
// Wire contract (verified from the live server source):
//   - dial ws://<base>/v1/audio/transcriptions/stream (cfg.BaseURL is http://
//     and upgraded to ws:// by the shared dialURL helper)
//   - client sends raw s16le mono 16 kHz PCM as BINARY frames; flush = an
//     empty binary frame (or text "end")
//   - server replies with JSON text frames: type partial / final / end
//   - silence yields only {"type":"end","text":""}
//   - keyless: NO Authorization header, no base64, no WAV body.
//
// RED against the current batch (chat.completions POST) adapter: the fake WS
// server is never reached and the transcripts do not match.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/coder/websocket"
)

const (
	rumbleStreamPath = "/v1/audio/transcriptions/stream"

	evPartial1 = `{"type":"partial","text":"Yesterday it was"}`
	evFinal1   = `{"type":"final","text":"Yesterday it was"}`
	evPartial2 = `{"type":"partial","text":"30 degrees"}`
	evFinal2   = `{"type":"final","text":"30 degrees"}`
	evEndFull  = `{"type":"end","text":"Yesterday it was 30 degrees"}`
	evEndEmpty = `{"type":"end","text":""}`
)

// rumbleFlusher is the barge-in seam the native adapter must expose on the
// provider returned by NewSTT. The base STTProvider interface has no such
// method, so the test probes it via a narrow interface (no concrete-type
// dependency).
type rumbleFlusher interface {
	FlushNow()
}

// rumbleFake is an httptest WebSocket server speaking the native Rumble
// protocol. It records the handshake path/auth and counts binary frames,
// and calls onBinary(empty, size) for each binary frame to decide which JSON
// text events (if any) to reply with.
type rumbleFake struct {
	srv        *httptest.Server
	mu         sync.Mutex
	gotAuth    string
	gotPath    string
	frames     int // non-empty binary PCM frames
	empties    int // empty binary flush frames
	sawBin     chan struct{}
	sawBinOnce sync.Once
	onBinary   func(empty bool, n int) []string
}

func newRumbleFake(t *testing.T, onBinary func(empty bool, n int) []string) *rumbleFake {
	t.Helper()
	f := &rumbleFake{onBinary: onBinary, sawBin: make(chan struct{})}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		reqPath := r.URL.Path
		f.mu.Lock()
		f.gotAuth = authHeader
		f.gotPath = reqPath
		f.mu.Unlock()

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()
		for {
			mt, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if mt != websocket.MessageBinary {
				continue
			}
			empty := len(data) == 0
			f.mu.Lock()
			if empty {
				f.empties++
			} else {
				f.frames++
			}
			f.mu.Unlock()
			if !empty {
				f.sawBinOnce.Do(func() { close(f.sawBin) })
			}
			if f.onBinary != nil {
				for _, ev := range f.onBinary(empty, len(data)) {
					if werr := conn.Write(ctx, websocket.MessageText, []byte(ev)); werr != nil {
						return
					}
				}
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// baseURL is an http:// httptest URL rewritten to ws://; dialURL (shared
// with the Deepgram adapter) appends rumbleStreamPath on the dial.
func (f *rumbleFake) baseURL() string {
	return strings.Replace(f.srv.URL, "http", "ws", 1)
}

func (f *rumbleFake) snapshot() (auth, path string, frames, empties int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gotAuth, f.gotPath, f.frames, f.empties
}

// drainRumble reads until the transcript channel closes or the timeout fires.
func drainRumble(t *testing.T, out <-chan voicechild.Transcript, timeout time.Duration) []voicechild.Transcript {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var got []voicechild.Transcript
	for {
		select {
		case tr, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, tr)
		case <-timer.C:
			t.Fatalf("timed out after %s waiting for transcript stream to close", timeout)
		}
	}
}

// runRumbleNative builds the STT through the real factory, starts a stream,
// optionally waits for the first PCM frame then kicks FlushNow, and drains.
func runRumbleNative(
	t *testing.T,
	f *rumbleFake,
	chunks [][]byte,
	closePCM bool,
	kick func(voicechild.STTProvider),
) []voicechild.Transcript {
	t.Helper()
	stt, err := NewSTT(ProviderConfig{
		Vendor: "rumble", Model: "Reson8/Rumble-2-Flash",
		BaseURL: f.baseURL(), SampleRate: 16000,
	}, "")
	if err != nil {
		t.Fatalf("NewSTT(rumble): %v", err)
	}

	pcm := make(chan []byte, len(chunks)+1)
	for _, c := range chunks {
		pcm <- c
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if kick != nil {
		select {
		case <-f.sawBin:
		case <-ctx.Done():
			t.Fatal("server never saw a binary PCM frame before FlushNow")
		}
		kick(stt)
	}
	if closePCM {
		close(pcm)
	}
	return drainRumble(t, out, 3*time.Second)
}

// T1 / F1: partial -> {Final:false}, final -> {Final:true}, one per event in
// order. The trailing `end` carries the full text already covered by the two
// finals, so it MUST NOT emit a duplicate final. Handshake is keyless.
func TestRumbleNativeStreamEvents(t *testing.T) {
	var once sync.Once
	f := newRumbleFake(t, func(empty bool, n int) []string {
		var events []string
		once.Do(func() {
			if !empty && n > 0 {
				events = []string{evPartial1, evFinal1, evPartial2, evFinal2, evEndFull}
			}
		})
		return events
	})

	got := runRumbleNative(t, f, [][]byte{
		pcmS16LE(160), pcmS16LE(160), pcmS16LE(160),
	}, true, nil)

	want := []voicechild.Transcript{
		{Text: "Yesterday it was", Final: false},
		{Text: "Yesterday it was", Final: true},
		{Text: "30 degrees", Final: false},
		{Text: "30 degrees", Final: true},
	}
	if len(got) != len(want) {
		t.Fatalf("transcript count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("transcript[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	auth, path, frames, _ := f.snapshot()
	if auth != "" {
		t.Errorf("keyless Rumble sent Authorization header %q", auth)
	}
	if path != rumbleStreamPath {
		t.Errorf("handshake path = %q, want %q", path, rumbleStreamPath)
	}
	if frames == 0 {
		t.Error("server saw no binary PCM frames (base64/WAV body is not the native protocol)")
	}
}

// T2 / F2: the server must receive exactly one empty binary flush frame both
// when the caller closes pcm and when FlushNow() kicks the pump early.
func TestRumbleNativeFlush(t *testing.T) {
	cases := []struct {
		name     string
		closePCM bool
		kick     bool
	}{
		{name: "pcm close", closePCM: true},
		{name: "FlushNow", kick: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRumbleFake(t, func(empty bool, n int) []string {
				if empty {
					return []string{evEndEmpty}
				}
				return nil
			})
			var kickFn func(voicechild.STTProvider)
			if tc.kick {
				kickFn = func(stt voicechild.STTProvider) {
					fl, ok := stt.(rumbleFlusher)
					if !ok {
						t.Fatalf("STT %T does not expose FlushNow()", stt)
					}
					fl.FlushNow()
				}
			}
			_ = runRumbleNative(t, f, [][]byte{pcmS16LE(160)}, tc.closePCM, kickFn)

			_, path, _, empties := f.snapshot()
			if path != rumbleStreamPath {
				t.Errorf("handshake path = %q, want %q", path, rumbleStreamPath)
			}
			if empties < 1 {
				t.Errorf("server saw %d empty binary flush frames, want >=1", empties)
			}
		})
	}
}

// T4 / F4: silence produces only {"type":"end","text":""}. No partial, no
// final, and the transcript channel closes cleanly.
func TestRumbleNativeSilence(t *testing.T) {
	var mu sync.Mutex
	var emitted bool
	f := newRumbleFake(t, func(empty bool, n int) []string {
		mu.Lock()
		defer mu.Unlock()
		if empty && !emitted {
			emitted = true
			return []string{evEndEmpty}
		}
		return nil
	})

	got := runRumbleNative(t, f, [][]byte{make([]byte, 640)}, true, nil)
	if len(got) != 0 {
		t.Fatalf("silence produced %d transcripts %+v, want zero", len(got), got)
	}
	auth, path, _, _ := f.snapshot()
	if auth != "" {
		t.Errorf("keyless Rumble sent Authorization header %q", auth)
	}
	if path != rumbleStreamPath {
		t.Errorf("handshake path = %q, want %q", path, rumbleStreamPath)
	}
}
