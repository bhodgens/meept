package voice

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// dialText connects a fake child to the server socket path and returns the
// connection plus a line reader over what the daemon sends.
func dialText(t *testing.T, path string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("child dial %s: %v", path, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, bufio.NewReader(conn)
}

// waitConn waits until fn reports the expected state, bounding the wait so
// a regression fails the test instead of hanging it.
func waitConn(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not reached within 2s")
}

// armFastAuth lowers the start-token read deadline for tests so the
// timeout-refusal case fails fast instead of waiting the production 5s.
// Call before the dial whose first line is expected to stall.
func (s *TextServer) armFastAuth() {
	s.mu.Lock()
	s.authTimeout = 200 * time.Millisecond
	s.mu.Unlock()
}

// shortDir creates a temp dir with a SHORT path. Unix socket paths are
// limited to ~104 bytes on macOS; t.TempDir() prefixes the test name, so
// long test names plus a socket file name can exceed the limit and make
// net.Listen fail (observed: only the longest-named test failed). Tests
// that bind real sockets must use this instead of t.TempDir.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "vs3")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// T3: listen, child connects, daemon Send arrives as an exact JSON line.
func TestTextServerSendReceive(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	var reader *bufio.Reader
	waitConn(t, func() bool {
		// The socket file appears once listening; dial then. The daemon
		// accepts asynchronously, so Send may still see "no child" for a
		// few milliseconds after the dial succeeds - poll until it lands.
		if _, err := os.Stat(path); err != nil {
			return false
		}
		var child net.Conn
		child, reader = dialText(t, path)
		_ = child
		return true
	})

	waitConn(t, func() bool {
		return srv.Send(Frame{CallID: "ccid-9", Event: EventSay, Text: "hi"}) == nil
	})
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("child read: %v", err)
	}
	want := `{"call_id":"ccid-9","event":"say","text":"hi"}` + "\n"
	if line != want {
		t.Fatalf("child read %q, want %q", line, want)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// T3: inbound child frames reach the handler with decoded fields.
func TestTextServerHandler(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	frames := make(chan Frame, 8)
	srv.SetHandler(func(f Frame) { frames <- f })

	var child net.Conn
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		child, _ = dialText(t, path)
		return true
	})

	_, err := child.Write([]byte(`{"call_id":"c1","event":"prompt","text":"partial"}` + "\n" +
		`{"call_id":"c1","event":"dtmf","text":"1"}` + "\n"))
	if err != nil {
		t.Fatalf("child write: %v", err)
	}

	got := make([]Frame, 0, 2)
	for len(got) < 2 {
		select {
		case f := <-frames:
			got = append(got, f)
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for frames, got %d", len(got))
		}
	}
	if got[0].Event != EventPrompt || got[0].Text != "partial" || got[0].CallID != "c1" {
		t.Fatalf("frame 0 = %+v", got[0])
	}
	// DTMF: decoded no-op, never an error, handler decides.
	if got[1].Event != "dtmf" {
		t.Fatalf("frame 1 = %+v, want dtmf", got[1])
	}
}

