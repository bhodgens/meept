// Package voice: socket layer (C1 text socket + C2 per-call audio pairs).
//
// Socket paths derive from config.MeeptPath() ("$MEEPT_HOME/voice/sock"),
// never a hardcoded ~/.meept or os.Getwd.
package voice

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// SocketDir returns the directory under which all voice unix sockets live:
// $MEEPT_HOME/voice/sock, resolved through config.MeeptPath() so an
// MEEPT_HOME override applies (never a hardcoded ~/.meept, never os.Getwd).
func SocketDir() string {
	return config.MeeptPath("voice", "sock")
}

// TextServer is the daemon side of the shared text socket (C1). It listens
// on a unix socket and serves exactly ONE child connection: the process
// model is one child hosting N call pipelines, and every frame carries
// call_id, so a single connection is sufficient and keeps routing trivial.
//
// A second connection attempt is accepted only long enough to be refused
// with a log line and closed (documented choice: refuse, not replace -
// replacing would silently steal the live call transport from a healthy
// child, e.g. one recovering from a forked duplicate).
type TextServer struct {
	path   string
	logger *slog.Logger

	mu      sync.Mutex
	ln      net.Listener
	child   net.Conn // nil until a child connects
	handler func(Frame)
	closed  bool

	// childReady closes the first time a child is claimed (leaf 03):
	// SendRaw's keys emission waits on it so a child that has dialed but
	// not yet been claimed by acceptLoop (unix connect completes into the
	// backlog before Accept runs) still receives its frame.
	childReadyOnce sync.Once
	childReady     chan struct{}
}

// NewTextServer creates a server listening on path (a unix socket path).
// The daemon owns removing the socket file at bind time, mirroring the
// internal/rpc accept-loop shape.
func NewTextServer(path string, logger *slog.Logger) *TextServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &TextServer{path: path, logger: logger, childReady: make(chan struct{})}
}

// Start binds the socket and serves until ctx is cancelled or Close is
// called. On ctx cancel the listener is closed and the socket file removed.
func (s *TextServer) Start(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("voice: create socket dir: %w", err)
	}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("voice: remove stale socket %s: %w", s.path, err)
	}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("voice: listen %s: %w", s.path, err)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close() //nolint:mutexio // lock released below; shutdown path
		_ = os.Remove(s.path)
		return errors.New("voice: text server already closed")
	}
	s.ln = ln
	s.mu.Unlock()

	go s.acceptLoop(ctx, ln)

	<-ctx.Done()
	s.close()
	return nil
}

// acceptLoop accepts connections until the listener closes. The first
// connection becomes the child; subsequent ones are refused with a log
// line (one child hosts N pipelines - see TextServer doc).
func (s *TextServer) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.isClosed() || ctx.Err() != nil {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			s.logger.Error("voice: text socket accept failed", "error", err)
			return
		}
		if s.claimChild(conn) {
			go s.readLoop(conn)
			continue
		}
		s.logger.Warn("voice: refusing second text-socket connection (one child hosts all pipelines)",
			"remote", conn.RemoteAddr().String())
		_ = conn.Close() //nolint:mutexio // refusal path, no lock held
	}
}

// claimChild takes conn as THE child if none is connected yet.
func (s *TextServer) claimChild(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child != nil || s.closed {
		return false
	}
	s.child = conn
	// Leaf 03: signal SendRaw that a child transport exists (close is
	// once-only via childReadyOnce).
	s.childReadyOnce.Do(func() { close(s.childReady) })
	return true
}

// readLoop reads newline-delimited frames from the child and dispatches
// them to the handler. Handler dispatch is synchronous inside the read
// goroutine; the handler must not block on socket writes back to this
// server (Send serializes independently). Unknown event values (dtmf and
// any other) decode fine and reach the handler untouched.
func (s *TextServer) readLoop(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		f, err := DecodeFrame(line)
		if err != nil {
			s.logger.Warn("voice: dropping undecodable text frame", "error", err)
			continue
		}
		s.mu.Lock()
		h := s.handler
		s.mu.Unlock()
		if h != nil {
			h(f)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) && !s.isClosed() {
		s.logger.Warn("voice: text socket read error", "error", err)
	}
	s.mu.Lock()
	if s.child == conn {
		s.child = nil
	}
	s.mu.Unlock()
	_ = conn.Close() //nolint:mutexio // lock released above
}

// SetHandler registers the callback for frames arriving from the child.
// Nil fn is ignored (every Set* is nil-guarded per repo rule).
func (s *TextServer) SetHandler(fn func(Frame)) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	s.handler = fn
	s.mu.Unlock()
}

