package daemon

// Wire-shape tests for the C1 open frame producer (local-leg plan CL1).
// The daemon side must send ONE open line per call, before splicing, with
// the child-native codec (L16@16k, NOT the negotiated wire codec) and CL4
// provider precedence (local-leg config before global; empty stays empty).
// The fake child here is just a dialer: TextServer.claimChild takes the
// first connection as THE child, so Send has a real socket to write to.

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/voice"
)

// fakeChildAt starts w's TextServer, connects a fake child to claim the
// child slot, and returns a line reader over everything the daemon sends.
func fakeChildAt(t *testing.T, w *VoiceWiring, path string) *bufio.Reader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	go func() {
		_ = w.TextServer.Start(ctx)
		close(ready)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("text server socket never appeared at %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("fake child dial %s: %v", path, err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		cancel()
		<-ready
	})
	return bufio.NewReader(conn)
}

// sendWhenChild polls sendOpenFrame until the TextServer has claimed the
// fake child's connection (the accept loop runs asynchronously behind the
// socket file's existence) or the deadline passes.
func sendWhenChild(t *testing.T, w *VoiceWiring, ccid string) error {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := w.sendOpenFrame(ccid)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSendOpenFramePhoneLeg(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	ccid := "cc-open-d"
	// A non-phone-shaped sentinel keeps the wire assertion readable: the
	// tool-output layer masks phone-shaped strings, and a masked literal
	// cannot be compared byte-exactly.
	w.cfg.FromNumber = "from-test"
	from := w.cfg.FromNumber
	w.cfg.TTSProvider = "qwen3"
	w.cfg.TTSVoice = "vivian"
	w.cfg.STTProvider = "deepgram"
	inst := acquireFor(w, ccid) // LegPhone
	to := inst.Target           // whatever the fixture's acquireFor dials

	path := filepath.Join(voice.SocketDir(), "test-text.sock")
	r := fakeChildAt(t, w, path)
	if err := sendWhenChild(t, w, ccid); err != nil {
		t.Fatalf("sendOpenFrame: %v", err)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read open line: %v", err)
	}
	// Global config values land; local/omits stay absent for a phone call.
	want := `{"call_id":"cc-open-d","event":"open","from":"` + from + `","to":"` + to + `","direction":"inbound","codec":"L16","sample_rate":16000,"stt_vendor":"deepgram","tts_vendor":"qwen3","tts_voice":"vivian"}` + "\n"
	if line != want {
		// In-process byte diff: the tool-output layer masks secret-shaped
		// strings, so printing the two lines can hide the real difference.
		t.Fatalf("open line mismatch len got=%d want=%d first_diff_idx=%d", len(line), len(want), firstDiff(line, want))
	}
}

// firstDiff returns the byte index where a and b first differ, or min(len)
// when one is a prefix of the other.
func firstDiff(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func TestSendOpenFrameLocalLegUsesLocalConfig(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	ccid := "cc-open-l"
	w.cfg.STTProvider = "deepgram"
	w.cfg.TTSProvider = "qwen3"
	w.cfg.Local = config.LocalConfig{
		Enabled:     true,
		STTProvider: "whisper",
		TTSProvider: "piper",
		TTSVoice:    "danny-medium",
		BargeInRms:  3000,
	}
	// VoiceWiring holds the Registry interface; AcquireLeg lives on the
	// concrete *voice.CallRegistry the fixture builds (local-leg acquire
	// wiring lands with leaf 07).
	lr, ok := w.Registry.(*voice.CallRegistry)
	if !ok {
		t.Fatalf("fixture registry is %T, want *voice.CallRegistry", w.Registry)
	}
	if _, err := lr.AcquireLeg(ccid, w.cfg.DefaultEmployee, "local:box", voice.LegLocal); err != nil {
		t.Fatalf("acquire leg: %v", err)
	}

	path := filepath.Join(voice.SocketDir(), "test-text.sock")
	r := fakeChildAt(t, w, path)
	if err := sendWhenChild(t, w, ccid); err != nil {
		t.Fatalf("sendOpenFrame: %v", err)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read open line: %v", err)
	}
	// Local-leg config wins over global; local + barge_in_rms present.
	want := `{"call_id":"cc-open-l","event":"open","to":"local:box","direction":"inbound","codec":"L16","sample_rate":16000,"local":true,"stt_vendor":"whisper","tts_vendor":"piper","tts_voice":"danny-medium","barge_in_rms":3000}` + "\n"
	if line != want {
		t.Fatalf("open line:\n got %s\nwant %s", line, want)
	}
}

// TestSendOpenFrameUnknownCcidStillSendsIdentity: no registry instance
// (call raced a release) must not drop the frame - identity + codec still
// let the child build its pipeline; the daemon-side router guard handles
// the missing instance separately.
func TestSendOpenFrameUnknownCcidStillSendsIdentity(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.cfg.FromNumber = "from-test"
	path := filepath.Join(voice.SocketDir(), "test-text.sock")
	r := fakeChildAt(t, w, path)
	if err := sendWhenChild(t, w, "cc-ghost"); err != nil {
		t.Fatalf("sendOpenFrame: %v", err)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read open line: %v", err)
	}
	want := `{"call_id":"cc-ghost","event":"open","from":"from-test","direction":"inbound","codec":"L16","sample_rate":16000}` + "\n"
	if line != want {
		t.Fatalf("open line:\n got %s\nwant %s", line, want)
	}
}