// T3: concurrent Send from multiple goroutines must be race-clean and
// every line must arrive intact (no interleaving).
func TestTextServerConcurrentSend(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	var reader *bufio.Reader
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		var child net.Conn
		child, reader = dialText(t, path)
		_ = child
		return true
	})
	// Dial returns before acceptLoop stores the child. A Send in that
	// gap returns "no child connected" and the sender returns. The drain
	// then blocks in ReadString forever, and the package hits the 10m
	// timeout (CI run 42349356264, socket_test.go drain wait). Wait until
	// one Send succeeds, then drop that warmup line so the counted burst
	// stays exact.
	waitConn(t, func() bool {
		return srv.Send(Frame{CallID: "c", Event: EventSay, Text: "warm"}) == nil
	})
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("warmup read: %v", err)
	}

	const goroutines, perG = 8, 25
	total := goroutines * perG

	// Drain concurrently: Senders must never be blocked on a child that
	// only starts reading after wg.Wait (socket buffer would fill and
	// deadlock the senders).
	drainDone := make(chan []string, 1)
	go func() {
		var lines []string
		for len(lines) < total {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}
			lines = append(lines, line)
		}
		drainDone <- lines
	}()

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for range perG {
				err := srv.Send(Frame{CallID: "c", Event: EventSay, Text: "x"})
				if err != nil {
					t.Errorf("Send: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	var lines []string
	select {
	case lines = <-drainDone:
	case <-time.After(5 * time.Second):
		t.Fatal("drain did not finish within 5s; Send raced accept or a write was lost")
	}
	if len(lines) != total {
		t.Fatalf("drained %d lines, want %d", len(lines), total)
	}
	for i := range total {
		line := lines[i]
		f, derr := DecodeFrame([]byte(line))
		if derr != nil {
			t.Fatalf("line %d not a clean frame (%q): %v", i+1, line, derr)
		}
		if f.Text != "x" {
			t.Fatalf("line %d corrupted: %+v", i+1, f)
		}
	}
}

// T3: a closed child connection surfaces as a Send error, not a panic.
func TestTextServerClosedChild(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	var child net.Conn
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		child, _ = dialText(t, path)
		return true
	})
	// Wait until the server has actually claimed the child: a successful
	// Send proves the daemon-side conn is live (the socket file can exist
	// before acceptLoop runs).
	waitConn(t, func() bool {
		return srv.Send(Frame{CallID: "c", Event: EventSay, Text: "warm"}) == nil
	})

	// Child hangs up: writes must return an error, never panic. There is
	// a window where the daemon-side conn still holds buffered data, so
	// poll until Send fails.
	_ = child.Close()
	var sendErr error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sendErr = srv.Send(Frame{CallID: "c", Event: EventClear})
		if sendErr != nil {
			break
		}
		// A send that lands in the socket buffer before close was noticed
		// is harmless; try again.
		time.Sleep(2 * time.Millisecond)
	}
	if sendErr == nil {
		t.Fatalf("Send after child close must error")
	}

	// Send with no child at all errors cleanly too.
	srv2 := NewTextServer(filepath.Join(dir, "text2.sock"), nil)
	if err := srv2.Send(Frame{CallID: "c", Event: EventSay}); err == nil {
		t.Fatalf("Send with no child must error")
	}
}

// Review checklist: exactly one child; the second connection is refused
// with a log line and closed, and the first child keeps working.
func TestTextServerSecondConnectionRefused(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	var first net.Conn
	var reader *bufio.Reader
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		first, reader = dialText(t, path)
		return true
	})

	// Ensure the server claimed the first child before the second dial:
	// otherwise the second connection might win the claim race. The warm
	// send doubles as data the final read below must NOT see twice.
	waitConn(t, func() bool {
		return srv.Send(Frame{CallID: "c", Event: EventSay, Text: "warm"}) == nil
	})
	_, _ = reader.ReadString('\n') // consume the warm frame
	_ = first                      // conn kept open via dialText cleanup

	second, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	// The second conn must be closed by the server shortly after accept.
	// Read-poll with a short deadline until the refused conn yields EOF.
	waitConn(t, func() bool {
		buf := make([]byte, 1)
		_ = second.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		_, rerr := second.Read(buf)
		return rerr != nil
	})
	_ = second.Close()

	// First child unaffected.
	if err := srv.Send(Frame{CallID: "c", Event: EventSay, Text: "still mine"}); err != nil {
		t.Fatalf("Send to first child: %v", err)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("first child read: %v", err)
	}
	if want := `{"call_id":"c","event":"say","text":"still mine"}` + "\n"; line != want {
		t.Fatalf("first child read %q, want %q", line, want)
	}
}

// T5: socket paths derive from config.MeeptPath(); MEEPT_HOME override
// lands the files under <home>/voice/sock, never a hardcoded ~/.meept.
func TestSocketDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("MEEPT_HOME", tmp)

	wantDir := filepath.Join(tmp, "voice", "sock")
	if got := SocketDir(); got != wantDir {
		t.Fatalf("SocketDir() = %q, want %q", got, wantDir)
	}

	// AudioPair files land under that dir when it is used (T4+T5).
	p, err := NewAudioPair(wantDir, "ccid-x")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	defer func() { _ = p.Close() }()
	for _, suffix := range []string{".in", ".out"} {
		if _, err := os.Stat(filepath.Join(wantDir, "ccid-x"+suffix)); err != nil {
			t.Fatalf("socket file %s missing: %v", suffix, err)
		}
	}
}