// Send writes one frame to the connected child. Safe for concurrent use:
// the single write is mutex-serialized so JSON lines never interleave. The
// mutex here exists to serialize connection access (repo mutex-scope rule)
// and is released before the write. Returns an error (never panics) when
// no child is connected or the child connection is closed.
func (s *TextServer) Send(f Frame) error {
	b, err := EncodeFrame(f)
	if err != nil {
		return err
	}
	s.mu.Lock()
	conn := s.child
	s.mu.Unlock()
	if conn == nil {
		return errors.New("voice: no child connected on text socket")
	}
	if _, err := conn.Write(b); err != nil {
		// Surface the dead connection to the caller; readLoop will also
		// notice and clear s.child.
		return fmt.Errorf("voice: write to child: %w", err)
	}
	return nil
}

// SendRaw writes one pre-encoded raw JSON line (already newline-terminated)
// to the connected child, bypassing the Frame codec. It exists for the C1
// keys frame (voice-tierA leaf 03): keys material must not live in the
// shared Frame struct, so the daemon marshals its own keysWire and ships
// the line through this seam. Same mutex-serialized single write as Send;
// returns an error (never panics) when no child is connected or the child
// connection is closed.
func (s *TextServer) SendRaw(line []byte) error {
	s.mu.Lock()
	conn := s.child
	s.mu.Unlock()
	if conn == nil {
		// A dialed-but-unclaimed child (connect completed into the unix
		// listen backlog before acceptLoop's claimChild ran) is not a
		// dead transport: wait briefly for the claim rather than failing
		// the first send. Bounded so a genuinely absent child still
		// errors fast; emission treats the error as a degrade (warn,
		// call continues). The send itself stays exactly once - this is
		// transport-readiness, not a retry loop.
		select {
		case <-s.childReady:
			s.mu.Lock()
			conn = s.child
			s.mu.Unlock()
		case <-time.After(2 * time.Second):
			return errors.New("voice: no child connected on text socket")
		}
	}
	if conn == nil {
		return errors.New("voice: no child connected on text socket")
	}
	if _, err := conn.Write(line); err != nil {
		// Surface the dead connection to the caller; readLoop will also
		// notice and clear s.child.
		return fmt.Errorf("voice: write to child: %w", err)
	}
	return nil
}

// isClosed reports whether Close has been called.
func (s *TextServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Close stops the listener, closes the child connection, and removes the
// socket file. Safe to call multiple times and concurrent with Start's
// shutdown path (both funnel through close once).
func (s *TextServer) Close() {
	s.close()
}

// close performs the idempotent shutdown: state is swapped out under the
// lock, then listeners/conns are closed and the socket file removed
// without the lock held.
func (s *TextServer) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ln := s.ln
	s.ln = nil
	child := s.child
	s.child = nil
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	if child != nil {
		_ = child.Close()
	}
	_ = os.Remove(s.path)
}

// AudioPair is the daemon side of one call's raw-PCM socket pair (C2):
// <ccid>.in carries daemon->child PCM, <ccid>.out carries child->daemon
// PCM. Frames are raw bytes, no envelope; chunk sizes are codec-derived
// (ChunkBytes). The pair does not reorder or pace.
type AudioPair struct {
	ccid string
	dir  string

	mu        sync.Mutex
	in        net.Conn // accepted from <ccid>.in listener
	out       net.Conn // accepted from <ccid>.out listener
	pendingIn [][]byte // writes buffered from before the child dialed .in
	closed    bool
	closeFn   func() // closes both listeners; set by NewAudioPair

	closeOnce sync.Once
}

// NewAudioPair creates the listener socket files <dir>/<ccid>.in and
// <dir>/<ccid>.out and starts accepting one child connection on each. The
// child dials them at call start. Existing files from a previous call with
// the same id are removed first (pair files are per-call and must not
// survive teardown).
func NewAudioPair(dir, ccid string) (*AudioPair, error) {
	if dir == "" {
		return nil, errors.New("voice: audio pair dir is empty")
	}
	if ccid == "" {
		return nil, errors.New("voice: audio pair ccid is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("voice: create socket dir %s: %w", dir, err)
	}
	inPath := filepath.Join(dir, ccid+".in")
	outPath := filepath.Join(dir, ccid+".out")
	for _, p := range []string{inPath, outPath} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("voice: remove stale socket %s: %w", p, err)
		}
	}
	inLn, err := net.Listen("unix", inPath)
	if err != nil {
		return nil, fmt.Errorf("voice: listen %s: %w", inPath, err)
	}
	outLn, err := net.Listen("unix", outPath)
	if err != nil {
		_ = inLn.Close()
		_ = os.Remove(inPath)
		return nil, fmt.Errorf("voice: listen %s: %w", outPath, err)
	}

	p := &AudioPair{ccid: ccid, dir: dir}
	p.closeFn = func() {
		_ = inLn.Close()
		_ = outLn.Close()
	}

	// One accepted connection per socket; the Accept error after Close is
	// the expected shutdown path and is ignored deliberately.
	go func() {
		conn, acceptErr := inLn.Accept()
		if acceptErr != nil {
			return
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = conn.Close()
			return
		}
		p.in = conn
		pending := p.pendingIn
		p.pendingIn = nil
		p.mu.Unlock()
		// Flush pre-connect writes in order. Errors here mean the child
		// dialed and immediately died; the next Write surfaces it.
		for _, chunk := range pending {
			if _, err := conn.Write(chunk); err != nil {
				return
			}
		}
	}()
	go func() {
		conn, acceptErr := outLn.Accept()
		if acceptErr != nil {
			return
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = conn.Close()
			return
		}
		p.out = conn
		p.mu.Unlock()
	}()
	return p, nil
}

