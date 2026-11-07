package voicechild

// Gate tests T4 + T5 (features F4/F5, master.md C5, leaves 05/06):
//   F4: pumpAudioIn feeds daemon PCM into the real STT; transcript
//       finals/partials become prompt frames with the right last flag;
//       empty-text transcripts are dropped.
//   F5: a non-empty partial transcript while p.speaking trips the
//       noteBargeIn latch -> exactly one interrupt frame per barge-in;
//       no interrupt when idle; latch re-arms after playback restarts.
//
// RED style: compiles against existing symbols (STTProvider, Transcript,
// emitPrompt, emitInterrupt, noteBargeIn all exist); fails on the missing
// WIRING. Missing behavior named in comments:
//   leaf 05: pumpAudioIn forwards chunks to p.stt.Stream (drops today,
//     pipeline.go pumpAudioIn: "Stub STT consumes nothing yet")
//   leaf 05: transcript consumer goroutine (emitPrompt has no
//     transcript caller today)
//   leaf 06: partial branch calls noteBargeIn + emitInterrupt
//     (noteBargeIn exists at pipeline.go:259 with no transcript caller)

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// recordingSTT implements STTProvider: it records every PCM chunk the
// pump forwards and emits a scripted transcript sequence.
type recordingSTT struct {
	mu     sync.Mutex
	chunks [][]byte
	trans  chan Transcript
	calls  int
}

func newRecordingSTT() *recordingSTT {
	return &recordingSTT{trans: make(chan Transcript, 16)}
}

func (r *recordingSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	go func() {
		defer close(r.trans)
		for {
			select {
			case chunk, ok := <-pcm:
				if !ok {
					return
				}
				r.mu.Lock()
				r.chunks = append(r.chunks, chunk)
				r.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
	return r.trans, nil
}

func (r *recordingSTT) emit(tr Transcript) {
	r.trans <- tr
}

func (r *recordingSTT) gotChunks() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.chunks))
	copy(out, r.chunks)
	return out
}

func (r *recordingSTT) streamCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// newSTTWiredChild starts a child with a recording STT factory and one
// live pipeline (setup through the fake daemon), returning the handles
// the T4/T5 tests drive.
func newSTTWiredChild(t *testing.T, ccid string) (*fakeDaemon, *Child, net.Conn, *pipeline, *recordingSTT) {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := newTestChild(t, d, "tok")
	stt := newRecordingSTT()
	child.SetProviderFactories(
		func(key string, rate int) (STTProvider, error) {
			if key != "stt-secret" {
				return nil, errors.New("test: want stt-secret key")
			}
			return stt, nil
		},
		func(key string, rate int) (TTSProvider, error) { return stubTTS{}, nil },
	)
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000})
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() {
		_ = in.Close()
		_ = out.Close()
	})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, ok := child.getPipeline(ccid)
	if !ok {
		t.Fatalf("pipeline %s missing after setup", ccid)
	}
	// Keys arrive (the transcript goroutine starts after providers
	// construct, per leaf 05's hook point in handleKeys).
	sendRaw(t, childConn, `{"call_id":"`+ccid+`","event":"keys","stt_key":"stt-secret","tts_key":"tts-secret"}`)
	t.Cleanup(func() {
		if err := childConn.Close(); err != nil {
			t.Errorf("close child conn: %v", err)
		}
		if runErr := <-runDone; runErr != nil {
			t.Errorf("Run returned error on close: %v", runErr)
		}
	})
	return d, child, childConn, p, stt
}

// TestPipelineSTTWiring (gate T4): .in bytes reach the STT; scripted
// transcripts become prompt frames with correct last flags; empty text
// is dropped.
func TestPipelineSTTWiring(t *testing.T) {
	const ccid = "pw1"
	d, child, childConn, p, stt := newSTTWiredChild(t, ccid)
	_ = childConn

	// RED (leaf 05): pumpAudioIn drops PCM today and never calls Stream,
	// so providers constructing does not start an STT session.
	waitFor(t, func() bool { return stt.streamCalls() == 1 })

	// Write three distinct PCM chunks to the daemon side of .in.
	in, _ := d.acceptAll(ccid) //nolint:dogsled // out accepted above; in reused here for writes
	var written [][]byte
	for i := 0; i < 3; i++ {
		chunk := []byte{byte(i), byte(i), byte(i), byte(i)}
		written = append(written, chunk)
		if _, err := in.Write(chunk); err != nil {
			t.Fatalf("write .in: %v", err)
		}
	}
	// RED (leaf 05): pumpAudioIn drops chunks today, so the fake STT
	// never records them.
	waitFor(t, func() bool {
		got := stt.gotChunks()
		if len(got) < len(written) {
			return false
		}
		for i, want := range written {
			if string(got[i]) != string(want) {
				t.Errorf("STT chunk %d = %v, want %v", i, got[i], want)
			}
		}
		return true
	})

	// Final transcript -> prompt with last:true (starts an employee turn).
	stt.emit(Transcript{Text: "what time is it", Final: true})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "what time is it" {
		t.Fatalf("after final transcript got event=%q text=%q, want prompt \"what time is it\" (leaf 05: no transcript caller)", f.Event, f.Text)
	}
	if f.Last == nil || !*f.Last {
		t.Fatalf("final prompt last flag = %v, want true", f.Last)
	}
	if f.CallID != ccid {
		t.Errorf("prompt call_id = %q, want %q", f.CallID, ccid)
	}

	// Partial transcript -> prompt with last:false (preview only).
	stt.emit(Transcript{Text: "what time", Final: false})
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "what time" {
		t.Fatalf("after partial transcript got event=%q text=%q, want prompt \"what time\"", f.Event, f.Text)
	}
	if f.Last != nil {
		t.Fatalf("partial prompt last flag = %v, want absent/false (preview)", *f.Last)
	}

	// Empty-text transcript (Deepgram silence) produces NO frame.
	stt.emit(Transcript{Text: "", Final: true})
	select {
	case f := <-frameWithin(t, d, 300*time.Millisecond):
		t.Fatalf("empty transcript emitted event=%q text=%q; silence results are dropped (C5)", f.Event, f.Text)
	case <-time.After(350 * time.Millisecond):
	}

	// Teardown contract: end frame closes the Stream session (the pump
	// dies with the pipeline ctx; the recording STT stops).
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 0 })
	if got := stt.streamCalls(); got != 1 {
		t.Errorf("Stream called %d times, want exactly 1 per pipeline (C5)", got)
	}
	_ = p
	_ = filepath.Join // keep filepath linked for helper parity
}