// T4: pair round-trip - daemon writes 640B to .in, child reads them; child
// writes back, daemon ReadFromChild receives them.
func TestAudioPairRoundTrip(t *testing.T) {
	dir := shortDir(t)
	p, err := NewAudioPair(dir, "ccid-1")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	defer func() { _ = p.Close() }()

	in, err := net.Dial("unix", filepath.Join(dir, "ccid-1.in"))
	if err != nil {
		t.Fatalf("child dial .in: %v", err)
	}
	out, err := net.Dial("unix", filepath.Join(dir, "ccid-1.out"))
	if err != nil {
		t.Fatalf("child dial .out: %v", err)
	}

	// Wait until the pair accepted both ends: dials complete before the
	// daemon-side Accept goroutines store the conns. Probe by looping one
	// byte around the full path: daemon writes to .in, the child-side conn
	// echoes it back on .out, daemon reads it. A not-yet-accepted end
	// errors instead of blocking, so the poll terminates.
	waitConn(t, func() bool {
		if err := p.WriteToChild([]byte{0xAB}); err != nil {
			return false
		}
		echo := make([]byte, 1)
		if _, err := ioReadFull(in, echo); err != nil {
			return false
		}
		if _, err := out.Write(echo); err != nil {
			return false
		}
		one := make([]byte, 1)
		if _, err := p.ReadFromChild(one); err != nil {
			return false
		}
		return one[0] == 0xAB
	})
	// The warm-up loop can have queued extra probe bytes on the child-side
	// conn; sweep it until a deadline read finds silence so the payload
	// check below reads exact data.
	for range 10 {
		if a := drain(in); !a {
			break
		}
	}
	// A partial probe (failed mid-flight after out.Write) can leave probe
	// bytes inside the DAEMON-side .out buffer, which drain cannot see.
	// Sync marker: the child writes a marker and the daemon consumes
	// everything up to and including it via p.ReadFromChild, leaving that
	// buffer empty by construction.
	if _, err := out.Write([]byte("SYNC|")); err != nil {
		t.Fatalf("sync write: %v", err)
	}
	syncBuf := make([]byte, 512)
	synced := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !synced {
		n, err := p.ReadFromChild(syncBuf)
		if err != nil {
			t.Fatalf("sync read: %v", err)
		}
		for i := 0; i+5 <= n; i++ {
			if string(syncBuf[i:i+5]) == "SYNC|" {
				synced = true
				break
			}
		}
	}
	if !synced {
		t.Fatalf("sync marker never arrived")
	}

	pcm := make([]byte, 640) // L16@16k 20ms chunk
	for i := range pcm {
		pcm[i] = byte(i)
	}
	if err := p.WriteToChild(pcm); err != nil {
		t.Fatalf("WriteToChild: %v", err)
	}
	got := make([]byte, len(pcm))
	if _, err := ioReadFull(in, got); err != nil {
		t.Fatalf("child read .in: %v", err)
	}
	for i := range pcm {
		if got[i] != pcm[i] {
			t.Fatalf("byte %d: got %d want %d", i, got[i], pcm[i])
		}
	}

	// Reverse direction: child writes, daemon reads.
	back := []byte("child-pcm-data")
	if _, err := out.Write(back); err != nil {
		t.Fatalf("child write .out: %v", err)
	}
	buf := make([]byte, len(back))
	n, err := p.ReadFromChild(buf)
	if err != nil {
		t.Fatalf("ReadFromChild: %v", err)
	}
	if n != len(back) {
		t.Fatalf("ReadFromChild n = %d, want %d", n, len(back))
	}
	if string(buf) != string(back) {
		t.Fatalf("ReadFromChild data mismatch")
	}
}

