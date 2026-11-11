package signalleg

// Bughunt fix-wave pin tests (F1): A1 resampler odd-tail-byte carry, A2
// outbound pump drain rate, A5 Place/Accept failure-path state reaping.
// Fakes only - same rig rules as audio_test.go (net.Pipe socket, real
// AudioPair child side, injectable pace clock).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/signal"
	"github.com/caimlas/meept/internal/voice"
)

// --- A1: resampler odd-tail-byte carry ---

// TestResamplerOddTailByteCarriedAcrossWrites pins A1: a stream Read
// that splits an s16 sample across two Writes must not lose or
// misplace the odd tail byte - the resampler holds it and prepends it
// to the next Write, so a split-fed stream decodes identically to the
// whole stream (every odd split offset is exercised).
func TestResamplerOddTailByteCarriedAcrossWrites(t *testing.T) {
	const samples = 500
	whole := make([]byte, 2*samples)
	for i := range whole {
		whole[i] = byte(i*31 + 7) // deterministic, non-trivial pattern
	}
	ref, err := NewResampler(16000, 16000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	want := ref.Write(whole)
	if len(want) != len(whole) {
		t.Fatalf("whole-stream passthrough: %d bytes, want %d", len(want), len(whole))
	}

	for offset := 1; offset < len(whole); offset += 2 {
		split, err := NewResampler(16000, 16000)
		if err != nil {
			t.Fatalf("NewResampler: %v", err)
		}
		var got []byte
		got = append(got, split.Write(whole[:offset])...)
		got = append(got, split.Write(whole[offset:])...)
		if len(got) != len(whole) {
			t.Fatalf("split at odd offset %d: output %d bytes, want %d (a dropped tail byte misaligns everything after it)", offset, len(got), len(whole))
		}
		for i := range whole {
			if got[i] != whole[i] {
				t.Fatalf("split at odd offset %d: byte %d = %#x, want %#x (stream misaligned)", offset, i, got[i], whole[i])
			}
		}
	}
}

// --- A2: outbound pump drains a realtime-rate producer ---

// TestOutboundAudioDrainsFastProducer pins A2: 100 child chunks (64 kB)
// are pumped to the socket in bounded wall time. The old per-iteration
// 200 ms sleep capped the pump at ~2.9 kB/s (this volume needs ~22 s);
// with the sleep only on empty iterations the drain lands in seconds.
func TestOutboundAudioDrainsFastProducer(t *testing.T) {
	f := newAudioFixture(t, "signal-fast-drain")
	clock := newFakePaceClock()
	// Auto-advance the fake clock: pace waits record but never delay.
	clock.mu.Lock()
	clock.perWait = func() { clock.fireNow <- struct{}{} }
	clock.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runOutboundAudio(ctx, f.pair, f.socket, 16000, "signal-fast-drain", discardLog(), clock.wait)
	}()

	const chunks = 100
	const totalBytes = chunks * 640
	go func() {
		chunk := make([]byte, 640)
		for i := range chunk {
			chunk[i] = byte(i * 3)
		}
		for range chunks {
			if _, err := f.childOut.Write(chunk); err != nil {
				return
			}
		}
	}()

	drained := make(chan int, 1)
	go func() {
		got := 0
		buf := make([]byte, 4096)
		for got < totalBytes {
			_ = f.remote.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, err := f.remote.Read(buf)
			got += n
			if err != nil {
				break
			}
		}
		drained <- got
	}()

	select {
	case got := <-drained:
		if got != totalBytes {
			t.Fatalf("drained %d bytes, want %d", got, totalBytes)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("fast producer not drained within 5s (a per-chunk sleep would need ~%s for %d chunks)",
			time.Duration(chunks)*audioReadDeadline, chunks)
	}

	// Pacing is still honest: every socket write was preceded by a
	// 20 ms pace tick through the injectable seam.
	waits := clock.intervals()
	if len(waits) == 0 {
		t.Fatalf("no pace ticks recorded (pacedWrite bypassed the wait seam)")
	}
	for _, d := range waits {
		if d != audioPace {
			t.Errorf("pace interval = %s, want %s", d, audioPace)
		}
	}

	// Teardown: cancel + close the child end so the pump's read sees
	// EOF and the ctx check exits it; close the pipe reader first.
	_ = f.remote.Close()
	cancel()
	_ = f.childOut.Close()
	_ = f.socket.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) &&
			!errors.Is(err, ErrAudioBackpressure) && !errors.Is(err, io.EOF) {
			t.Errorf("outbound pump ended with %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("outbound pump did not end after teardown")
	}
}

// --- A5: Place/Accept failure paths reap the tracked call state ---

// newFailStubLeg builds a live Leg whose signal-cli stub answers every
// json-rpc request with an ERROR object - StartCall and AcceptCall both
// fail, exercising the Place/Accept error paths (A5).
func newFailStubLeg(t *testing.T) *Leg {
	t.Helper()
	t.Setenv("MEEPT_HOME", shortAudioDir(t))
	if err := os.MkdirAll(voice.SocketDir(), 0o755); err != nil {
		t.Fatalf("sock dir: %v", err)
	}
	rpcLn, err := net.Listen("unix", filepath.Join(shortAudioDir(t), "fail.sock"))
	if err != nil {
		t.Fatalf("stub listen: %v", err)
	}
	t.Cleanup(func() { _ = rpcLn.Close() })
	go func() {
		for {
			conn, aerr := rpcLn.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }() //nolint:errcheck // test stub
				buf := make([]byte, 4096)
				pend := make([]byte, 0, 4096)
				for {
					n, rerr := c.Read(buf)
					if rerr != nil {
						return
					}
					pend = append(pend, buf[:n]...)
					for {
						idx := indexByte(pend, '\n')
						if idx < 0 {
							break
						}
						line := pend[:idx]
						pend = pend[idx+1:]
						var req struct {
							ID     int    `json:"id"`
							Method string `json:"method"`
						}
						if jerr := json.Unmarshal(line, &req); jerr != nil {
							continue
						}
						resp := map[string]any{
							"jsonrpc": "2.0",
							"id":      req.ID,
							"error":   map[string]any{"code": -32000, "message": "stub refusal"},
						}
						b, _ := json.Marshal(resp)      //nolint:errcheck // static shape
						_, _ = c.Write(append(b, '\n')) //nolint:errcheck // test stub
					}
				}
			}(conn)
		}
	}()
	client, err := signal.Dial(rpcLn.Addr().String())
	if err != nil {
		t.Fatalf("signal.Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() }) //nolint:errcheck // test teardown
	leg, lerr := New(Deps{
		Client:     client,
		Registry:   &fakeRegistry{inner: voice.NewRegistry(4)},
		Manager:    voice.NewManager(voice.ManagerConfig{}, nil, nil),
		TextServer: voice.NewTextServer(filepath.Join(shortAudioDir(t), "fail-text.sock"), discardLog()),
		Allowlist:  func(string) bool { return true },
		Logger:     discardLog(),
	})
	if lerr != nil {
		t.Fatalf("New: %v", lerr)
	}
	return leg
}

// assertNoTrackedCallState pins the A5 observable: Live() is empty and
// the l.calls map holds nothing after a failed dial/accept.
func assertNoTrackedCallState(t *testing.T, leg *Leg, attempt int) {
	t.Helper()
	if live := leg.Live(); len(live) != 0 {
		t.Fatalf("attempt #%d: Live() = %v, want empty (failed call leaked into the live set)", attempt, live)
	}
	leg.mu.Lock()
	grown := len(leg.calls)
	leg.mu.Unlock()
	if grown != 0 {
		t.Fatalf("attempt #%d: l.calls holds %d entries, want 0 (callState leak)", attempt, grown)
	}
}

// TestPlaceStartCallFailureReapsCallState pins A5 (Place path): a
// StartCall failure deletes l.calls[ccid] and latches the terminal
// state - Live() stays empty and the map never grows over repeated
// failures.
func TestPlaceStartCallFailureReapsCallState(t *testing.T) {
	leg := newFailStubLeg(t)
	for i := 1; i <= 5; i++ {
		_, err := leg.Place(context.Background(), "emp-1", "eeee5555-2222-3333-4444-555555555555", "")
		if err == nil {
			t.Fatalf("Place #%d: err = nil, want a startCall refusal", i)
		}
		if !errors.Is(err, signal.ErrRPCError) {
			t.Fatalf("Place #%d: err = %v, want a json-rpc error from StartCall (an earlier gate fired instead)", i, err)
		}
		assertNoTrackedCallState(t, leg, i)
	}
}

// TestAcceptCallFailureReapsCallState pins A5 (Accept path): an
// AcceptCall failure deletes l.calls[ccid] and latches the terminal
// state - same no-growth assertion over repeated failures.
func TestAcceptCallFailureReapsCallState(t *testing.T) {
	leg := newFailStubLeg(t)
	for i := 1; i <= 5; i++ {
		_, err := leg.Accept(context.Background(), signal.CallEvent{
			EnvelopeSourceUUID: "ffff6666-2222-3333-4444-555555555555",
			CallID:             "call-accept-fail-" + testItoa(i),
		}, "emp-1")
		if err == nil {
			t.Fatalf("Accept #%d: err = nil, want an acceptCall refusal", i)
		}
		if !errors.Is(err, signal.ErrRPCError) {
			t.Fatalf("Accept #%d: err = %v, want a json-rpc error from AcceptCall (an earlier gate fired instead)", i, err)
		}
		assertNoTrackedCallState(t, leg, i)
	}
}
