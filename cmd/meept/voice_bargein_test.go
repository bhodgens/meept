package main

// voice_bargein_test.go - bughunt 2026-11-11 defect 1 (the ~750 ms
// ghost tail): unit pins on the CLI live lane for the daemon->client
// ev control socket contract.
//
//   a. a clear token silences the speakers: drainLiveSpeaker's select
//      arm calls sink.Clear() -> the fake player's Clear (the sink's
//      sole-owner goroutine does it, never the ev reader), and the
//      buffered backlog is dropped.
//   b. EOF-after-clear DROPS the pending tail (the abort-race bytes
//      must never replay post-hangup) while EOF-without-clear still
//      FLUSHES it (the existing TestDrainLiveSpeakerFlushesTailOnEOF
//      pins the second half; a post-clear bound handoff re-arms the
//      flush - the resumed-stream hangup path).
//   c. old-daemon shape: no EvPath on the open result = no ev dial,
//      nil clears channel, behavior byte-identical to the pre-fix drain.
//
// Seam style mirrors voice_playout_test.go: the fake player records
// ordered events, the socket reader is a channel-driven controllable
// io.Reader so clear/flush ordering is deterministic (every step waits
// on an observable fake-player event before the next input, so the
// loop's select never races an undecided order).

import (
	"bytes"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

// bargePlayer is a concurrency-safe ordered fake: Play appends
// "play:<n>", Clear appends "clear", Close appends "close".
type bargePlayer struct {
	mu     sync.Mutex
	events []string
	err    error
}

func (p *bargePlayer) Play(burst []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, "play:"+strconv.Itoa(len(burst)))
	return nil
}
func (p *bargePlayer) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, "clear")
}
func (p *bargePlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, "close")
}

func (p *bargePlayer) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