// T4: Close removes both socket files; double Close is safe.
func TestAudioPairClose(t *testing.T) {
	dir := shortDir(t)
	p, err := NewAudioPair(dir, "ccid-2")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	inPath := filepath.Join(dir, "ccid-2.in")
	outPath := filepath.Join(dir, "ccid-2.out")
	if _, err := os.Stat(inPath); err != nil {
		t.Fatalf(".in file missing: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf(".out file missing: %v", err)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(inPath); !os.IsNotExist(err) {
		t.Fatalf(".in still present after Close: %v", err)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf(".out still present after Close: %v", err)
	}
	// Double close: must not error or panic.
	if err := p.Close(); err != nil {
		t.Fatalf("double Close: %v", err)
	}
}

// Writes/reads after Close error cleanly (no panic), including when the
// child never connected.
func TestAudioPairLifecycleErrors(t *testing.T) {
	if _, err := NewAudioPair("", "c"); err == nil {
		t.Fatalf("empty dir must error")
	}
	if _, err := NewAudioPair(t.TempDir(), ""); err == nil {
		t.Fatalf("empty ccid must error")
	}

	dir := shortDir(t)
	p, err := NewAudioPair(dir, "ccid-3")
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	// No child dialed yet: ReadFromChild errors (nothing to read); a small
	// WriteToChild is buffered for flush-on-accept (leaf 06 splice writes
	// before the child dials on live calls), and after Close it errors.
	if _, err := p.ReadFromChild(make([]byte, 8)); err == nil {
		t.Fatalf("ReadFromChild without child must error")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.WriteToChild([]byte{1}); err == nil {
		t.Fatalf("WriteToChild after Close must error")
	}
}

// drain consumes any bytes still queued on a child-side conn from the
// warm-up probe loop, using a short read deadline so it never blocks on
// silence. Returns true if any byte was consumed.
func drain(c net.Conn) bool {
	consumed := false
	buf := make([]byte, 4096)
	for {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
		n, err := c.Read(buf)
		if n > 0 {
			consumed = true
		}
		if err != nil {
			break
		}
	}
	// Restore streaming mode.
	_ = c.SetReadDeadline(time.Time{})
	return consumed
}

func ioReadFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// ---- start-token authentication gate (bughunt 2026-11-07 voice) ----

// T-auth-1: with an expected token armed, a child that sends the token as
// its first line is claimed; frames flow after the auth line, and the auth
// line itself is NEVER parsed as a frame (the handler sees only the prompt).
func TestTextServerTokenAuthClaimsChild(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	srv.SetExpectedToken("tok-abc")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	frames := make(chan Frame, 8)
	srv.SetHandler(func(f Frame) { frames <- f })

	var child net.Conn
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		child, _ = dialText(t, path)
		return true
	})
	if _, err := child.Write([]byte("tok-abc\n" +
		`{"call_id":"c1","event":"prompt","text":"hello"}` + "\n")); err != nil {
		t.Fatalf("child write: %v", err)
	}

	// Claimed: Send to the child succeeds (it must drain the frame).
	waitConn(t, func() bool {
		return srv.Send(Frame{CallID: "c1", Event: EventSay, Text: "ok"}) == nil
	})
	select {
	case f := <-frames:
		if f.Event != EventPrompt || f.CallID != "c1" || f.Text != "hello" {
			t.Fatalf("frame = %+v, want prompt c1 hello", f)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("frame after auth line never reached the handler")
	}
	// The auth line must NOT have reached the handler as a frame: only the
	// one prompt was written. Drain any late arrival to prove absence.
	select {
	case f := <-frames:
		t.Fatalf("unexpected second frame delivered: %+v", f)
	case <-time.After(100 * time.Millisecond):
	}
}

// T-auth-2: a WRONG token is refused — conn closed, never claimed (Send
// keeps failing with "no child connected"), and the warning names the
// failure without leaking token material. A LATER correct dial still
// claims the transport (refusal does not poison the gate).
func TestTextServerTokenAuthRefusesWrongToken(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	logBuf := &lockedBuffer{}
	srv := NewTextServer(path, slog.New(slog.NewTextHandler(logBuf, nil)))
	//nolint:gosec // G101: fake auth token fixture, not a real credential
	const token = "sekrit-token-xyz"
	srv.SetExpectedToken(token)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	var impostor net.Conn
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		impostor, _ = dialText(t, path)
		return true
	})
	if _, err := impostor.Write([]byte("not-the-token\n")); err != nil {
		t.Fatalf("impostor write: %v", err)
	}
	// Refused conn: the server closes it, so a read sees EOF/error.
	waitConn(t, func() bool {
		buf := make([]byte, 1)
		_ = impostor.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		_, rerr := impostor.Read(buf)
		return rerr != nil
	})

	// No claim ever happened: Send fails while the gate holds no child.
	if err := srv.Send(Frame{CallID: "c", Event: EventSay}); err == nil {
		t.Fatal("impostor was claimed as the child")
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "start token authentication failed") {
		t.Fatalf("refusal not logged: %s", logs)
	}
	if strings.Contains(logs, token) || strings.Contains(logs, "not-the-token") {
		t.Fatalf("log leaked token material: %s", logs)
	}

	// A later correct child still claims (refused dialers don't poison).
	var child net.Conn
	waitConn(t, func() bool {
		child, _ = dialText(t, path)
		_, werr := child.Write([]byte(token + "\n"))
		if werr != nil {
			return false
		}
		return srv.Send(Frame{CallID: "c", Event: EventSay, Text: "hi"}) == nil
	})
	_ = child

	// And the SECOND correct dial (child slot taken) is refused by the
	// one-child rule, not the token gate.
	second, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()
	waitConn(t, func() bool {
		buf := make([]byte, 1)
		_ = second.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		_, rerr := second.Read(buf)
		return rerr != nil
	})
}

