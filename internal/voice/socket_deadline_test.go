package voice

// socket_deadline_test.go - bounded client transport waits.
//
// AudioPair.WriteToClient / ReadFromMic are called once per 20 ms audio
// chunk from the local splice's pacer goroutine. Before these pins both
// accept-waits were UNBOUNDED: a client that never dialed spun the caller
// at 100 Hz for the life of the call, and a client that connected then
// stopped reading blocked the pacer forever on conn.Write (no deadline).
// The contract now: every wait is bounded, a timeout is the named
// ErrClientWriteTimeout sentinel, and a successful write CLEARS its
// deadline so no later writer inherits a stale bound.

import (
	"bytes"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// setClientAcceptTimeout lowers the accept-wait bound for the duration of a
// test (restored by t.Cleanup).
func setClientAcceptTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := clientAcceptTimeoutValue()
	acceptTimeoutMu.Lock()
	clientAcceptTimeout = d
	acceptTimeoutMu.Unlock()
	t.Cleanup(func() {
		acceptTimeoutMu.Lock()
		clientAcceptTimeout = orig
		acceptTimeoutMu.Unlock()
	})
}

// A client that never dials must fail the pacer call within the bound
// instead of spinning forever.
func TestWriteToClientUnconnectedIsBounded(t *testing.T) {
	setClientAcceptTimeout(t, 60*time.Millisecond)

	dir := shortDir(t)
	p, err := NewAudioPair(dir, "ccid-deadline")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	defer func() { _ = p.Close() }()

	start := time.Now()
	done := make(chan error, 1)
	go func() { _, werr := p.WriteToClient([]byte("pcm")); done <- werr }()

	select {
	case werr := <-done:
		if werr == nil {
			t.Fatalf("write to an unconnected client must error")
		}
		if !errors.Is(werr, ErrClientWriteTimeout) {
			t.Fatalf("unconnected accept timeout must be ErrClientWriteTimeout, got %v", werr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("WriteToClient still blocked after the accept bound (would spin forever)")
	}
	// Bounded means bounded: it must not have waited for the default.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("wait was not bounded by the configured accept timeout: %s", elapsed)
	}
}

// The sibling mic read is bounded the same way.
func TestReadFromMicUnconnectedIsBounded(t *testing.T) {
	setClientAcceptTimeout(t, 60*time.Millisecond)

	dir := shortDir(t)
	p, err := NewAudioPair(dir, "ccid-deadline-mic")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	defer func() { _ = p.Close() }()

	done := make(chan error, 1)
	go func() {
		_, rerr := p.ReadFromMic(make([]byte, 64))
		done <- rerr
	}()
	select {
	case rerr := <-done:
		if !errors.Is(rerr, ErrClientWriteTimeout) {
			t.Fatalf("mic read accept timeout must be ErrClientWriteTimeout, got %v", rerr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ReadFromMic still blocked after the accept bound (would spin forever)")
	}
}

// A client that connects but never reads must hit the write deadline rather
// than parking the pacer goroutine forever.
func TestWriteToClientWedgedReaderIsBounded(t *testing.T) {
	dir := shortDir(t)
	p, err := NewAudioPair(dir, "ccid-wedged")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	defer func() { _ = p.Close() }()

	// Dial .out and never read. The unix socket's kernel buffer fills, so a
	// write past it blocks until the deadline fires.
	client, err := net.Dial("unix", filepath.Join(dir, "ccid-wedged.out"))
	if err != nil {
		t.Fatalf("client dial .out: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Wait for the accept goroutine to store the conn.
	waitConn(t, func() bool {
		p.mu.Lock()
		got := p.out != nil
		p.mu.Unlock()
		return got
	})

	// Fill the socket buffer in the background; the write that cannot fit is
	// the one that must be bounded.
	go func() {
		buf := bytes.Repeat([]byte{0x5A}, 32*1024)
		for {
			if _, werr := client.Write(buf); werr != nil {
				return
			}
		}
	}()
	// Give the filler time to saturate the buffer before the bounded write.
	time.Sleep(50 * time.Millisecond)

	writeDone := make(chan error, 1)
	go func() {
		chunk := bytes.Repeat([]byte{0x11}, 64*1024)
		// Linux unix-socket send buffers are often ~212 KB, larger than
		// a single chunk. Keep writing until WriteToClient actually
		// blocks against the full buffer; only then can the deadline
		// fire. Bounded so a broken path fails instead of hanging.
		deadline := time.Now().Add(clientWriteTimeout + 3*time.Second)
		for time.Now().Before(deadline) {
			n, werr := p.WriteToClient(chunk)
			if werr != nil {
				writeDone <- werr
				return
			}
			if n < len(chunk) {
				continue
			}
		}
		writeDone <- nil
	}()

	select {
	case werr := <-writeDone:
		if werr == nil {
			t.Fatalf("expected a bounded write failure against a non-reading client")
		}
		if !errors.Is(werr, ErrClientWriteTimeout) {
			t.Fatalf("wedged write must be ErrClientWriteTimeout, got %v", werr)
		}
	case <-time.After(clientWriteTimeout + 3*time.Second):
		t.Fatalf("write still blocked past the deadline: the pacer goroutine would park forever")
	}
}

// A successful write must CLEAR the deadline (writeChild's posture): the
// next write on the same conn must not inherit a stale bound.
func TestWriteToClientClearsDeadlineAfterWrite(t *testing.T) {
	dir := shortDir(t)
	p, err := NewAudioPair(dir, "ccid-clear")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	defer func() { _ = p.Close() }()

	client, err := net.Dial("unix", filepath.Join(dir, "ccid-clear.out"))
	if err != nil {
		t.Fatalf("client dial .out: %v", err)
	}
	defer func() { _ = client.Close() }()
	waitConn(t, func() bool {
		p.mu.Lock()
		got := p.out != nil
		p.mu.Unlock()
		return got
	})

	if _, err := p.WriteToClient([]byte("first")); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("client read 1: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("payload = %q, want %q", got, "first")
	}

	// The deadline was cleared on the daemon side; a second write must still
	// succeed promptly (a stale deadline would have expired by now if it had
	// been inherited as a far-future bound, and a past one would error).
	if _, err := p.WriteToClient([]byte("2nd")); err != nil {
		t.Fatalf("write 2 after a successful write 1 (stale deadline): %v", err)
	}
	second := make([]byte, 3)
	if _, err := io.ReadFull(client, second); err != nil {
		t.Fatalf("client read 2: %v", err)
	}
	if string(second) != "2nd" {
		t.Fatalf("second payload = %q, want %q", second, "2nd")
	}
}

// Close still wins the race against the accept bound: teardown never has to
// wait out the deadline.
func TestWriteToClientCloseEndsWaitImmediately(t *testing.T) {
	setClientAcceptTimeout(t, time.Hour) // the wait would otherwise be very long

	dir := shortDir(t)
	p, err := NewAudioPair(dir, "ccid-close-fast")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, werr := p.WriteToClient([]byte("x")); done <- werr }()

	time.Sleep(50 * time.Millisecond)
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Close must end the accept wait immediately, not wait out the bound")
	}
}