// waitEvents blocks until the fake player's log has at least n events
// (or fails after 5 s). Waiting between inputs is what makes the
// drain's select order deterministic: with no queued read, the clear
// token is the ONLY ready arm.
func (p *bargePlayer) waitEvents(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if len(p.snapshot()) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake player reached %d events, want %d: %v", len(p.snapshot()), n, p.snapshot())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// ctrlReader is a controllable io.Reader: each Read pops one scripted
// result; an empty script blocks until the test feeds or ends it.
type ctrlReader struct {
	mu     sync.Mutex
	cond   *sync.Cond
	closed bool
	err    error
	data   [][]byte
}

func newCtrlReader() *ctrlReader {
	r := &ctrlReader{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *ctrlReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	for len(r.data) == 0 && !r.closed {
		// sync.Cond.Wait REQUIRES the mutex held and atomically releases
		// it while blocked - there is no I/O under the lock here (the
		// mutexio analyzer has no Cond special-case; false positive).
		r.cond.Wait() //nolint:mutexio // cond.Wait contract: must hold L, releases it internally
	}
	if len(r.data) > 0 {
		d := r.data[0]
		r.data = r.data[1:]
		r.mu.Unlock()
		return copy(p, d), nil
	}
	err := r.err
	if err == nil {
		err = io.EOF
	}
	r.mu.Unlock()
	return 0, err
}

func (r *ctrlReader) feed(b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, append([]byte(nil), b...))
	r.cond.Broadcast()
}

func (r *ctrlReader) end(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
	r.closed = true
	r.cond.Broadcast()
}

// feedChunks emits n paced 640 B chunks.
func feedChunks(n int) []byte {
	return bytes.Repeat([]byte{0x5A}, n*liveSpeakerReadChunk)
}

// splitChunks feeds a paced stream one chunk per Read so the drain
// observes per-arrival backlog growth (the bound flush lands on the
// exact chunk that crosses liveMaxPendingAudioBytes).
func splitChunks(r *ctrlReader, data []byte) {
	for len(data) > 0 {
		n := min(liveSpeakerReadChunk, len(data))
		r.feed(data[:n])
		data = data[n:]
	}
}

// ---------------------------------------------------------------------------
// (a)+(b) clear -> sink.Clear + EOF-after-clear drop
// ---------------------------------------------------------------------------

// The test rhythm everywhere in this file: feed bytes, push the clear
// token, then WAIT on the race-free fake-player event before the next
// step. Waiting for the "clear" event before sending EOF is load-
// bearing: the drain's select tie-breaks randomly between a ready
// token and a ready read, so the EOF branch must never be reachable
// while the token is still queued (that would legitimately Flush,
// since no clear had been *processed* yet). Total bytes below the
// bound guarantee no mid-stream handoff can race the token.
func TestDrainLiveSpeakerClearSilencesAndDropsEOF(t *testing.T) {
	t.Parallel()
	fp := &bargePlayer{}
	sink := voice.NewSpeakerSink(fp, time.Hour)
	done := make(chan error, 1)
	clears := make(chan struct{}, 4)
	r := newCtrlReader()

	go drainLiveSpeaker(r, sink, done, clears)

	// 1. under-bound backlog buffered, then 2. the latch token. 6+3
	// chunks total = 5760 B < the 12000 B bound, so NO handoff is
	// possible anywhere in this test: every byte of the interrupted
	// stream must die via Clear, never reach the player.
	splitChunks(r, feedChunks(6))
	clears <- struct{}{}
	fp.waitEvents(t, 1)
	splitChunks(r, feedChunks(3))
	// 3. more abort-race bytes land AFTER the latch (the write-
	// deadline in-flight tail), then EOF: the tail is DROPPED, never
	// flushed post-hangup.
	r.end(nil)
	if err := <-done; err != nil {
		t.Fatalf("drain: %v", err)
	}
	// Race-free post-return assertions (the drain has returned; the
	// sink is no longer written). The exact Clear COUNT is
	// interleaving-dependent (the EOF drop is also a Clear), so the
	// pinned contract is the absence of Play plus the Clear first.
	if sink.Pending() != 0 {
		t.Fatalf("EOF-after-clear must drop the tail: %d B pending at exit", sink.Pending())
	}
	got := fp.snapshot()
	if len(got) == 0 || got[0] != "clear" {
		t.Fatalf("events = %v, want the barge-in Clear first", got)
	}
	for _, e := range got {
		if len(e) >= 5 && e[:5] == "play:" {
			t.Fatalf("post-clear EOF must NOT flush/play: events = %v", got)
		}
	}
}

func TestDrainLiveSpeakerPostClearHandoffRearmsEOF(t *testing.T) {
	t.Parallel()
	fp := &bargePlayer{}
	sink := voice.NewSpeakerSink(fp, time.Hour)
	done := make(chan error, 1)
	clears := make(chan struct{}, 4)
	r := newCtrlReader()

	go drainLiveSpeaker(r, sink, done, clears)

	// Burst 1 crosses the bound (19 chunks = 12160 B >= 12000 B) -> a
	// handoff happens (the resumed stream's audio is now PLAYING, not
	// buffered).
	splitChunks(r, feedChunks(19))
	fp.waitEvents(t, 1)

	// Latch mid-play: clear pauses the live burst + drops the backlog.
	// The next input is gated on the "clear" event landing, so the
	// sawClear flag is provably set before any post-clear byte.
	clears <- struct{}{}
	fp.waitEvents(t, 2)
	got := fp.snapshot()
	if got[0] != "play:12160" || got[1] != "clear" {
		t.Fatalf("events = %v, want [play:12160 clear] first", got)
	}

	// The say resumes after the latch: enough fresh audio to cross the
	// bound again. That second handoff is the "new utterance is
	// playing" proof -> the sawClear flag must RE-ARM the normal EOF
	// flush.
	splitChunks(r, feedChunks(19))
	fp.waitEvents(t, 3)

	// A final under-bound tail + EOF: this tail belongs to the RESUMED
	// stream and must play out (the no-barge hangup path).
	splitChunks(r, feedChunks(2))
	r.end(nil)
	if err := <-done; err != nil {
		t.Fatalf("drain: %v", err)
	}
	want := []string{"play:12160", "clear", "play:12160", "play:1280"}
	if got := fp.snapshot(); len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("events = %v, want %v", got, want)
			}
		}
	}
}