// T-auth-3: a dialer that sends NOTHING and hangs is refused at the auth
// deadline (fast arm: 200ms); EOF-before-line is refused the same way.
func TestTextServerTokenAuthRefusesStalledAndEOF(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	srv.SetExpectedToken("tok-abc")
	srv.armFastAuth()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	var stalled net.Conn
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		stalled, _ = dialText(t, path)
		return true
	})
	waitConn(t, func() bool {
		buf := make([]byte, 1)
		_ = stalled.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		_, rerr := stalled.Read(buf)
		return rerr != nil
	})
	if err := srv.Send(Frame{CallID: "c", Event: EventSay}); err == nil {
		t.Fatal("stalled dialer was claimed as the child")
	}

	// EOF without any line: also refused, and the gate stays usable.
	eofConn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("eof dial: %v", err)
	}
	_ = eofConn.Close()
	waitConn(t, func() bool {
		return srv.Send(Frame{CallID: "c", Event: EventSay}) != nil
	})
	// A correct child after both refusals still claims.
	var child net.Conn
	waitConn(t, func() bool {
		child, _ = dialText(t, path)
		if _, werr := child.Write([]byte("tok-abc\n")); werr != nil {
			return false
		}
		return srv.Send(Frame{CallID: "c", Event: EventSay, Text: "hi"}) == nil
	})
	_ = child
}

// T-auth-4: with NO expected token armed the server keeps the legacy
// first-dial-wins behavior exactly: a bare dial (no auth line at all) is
// claimed and frames flow. This is what every existing test fixture and
// any hand-built child dialer relies on.
func TestTextServerTokenAuthDisabledLegacyAccept(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	// Deliberately no SetExpectedToken call.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	var child net.Conn
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		child, _ = dialText(t, path)
		return true
	})
	// NO auth line: straight to a frame.
	if _, err := child.Write([]byte(`{"call_id":"c1","event":"prompt","text":"x"}` + "\n")); err != nil {
		t.Fatalf("child write: %v", err)
	}
	frames := make(chan Frame, 4)
	srv.SetHandler(func(f Frame) { frames <- f })
	select {
	case f := <-frames:
		if f.CallID != "c1" || f.Event != EventPrompt {
			t.Fatalf("legacy frame = %+v", f)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("legacy (no token) accept: frame never reached handler")
	}
}

// T-auth-5: SetExpectedToken("") disarms the gate (nil-safe setter: an
// empty token never arms a partial state).
func TestTextServerSetExpectedTokenClearsGate(t *testing.T) {
	dir := shortDir(t)
	path := filepath.Join(dir, "text.sock")
	srv := NewTextServer(path, nil)
	srv.SetExpectedToken("tok")
	srv.SetExpectedToken("")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()

	var child net.Conn
	waitConn(t, func() bool {
		if _, err := os.Stat(path); err != nil {
			return false
		}
		child, _ = dialText(t, path)
		return true
	})
	// Bare dial, no auth line: must claim (gate cleared).
	waitConn(t, func() bool {
		return srv.Send(Frame{CallID: "c", Event: EventSay}) == nil
	})
	_ = child
}
