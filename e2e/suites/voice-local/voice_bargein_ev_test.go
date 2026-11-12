//go:build e2e

// voice_bargein_ev_test.go - bughunt 2026-11-11 defect 1 (the ~750 ms
// ghost tail): the daemon-side barge-in CLEAR LANE proven hermetically
// over a real local call. voice.local.open must return ev_path; a
// client dialed onto that listener must receive {"event":"clear"} as
// ONE JSON line, latency-bounded, when the child's C1 interrupt frame
// latches the local splice (VoiceWiring.routeFrame -> LatchBargeIn ->
// ClientEventsFor(ccid).Emit). This is the audio-plane edge the CLI's
// drainLiveSpeaker consumes to sink.Clear(); the client-side silence
// (sink.Clear wiring + the EOF-after-clear drop) is pinned by
// cmd/meept/voice_bargein_test.go unit seams - no real audio device in
// CI.

package voicelocal

import (
	"bufio"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

func TestVoiceLocalBargeInClientClear(t *testing.T) {
	_, modelDir := shimPath(t)
	modelPath := writePiperModel(t, modelDir)
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootLocalStack(t, fake, modelPath)

	const (
		employee = "emp-local-evclear"
		label    = "evclearbox"
	)
	rpcCreateEmployee(t, s, employee)

	// 1. Real local open: the result must carry the ev listener path
	// next to the pair paths. An old-daemon-shaped result fails the pin
	// loudly - the RPC field is the lane's only advertisement.
	open := rpcOpenLocalCall(t, s, employee, label)
	ccid, _ := open["call_id"].(string)
	evPath, _ := open["ev_path"].(string)
	if evPath == "" {
		t.Fatalf("voice.local.open returned no ev_path (barge-in clear lane missing): %v", open)
	}
	if wantSuffix := ccid + ".c.ev"; !strings.HasSuffix(evPath, wantSuffix) {
		t.Fatalf("ev_path %q does not carry the client-lane naming convention %q", evPath, wantSuffix)
	}
	if len(evPath) >= 103 {
		t.Fatalf("ev_path %s exceeds the sun_path limit", evPath)
	}
	if _, err := os.Stat(evPath); err != nil {
		t.Fatalf("ev listener socket file missing: %v", err)
	}

	// 2. Join the leg like a live client, then dial the ev lane BEFORE
	// any interrupt can land. Two ordering facts are load-bearing:
	//   - the splice's join gate waits for the client's FIRST mic byte
	//     (10 s grace, then ErrNoClient tears the call down): write one
	//     silent chunk so the lane is tested on a stable live call, not
	//     one racing its own reaper. The child's whisper pump sees the
	//     silence and emits nothing (no final prompt, no turn).
	//   - the daemon's Emit DROPS when no client is attached (the lane
	//     is live-only like the monitor tap): the ev dial therefore
	//     precedes the interrupt, with a settle beat so the accept
	//     goroutine has stored the conn before the latch can fire.
	client := dialLocalClient(t, open)
	go client.speakMic()
	if _, err := client.mic.Write(make([]byte, 640)); err != nil {
		t.Fatalf("client mic silence: %v", err)
	}

	evConn := dialEvLane(t, evPath)
	defer evConn.Close() //nolint:errcheck // teardown
	time.Sleep(200 * time.Millisecond)

	// 3. The fake child's control socket (local-only open spawns the
	// child - same observable as TestVoiceLocalSpawnsChildWithoutPhone)
	// up + attached, so the suite can drive the C1 interrupt frame.
	awaitCtrlSocket(t, ctrlPath)
	fake.connectChild(ctrlPath, 15*time.Second)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)

	// 4. Interrupt -> clear line. The latency assertion is bounded at
	// 1 s from the ctrl command: the true path is child C1 frame ->
	// routeFrame latch -> one unix write, all on the frame-router
	// goroutine. Anything slower means the emit rode a queue it must
	// NOT be on (the whole point of emitting at the audio-plane edge
	// instead of the talk loop). The 10 s read deadline only absorbs
	// CI scheduling noise for the DELIVERY (we still fail if it arrives
	// past the bound).
	t0 := time.Now()
	fake.childCommand(ctrlMsg{Ctrl: "interrupt", CallID: ccid})
	_ = evConn.SetReadDeadline(t0.Add(10 * time.Second))
	line, err := bufio.NewReader(evConn).ReadString('\n')
	latency := time.Since(t0)
	if err != nil {
		t.Fatalf("read clear line from ev lane %s: %v", evPath, err)
	}
	if line != "{\"event\":\"clear\"}\n" {
		t.Fatalf("ev line = %q, want byte-exact {\"event\":\"clear\"}\\n", line)
	}
	if latency > time.Second {
		t.Fatalf("clear latency %s > 1s: the emit must ride the frame-router goroutine, not the talk-loop queue", latency)
	}

	// The splice side latched too (the same transition the emit rides):
	// the log line proves LatchBargeIn returned true exactly once for
	// this barge-in (one clear line - the reader saw it, no second line
	// may arrive before EOF).
	awaitLogLine(t, s, "voice: local barge-in", 10*time.Second)

	// 5. Teardown: end the call through the RPC; the release fan-out +
	// lane close must complete and remove the socket file.
	endClient := harness.DialRPC(t, s.SocketPath)
	end := endClient.CallResult("voice.local.end", map[string]any{"call_id": ccid})
	if end["ok"] != true {
		t.Fatalf("voice.local.end result = %v, want ok:true", end)
	}
	awaitLogLine(t, s, "voice call ended", 15*time.Second)

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(evPath); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ev socket file %s survived voice.local.end", evPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// dialEvLane dials the daemon's ev listener with the bounded retry the
// pair dial uses (the listener file appears at bind() a beat before
// listen() finishes).
func dialEvLane(t *testing.T, evPath string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("unix", evPath)
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("ev lane %s dial: %v", evPath, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// awaitCtrlSocket polls until the fake child's control socket exists
// (local-only open must spawn it - the same bounded poll the spawn test
// inlines, extracted for the second caller).
func awaitCtrlSocket(t *testing.T, ctrlPath string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(ctrlPath); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child control socket %s never appeared after a local-only open", ctrlPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