// The BOUND-FLUSH re-arm (pinned by TestDrainLiveSpeakerPostClearHandoff-
// RearmsEOF above) is deliberately the ONLY "new utterance is playing"
// proof. This pins the conservative other half of the documented
// tradeoff: post-clear audio that NEVER crosses the handoff bound
// (a resumed reply shorter than 375 ms whose call then hits EOF) is
// dropped, not played - the CLI cannot distinguish a short resume from
// latch-race residue, and the defect class this fix closes (playing
// ABORTED audio post-hangup) wins over the bounded <=375 ms tail loss.
// The normal (no-barge-in) hangup path is unaffected: sawClear is only
// ever set by an ev-clear token.
func TestDrainLiveSpeakerShortPostClearTailDropsAtEOF(t *testing.T) {
	t.Parallel()
	fp := &bargePlayer{}
	sink := voice.NewSpeakerSink(fp, time.Hour)
	done := make(chan error, 1)
	clears := make(chan struct{}, 4)
	r := newCtrlReader()

	go drainLiveSpeaker(r, sink, done, clears)

	splitChunks(r, feedChunks(3))
	clears <- struct{}{}
	fp.waitEvents(t, 1) // the latch Clear consumed the backlog
	time.Sleep(voice.SinkGap + 50*time.Millisecond)
	// Post-clear audio that stays UNDER the bound (2 chunks = 1280 B
	// < 12000 B), then EOF: dropped by the sawClear branch.
	splitChunks(r, feedChunks(2))
	r.end(nil)
	if err := <-done; err != nil {
		t.Fatalf("drain: %v", err)
	}
	got := fp.snapshot()
	// The first event is the latch Clear; a second may come from the
	// EOF drop path (sawClear -> Clear). NO play: event may ever reach
	// the player here.
	for _, e := range got {
		if len(e) >= 5 && e[:5] == "play:" {
			t.Fatalf("sub-bound post-clear tail must never play: events = %v", got)
		}
	}
	if len(got) == 0 || got[0] != "clear" {
		t.Fatalf("events = %v, want the barge-in Clear first", got)
	}
	if sink.Pending() != 0 {
		t.Fatalf("EOF-after-clear must Clear(): %d B pending at exit", sink.Pending())
	}
}

// (b, second pin) EOF WITHOUT any clear still flushes the under-bound
// tail - the existing TestDrainLiveSpeakerFlushesTailOnEOF covers the
// nil-channel (no ev lane) shape; this pins the SAME behavior on a LIVE
// clears channel that simply never received a token (a call with no
// barge-in must never change).
func TestDrainLiveSpeakerNoClearOnLiveChannelFlushes(t *testing.T) {
	t.Parallel()
	fp := &bargePlayer{}
	sink := voice.NewSpeakerSink(fp, time.Hour)
	done := make(chan error, 1)
	clears := make(chan struct{}, 4)
	r := newCtrlReader()

	go drainLiveSpeaker(r, sink, done, clears)
	splitChunks(r, feedChunks(3))
	r.end(nil)
	if err := <-done; err != nil {
		t.Fatalf("drain: %v", err)
	}
	got := fp.snapshot()
	if len(got) != 1 || got[0] != "play:1920" {
		t.Fatalf("events = %v, want the EOF-flushed tail only", got)
	}
}

// ---------------------------------------------------------------------------
// (c) old-daemon shape + the ev reader itself
// ---------------------------------------------------------------------------