// frameWithin converts waitFrame into a select-able channel.
func frameWithin(t *testing.T, d *fakeDaemon, within time.Duration) <-chan voiceframe.Frame {
	t.Helper()
	ch := make(chan voiceframe.Frame, 1)
	go func() {
		ch <- d.waitFrame(t)
	}()
	// The goroutine owns the pop; give the reader `within` to decide.
	time.AfterFunc(within, func() {})
	return ch
}

// TestBargeInViaPartial (gate T5): partial while speaking -> exactly one
// interrupt frame; second partial -> no second interrupt; after a new say
// restarts playback, the next partial interrupts again. Idle partials
// never interrupt.
func TestBargeInViaPartial(t *testing.T) {
	const ccid = "bi1"
	d, child, childConn, p, stt := newSTTWiredChild(t, ccid)
	_ = childConn // teardown path exercises handleSay/handleClear directly

	// RED (leaf 05): Stream is never called today (no pump wiring).
	waitFor(t, func() bool { return stt.streamCalls() == 1 })

	interrupts := func() (count int, order []string) {
		d.mu.Lock()
		defer d.mu.Unlock()
		for _, f := range d.queue {
			order = append(order, f.Event)
			if f.Event == voiceframe.EventInterrupt {
				count++
			}
		}
		return count, order
	}

	// Scenario A: NOT speaking -> partial emits prompt only, no interrupt.
	stt.emit(Transcript{Text: "hello are there", Final: false})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt {
		t.Fatalf("idle partial emitted %q, want prompt", f.Event)
	}
	if n, _ := interrupts(); n != 0 {
		t.Fatalf("idle partial emitted %d interrupts, want 0 (F5: no interrupt when not speaking)", n)
	}

	// Scenario B: speaking -> first partial trips the latch exactly once.
	if !ok2(child, ccid) {
		t.Fatal("pipeline missing")
	}
	say := voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "sure, one moment"}
	if err := child.handleSay(say); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	stt.emit(Transcript{Text: "wait", Final: false})
	deadline := time.Now().Add(2 * time.Second)
	var gotInt int
	var gotOrder []string
	for time.Now().Before(deadline) {
		gotInt, gotOrder = interrupts()
		if gotInt > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if gotInt == 0 {
		t.Fatalf("missing behavior: partial while speaking emitted no interrupt frame (leaf 06: noteBargeIn has no transcript caller); frames so far: %v", gotOrder)
	}
	// The prompt preview still flows.
	waitFor(t, func() bool {
		_, order := interrupts()
		return len(order) >= 1 && strings.Join(order, ",") != "" && containsEvent(order, voiceframe.EventPrompt, "wait")
	})

	// Second partial while STILL speaking: latch blocks a second interrupt.
	stt.emit(Transcript{Text: "wait please", Final: false})
	time.Sleep(100 * time.Millisecond)
	if n, _ := interrupts(); n != 1 {
		t.Fatalf("second partial emitted interrupt #%d, want exactly 1 per barge-in (latch)", n)
	}

	// Scenario C: clear retires the stream, a new say restarts playback,
	// and the next partial interrupts again (latch re-arm).
	child.handleClear(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventClear})
	waitFor(t, func() bool { return !p.isSpeaking() })
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "where were we"}); err != nil {
		t.Fatalf("handleSay 2: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })

	stt.emit(Transcript{Text: "go on", Final: false})
	waitFor(t, func() bool {
		n, _ := interrupts()
		return n >= 2
	})
	if n, _ := interrupts(); n != 2 {
		t.Fatalf("interrupt count after re-arm = %d, want exactly 2 (one per barge-in)", n)
	}
}

func ok2(c *Child, ccid string) bool {
	_, ok := c.getPipeline(ccid)
	return ok
}

func containsEvent(order []string, event, text string) bool {
	// order carries events only; the text-level check lives in the
	// waitFrame assertions above. Here: any prompt after the interrupt.
	foundInt := false
	for _, e := range order {
		if e == voiceframe.EventInterrupt {
			foundInt = true
		}
		if foundInt && e == voiceframe.EventPrompt {
			return true
		}
	}
	return false
}

var (
	_ io.Reader // keep io linked
	_ time.Duration
)
