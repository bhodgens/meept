package voicechild

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// Self-verification checklist: two ccids in one child, -race clean. Both
// pipelines register from setup frames, both receive say -> first PCM chunk
// on their own .out socket, and end tears down exactly that call.
func TestConcurrentPipelines(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("ca")
	d.listenPair("cb")

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	childConn := d.serveText()

	for _, ccid := range []string{"ca", "cb"} {
		d.sendLine(childConn, voiceframe.Frame{
			CallID: ccid, Event: voiceframe.EventSetup,
			Codec: "L16", SampleRate: 16000,
		})
	}
	inA, outA := d.acceptAll("ca")
	inB, outB := d.acceptAll("cb")
	defer func() { _ = inA.Close() }()
	defer func() { _ = outA.Close() }()
	defer func() { _ = inB.Close() }()
	defer func() { _ = outB.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 2 })

	// say to both calls; each .out must get its own stub audio.
	d.sendLine(childConn, voiceframe.Frame{CallID: "ca", Event: voiceframe.EventSay, Text: "a speech", Last: lastPtr(true)})
	d.sendLine(childConn, voiceframe.Frame{CallID: "cb", Event: voiceframe.EventSay, Text: "b speech", Last: lastPtr(true)})

	type read struct {
		ccid string
		n    int
		buf  []byte
	}
	got := make(chan read, 2)
	readOne := func(ccid string, conn interface {
		Read([]byte) (int, error)
	}) {
		buf := make([]byte, 640)
		go func() {
			n, _ := conn.Read(buf)
			got <- read{ccid: ccid, n: n, buf: buf[:n]}
		}()
	}
	readOne("ca", outA)
	readOne("cb", outB)

	seen := make(map[string]int, 2)
	deadline := time.After(5 * time.Second)
	for len(seen) < 2 {
		select {
		case r := <-got:
			seen[r.ccid] = r.n
		case <-deadline:
			t.Fatalf("timed out waiting for audio on both .out sockets; seen=%v", seen)
		}
	}
	for ccid, n := range seen {
		if n != 640 {
			t.Fatalf("call %s first chunk = %d bytes, want 640", ccid, n)
		}
	}

	// end one call; the other must stay live.
	d.sendLine(childConn, voiceframe.Frame{CallID: "ca", Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	if _, ok := child.getPipeline("cb"); !ok {
		t.Fatalf("call cb pipeline must survive call ca end")
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	if runErr := <-runDone; runErr != nil {
		t.Fatalf("Run returned error on close: %v", runErr)
	}
	_ = filepath.Join // keep path/filepath linked
}