func TestSetupLiveSpeakerOldDaemonNoEvPath(t *testing.T) {
	t.Parallel()
	seq := &playSeq{}
	var feed net.Conn
	open := voiceLocalOpenResult{CallID: "local-olddaemon", OutPath: "/no/such/sock"}

	lspk, err := setupLiveSpeaker(open, "secretary", "",
		func() voice.Player { return &seqPlayer{seq: seq} },
		fakeSpeakerDial(seq, &feed))
	if err != nil {
		t.Fatalf("setupLiveSpeaker: %v", err)
	}
	defer teardownLiveSpeaker(lspk, closedChan())
	if lspk.clears != nil || lspk.evConn != nil {
		t.Fatal("no EvPath must mean NO ev lane (old daemon = today's behavior verbatim)")
	}
	events, _, _ := seq.snapshot()
	dials := 0
	for _, e := range events {
		if len(e) >= 5 && e[:5] == "dial:" {
			dials++
		}
	}
	if dials != 1 {
		t.Fatalf("dials = %d, want exactly 1 (the .out socket; no ev dial for an absent path)", dials)
	}
}

func TestSetupLiveSpeakerEvDialFailureDegrades(t *testing.T) {
	t.Parallel()
	seq := &playSeq{}
	var feed net.Conn
	open := voiceLocalOpenResult{CallID: "local-evfail", OutPath: "/no/such/sock", EvPath: "/no/such/ev"}

	lspk, err := setupLiveSpeaker(open, "secretary", "",
		func() voice.Player { return &seqPlayer{seq: seq} },
		func(network, addr string) (net.Conn, error) {
			if addr == open.EvPath {
				return nil, errors.New("ev lane gone")
			}
			return fakeSpeakerDial(seq, &feed)(network, addr)
		})
	if err != nil {
		t.Fatalf("an ev dial failure must NEVER fail the call setup: %v", err)
	}
	defer teardownLiveSpeaker(lspk, closedChan())
	if lspk.clears != nil || lspk.evConn != nil {
		t.Fatal("a failed ev dial must degrade to the no-lane shape")
	}
}

func TestSetupLiveSpeakerDialsEvWhenPresent(t *testing.T) {
	t.Parallel()
	// A REAL listener stands in for the daemon's <ccid>.c.ev socket so
	// the whole client-side lane (dial -> clientClearReader -> token ->
	// drain select -> sink.Clear -> fake player Clear) runs.
	dir := t.TempDir()
	evPath := filepath.Join(dir, "local-ev1.c.ev")
	ln, err := net.Listen("unix", evPath)
	if err != nil {
		t.Skipf("unix listener unavailable: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close() //nolint:errcheck // test conn
				// Mirror the daemon's byte shape: unknown kind first
				// (additive protocol: MUST be ignored, not fatal),
				// then the clear line the CLI consumes.
				_, _ = c.Write([]byte("{\"event\":\"some-future-kind\"}\n"))
				_, _ = c.Write([]byte("{\"event\":\"clear\"}\n"))
				time.Sleep(50 * time.Millisecond)
			}(conn)
		}
	}()

	seq := &playSeq{}
	var feed net.Conn
	open := voiceLocalOpenResult{CallID: "local-ev1", OutPath: "/no/such/sock", EvPath: evPath}

	lspk, err := setupLiveSpeaker(open, "secretary", "",
		func() voice.Player { return &seqPlayer{seq: seq} },
		func(network, addr string) (net.Conn, error) {
			if addr == evPath {
				return net.Dial(network, addr) // the REAL ev listener
			}
			return fakeSpeakerDial(seq, &feed)(network, addr)
		})
	if err != nil {
		t.Fatalf("setupLiveSpeaker: %v", err)
	}
	if lspk.clears == nil || lspk.evConn == nil {
		t.Fatal("EvPath present must dial + arm the ev lane")
	}
	defer teardownLiveSpeaker(lspk, closedChan())

	select {
	case <-lspk.clears:
	case <-time.After(5 * time.Second):
		t.Fatal("clientClearReader never forwarded the daemon's clear line as a token")
	}
}