// WriteToChild writes one raw PCM chunk to the child over the <ccid>.in
// socket. Errors (never panics) while the child has not connected yet or
// after Close.
func (p *AudioPair) WriteToChild(pcm []byte) error {
	p.mu.Lock()
	conn := p.in
	if conn == nil && !p.closed {
		// Pre-dial: buffer the chunk for flush-on-accept. The media WS
		// goes live before the child dials on real calls, and the splice
		// must not error (or lose audio) over that ordering.
		if len(p.pendingIn)+len(pcm) <= maxPendingInBytes {
			cp := make([]byte, len(pcm))
			copy(cp, pcm)
			p.pendingIn = append(p.pendingIn, cp)
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		return fmt.Errorf("voice: %s.in pre-connect buffer full", p.ccid)
	}
	p.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("voice: child not connected on %s.in", p.ccid)
	}
	if _, err := conn.Write(pcm); err != nil {
		return fmt.Errorf("voice: write %s.in: %w", p.ccid, err)
	}
	return nil
}

// maxPendingInBytes bounds the pre-connect write buffer: 128 x 640-byte
// L16@16k chunks is 2.56 s of audio, far past any realistic dial window.
const maxPendingInBytes = 128 * 640

// pairInPath returns the .in socket file path for the pair (CL5: the
// LocalCall exposes the pair PATHS; leaf 08's clients dial them).
func pairInPath(p *AudioPair) string {
	return filepath.Join(p.dir, p.ccid+".in")
}

// pairOutPath returns the .out socket file path for the pair.
func pairOutPath(p *AudioPair) string {
	return filepath.Join(p.dir, p.ccid+".out")
}

// ReadFromClient reads raw mic bytes from the CLIENT over the accepted
// <ccid>.in conn (local-leg leaf 07). The local leg's client dials the
// pair like the child does and writes mic bytes there; the splice's
// chunking loop reads them through this accessor. Delegates to
// ReadFromChild: same accepted-conn surface, same errors while nobody
// connected or after Close.
func (p *AudioPair) ReadFromClient(buf []byte) (int, error) {
	return p.ReadFromChild(buf)
}

// WriteToClient writes raw bytes to the CLIENT over the accepted conn
// (local-leg leaf 07). Mirrors ReadFromClient: the local leg reuses the
// pair's accepted-conn surface, so client mic in and speaker out ride
// the same pair sockets the child audio already uses.
func (p *AudioPair) WriteToClient(pcm []byte) (int, error) {
	p.mu.Lock()
	conn := p.out
	p.mu.Unlock()
	if conn == nil {
		return 0, fmt.Errorf("voice: client not connected on %s.out", p.ccid)
	}
	n, err := conn.Write(pcm)
	if err != nil {
		return n, fmt.Errorf("voice: write %s.out (client): %w", p.ccid, err)
	}
	return n, nil
}

// ReadFromChild reads raw PCM from the child over the <ccid>.out socket.
// Errors while the child has not connected yet or after Close.
func (p *AudioPair) ReadFromChild(buf []byte) (int, error) {
	p.mu.Lock()
	conn := p.out
	p.mu.Unlock()
	if conn == nil {
		return 0, fmt.Errorf("voice: child not connected on %s.out", p.ccid)
	}
	n, err := conn.Read(buf)
	if err != nil {
		return n, fmt.Errorf("voice: read %s.out: %w", p.ccid, err)
	}
	return n, nil
}

// Close closes both listeners and any accepted connections and removes
// both socket files. Safe to call multiple times.
func (p *AudioPair) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		in := p.in
		p.in = nil
		out := p.out
		p.out = nil
		p.pendingIn = nil
		closeFn := p.closeFn
		p.mu.Unlock()

		if closeFn != nil {
			closeFn()
		}
		if in != nil {
			_ = in.Close()
		}
		if out != nil {
			_ = out.Close()
		}
		// Remove the socket files regardless of listener close errors:
		// leaking per-call files on disk is worse than a stale-listener
		// errno here, and NewAudioPair removes stale files defensively
		// anyway.
		for _, suffix := range []string{".in", ".out"} {
			if err := os.Remove(filepath.Join(p.dir, p.ccid+suffix)); err != nil && !os.IsNotExist(err) {
				closeErr = fmt.Errorf("voice: remove %s%s: %w", p.ccid, suffix, err)
			}
		}
	})
	return closeErr
}
