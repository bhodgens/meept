// Package voicechild is the child side of the voice sidecar protocol: one
// process (cmd/meept-voice) hosting N call pipelines over the shared text
// socket (C1) and per-call audio socket pairs (C2). Contract references
// (C1, C2) point at docs/plans/20261102-voice-sidecar/master.md.
//
// Each live call gets one jargo pipeline (pipeline.New + pipeline.NewWorker);
// frames are routed by call_id. Provider adapters (STT/TTS) are stubbed
// here; leaf 08 fills internal/voicechild/providers.
package voicechild

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/voice/clipstore"
	"github.com/caimlas/meept/internal/voiceframe"
)

// textWriteTimeout bounds a single text-socket write under the write
// mutex (audit fix 3): the repo invariant is "never hold a mutex across
// I/O" — the write mutex inherently serializes conn.Write (one line at a
// time is the CONTRACT: concurrent writes would interleave JSON lines),
// so the safety property comes from the deadline: a wedged daemon can
// stall one writer for at most this long, never the whole child.
const textWriteTimeout = 2 * time.Second

// ChildConfig configures a Child.
type ChildConfig struct {
	// TextSocketPath is the unix socket path of the daemon's shared text
	// socket (C1).
	TextSocketPath string
	// AudioDir is the directory holding per-call audio socket pairs
	// (<ccid>.in / <ccid>.out, C2).
	AudioDir string
	// TokenReader supplies the start token (stdin in production; any
	// reader in tests). The token is never argv, never env.
	TokenReader io.Reader
	// Logger receives structured logs; nil defaults to slog.Default().
	Logger *slog.Logger
}

// Child connects the daemon text socket and routes C1 frames to per-call
// pipelines.
type Child struct {
	cfg    ChildConfig
	logger *slog.Logger
	token  string

	// Provider factories, injected by cmd/meept-voice (leaf 08 adapters);
	// nil factories leave the leaf-07 stubs in place (tests).
	sttFactory STTFactory
	ttsFactory TTSFactory
	// vadFactory, injected by cmd/meept-voice, builds an input-side
	// speech-vs-noise analyzer (jargo Silero VAD) for the local leg. A
	// nil factory keeps the legacy RMS + STT-evidence gate; a factory
	// that errors (ONNX lib absent) degrades the same way, per call.
	vadFactory VADFactory
	// turnFactory, injected by cmd/meept-voice, builds an input-side
	// COMPLETE-THOUGHT analyzer (jargo Smart Turn) for the local leg. A
	// nil factory keeps the whisper-punctuation endpointing oracle; a
	// factory that errors (ONNX lib absent) degrades the same way, per
	// call.
	turnFactory TurnFactory

	// mu guards conn + the provider factories ONLY (audit fix 3: the
	// pipelines map moved to pipelineMu). writeMu serializes text-socket
	// writes; conn itself is read under writeMu after Run publishes it.
	mu      sync.Mutex
	writeMu sync.Mutex // serializes writeLine's conn.Write
	conn    net.Conn   // live text socket; nil until Run dials

	// clipStore is the shared filler-clip cache (Contract C2) handed to
	// every pipeline at construction. New builds clipstore.NewStore()
	// (cheap + inert: the store creates its directory only on the first
	// successful Put — the R4 no-sidecar-no-writes gate lives in the
	// store, not here). SetClipStore swaps it (tests, custom roots).
	// mu-guarded like the factories; read per pipeline at construction.
	clipStore *clipstore.Store
	// resampler is the injected rate converter for clip playback (see
	// Resampler in clips.go); mu-guarded.
	resampler Resampler

	pipelineMu sync.Mutex           // guards pipelines (routing map)
	pipelines  map[string]*pipeline // keyed by call_control_id
}

// writeLine writes one encoded frame line to the text socket, serialized
// across concurrent pipelines (prompt/interrupt emitters race freely).
// Audit fix 3: a DEDICATED write mutex (not the state map's lock) plus a
// write deadline. The mutex is held across conn.Write by necessity —
// serializing whole lines is the socket's contract — so the "never hold a
// mutex across I/O" invariant is honored via the bound instead:
// SetWriteDeadline caps the hold at textWriteTimeout, and the lock here
// guards nothing but writes, so a stalled write can never freeze frame
// routing or provider-factory reads.
func (c *Child) writeLine(ctx context.Context, b []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("voicechild: write cancelled: %w", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("voicechild: text socket not connected")
	}
	if err := conn.SetWriteDeadline(time.Now().Add(textWriteTimeout)); err != nil {
		return fmt.Errorf("voicechild: set write deadline: %w", err)
	}
	if _, err := conn.Write(b); err != nil {
		return fmt.Errorf("voicechild: write text socket: %w", err)
	}
	// Clear the deadline so a later writer never inherits a stale bound.
	_ = conn.SetWriteDeadline(time.Time{})
	return nil
}

// New builds a Child. The start token is read from TokenReader (stdin in
// production) inside Run, never from argv or env.
func New(cfg ChildConfig) *Child {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Child{
		cfg:       cfg,
		logger:    cfg.Logger,
		pipelines: make(map[string]*pipeline),
		clipStore: clipstore.NewStore(),
	}
}

// SetClipStore overrides the filler-clip cache root (tests and callers
// that resolve the store dir themselves; nil disables the clip path
// entirely — every say falls through to live synth). Call before Run.
func (c *Child) SetClipStore(s *clipstore.Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clipStore = s
}

// clipStoreFor reads the shared store under the mu regime, for pipeline
// construction.
func (c *Child) clipStoreFor() *clipstore.Store {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clipStore
}

// readToken reads the start token from the configured reader. The token is
// whitespace-trimmed; a missing or empty token is an error (the daemon will
// never answer with provider keys without it).
func readToken(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("voicechild: nil token reader")
	}
	br := bufio.NewReader(io.LimitReader(r, 4096))
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("voicechild: read start token: %w", err)
	}
	token := trimSpace(line)
	if token == "" {
		return "", errors.New("voicechild: empty start token")
	}
	return token, nil
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

// getPipeline returns the live pipeline for ccid.
func (c *Child) getPipeline(ccid string) (*pipeline, bool) {
	c.pipelineMu.Lock()
	defer c.pipelineMu.Unlock()
	p, ok := c.pipelines[ccid]
	return p, ok
}

// registerPipeline stores a pipeline for ccid.
func (c *Child) registerPipeline(p *pipeline) {
	c.pipelineMu.Lock()
	defer c.pipelineMu.Unlock()
	c.pipelines[p.ccid] = p
}

// dropPipeline removes and returns the pipeline for ccid, if live.
func (c *Child) dropPipeline(ccid string) (*pipeline, bool) {
	c.pipelineMu.Lock()
	defer c.pipelineMu.Unlock()
	p, ok := c.pipelines[ccid]
	if ok {
		delete(c.pipelines, ccid)
	}
	return p, ok
}

// pipelineCount reports how many live pipelines exist (test seam).
func (c *Child) pipelineCount() int {
	c.pipelineMu.Lock()
	defer c.pipelineMu.Unlock()
	return len(c.pipelines)
}

// SetProviderFactories injects the leaf-08 provider constructors. Nil
// arguments keep the stubs (tests); call before Run in production - the
// lock here makes a test-time swap race-free against a running child.
func (c *Child) SetProviderFactories(stt STTFactory, tts TTSFactory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if stt != nil {
		c.sttFactory = stt
	}
	if tts != nil {
		c.ttsFactory = tts
	}
}

// SetVADFactory injects the input-side speech analyzer constructor (jargo
// Silero VAD, wired by cmd/meept-voice). A nil argument keeps the legacy
// RMS + STT-evidence gate; a factory that errors at construct time
// (ONNX runtime lib absent) degrades to the same gate per call.
func (c *Child) SetVADFactory(f VADFactory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vadFactory = f
}

// SetTurnFactory injects the input-side complete-thought analyzer
// constructor (jargo Smart Turn, wired by cmd/meept-voice). A nil argument
// keeps the whisper-punctuation endpointing oracle; a factory that errors
// at construct time (ONNX runtime lib absent) degrades the same way.
func (c *Child) SetTurnFactory(f TurnFactory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turnFactory = f
}

// vadFactoryFor returns the child's VAD factory under the mu regime (nil
// default).
func (c *Child) vadFactoryFor() VADFactory {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vadFactory
}

// turnFactoryFor returns the child's turn-analyzer factory under the mu
// regime (nil default).
func (c *Child) turnFactoryFor() TurnFactory {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turnFactory
}

// Run connects the text socket, reads the start token from TokenReader
// (stdin in production), and serves frames until ctx is cancelled or the
// daemon closes the connection. Each setup frame registers a pipeline for
// its call_control_id; each end frame tears it down.
func (c *Child) Run(ctx context.Context) error {
	if c.cfg.TextSocketPath == "" {
		return errors.New("voicechild: text socket path is empty")
	}
	token, err := readToken(c.cfg.TokenReader)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", c.cfg.TextSocketPath)
	if err != nil {
		return fmt.Errorf("voicechild: dial text socket %s: %w", c.cfg.TextSocketPath, err)
	}
	defer func() { _ = conn.Close() }() //nolint:mutexio // deferred close, no lock held

	// Start-token proof (bughunt 2026-11-07 voice): the one-time token read
	// from STDIN is the child's credential for the text socket. The daemon
	// reads the FIRST line before claiming the connection, so the token
	// goes out here — after dialing, BEFORE any C1 frame. A token-less
	// daemon (no expected token armed: unit fixtures, hand-built dialers)
	// treats the line as an undecodable frame and drops it with a warning;
	// the seam is harmless in that direction.
	if _, err := conn.Write([]byte(token + "\n")); err != nil {
		return fmt.Errorf("voicechild: send start token: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.logger.Info("voicechild: connected to daemon text socket", "path", c.cfg.TextSocketPath)

	readErr := c.serve(ctx, conn)
	c.mu.Lock()
	c.conn = nil
	c.mu.Unlock()
	// Tear down every live pipeline with the connection gone.
	c.pipelineMu.Lock()
	live := make([]*pipeline, 0, len(c.pipelines))
	for _, p := range c.pipelines {
		live = append(live, p)
	}
	c.pipelines = make(map[string]*pipeline)
	c.pipelineMu.Unlock()
	for _, p := range live {
		p.close(context.Background())
	}
	return readErr
}

// serve reads newline-delimited frames until ctx is cancelled or the text
// socket errors/EOFs. A per-frame handler failure NEVER ends the loop
// (audit fix 4): handleLine logs, tears down only the implicated call,
// and returns nil — "fail the call, not the child", the pattern handleKeys
// already implements. Run exits on text-socket read failures only, so one
// malformed line or one failed audio dial cannot kill every live call.
func (c *Child) serve(ctx context.Context, conn net.Conn) error {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	lineCh := make(chan string, 16)
	errCh := make(chan error, 1)
	go func() {
		br := bufio.NewScanner(conn)
		br.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for br.Scan() {
			// Audit fix 6: a bare send here could block forever once the
			// dispatcher exits with readCtx cancelled (the reader keeps
			// scanning until conn.Close lands): select on the done
			// channel so teardown never hangs the reader goroutine.
			select {
			case lineCh <- br.Text():
			case <-readCtx.Done():
				return
			}
		}
		if err := br.Err(); err != nil {
			select {
			case errCh <- fmt.Errorf("voicechild: read text socket: %w", err):
			case <-readCtx.Done():
			}
			return
		}
		select {
		case errCh <- nil: // clean EOF: daemon closed the connection
		case <-readCtx.Done():
		}
	}()

	for {
		select {
		case <-readCtx.Done():
			return nil
		case err := <-errCh:
			if err != nil {
				return err
			}
			return nil // EOF: not a child error
		case line := <-lineCh:
			c.handleLine(readCtx, line)
		}
	}
}

// handleLine decodes one C1 frame and routes it by event. Per-frame
// failures are isolated (audit fix 4): a decode error, a setup/open dial
// failure, or any handler error logs and tears down ONLY the implicated
// call's pipeline; the child and all other calls keep serving. handleLine
// never returns an error to the reader loop — serve() owns process
// termination, and it ends only on a text-socket read failure.
func (c *Child) handleLine(ctx context.Context, line string) {
	frame, err := voiceframe.DecodeFrame([]byte(line))
	if err != nil {
		c.logger.Error("voicechild: malformed frame dropped", "err", err)
		return // a bad line kills the line, not the child
	}
	switch frame.Event {
	case voiceframe.EventOpen:
		c.failCall(frame.CallID, c.handleOpen(ctx, frame), "open")
	case voiceframe.EventSetup:
		c.failCall(frame.CallID, c.handleSetup(ctx, frame), "setup")
	case voiceframe.EventSay:
		c.failCall(frame.CallID, c.handleSay(frame), "say")
	case voiceframe.EventClear:
		c.handleClear(frame)
	case voiceframe.EventEnd:
		// Hangup flush: the pipeline's close() drains the transcript
		// lane (pump EOF -> consumer EOF flush); nothing to emit here.
		c.handleEnd(frame)
	case voiceframe.EventClipRender:
		// Clip warming (Contract C3): never an error path — a render
		// failure answers clip_ready ok:false, and an unknown call drops
		// the frame (the planner re-requests per call). failCall is
		// deliberately NOT used: the clip path must never tear down a
		// live call.
		c.handleClipRender(frame)
	case "keys": // voiceframe keys event (leaf-07 C1 shape; constant lives here: the shared codec carries no key fields)
		c.failCall(frame.CallID, c.handleKeysFrame(line), "keys")
	default:
		// Unknown events (dtmf and friends) decode fine per C1; log and
		// continue so a voicemail-tree "press 1" never errors the call.
		c.logger.Debug("voicechild: unhandled frame event", "event", frame.Event, "call_id", frame.CallID)
	}
}

// failCall applies the "fail the call, not the child" posture to one
// handler result: on error, log and tear down exactly that call's
// pipeline (mirroring handleKeys' construction-failure path). An error
// with an empty call_id has no pipeline to drop — log and move on.
func (c *Child) failCall(ccid string, err error, op string) {
	if err == nil {
		return
	}
	c.logger.Error("voicechild: frame handler failed; tearing down the call only",
		"err", err, "call_id", ccid, "event", op)
	if ccid == "" {
		return
	}
	if p, ok := c.dropPipeline(ccid); ok {
		p.close(context.Background())
	}
}

// handleSetup registers a pipeline for the frame's call_control_id. A setup
// for an already-live ccid is idempotent (existing pipeline kept).
func (c *Child) handleSetup(ctx context.Context, frame voiceframe.Frame) error {
	if frame.CallID == "" {
		return errors.New("voicechild: setup frame with empty call_id")
	}
	if _, ok := c.getPipeline(frame.CallID); ok {
		c.logger.Debug("voicechild: setup for live call ignored", "call_id", frame.CallID)
		return nil
	}
	p, err := c.newPipeline(frame)
	if err != nil {
		return err
	}
	c.registerPipeline(p)
	c.logger.Info("voicechild: pipeline registered",
		"call_id", frame.CallID, "codec", frame.Codec, "sample_rate", frame.SampleRate)
	return p.start(ctx)
}

// handleSay routes speech text to the call's pipeline TTS stage.
func (c *Child) handleSay(frame voiceframe.Frame) error {
	p, ok := c.getPipeline(frame.CallID)
	if !ok {
		c.logger.Warn("voicechild: say for unknown call", "call_id", frame.CallID)
		return nil
	}
	return p.enqueueSay(frame)
}

// handleClear drops queued TTS audio for the call and stops pending writes
// to the call's .out socket (C2: clear stops playback for THAT call only).
func (c *Child) handleClear(frame voiceframe.Frame) {
	p, ok := c.getPipeline(frame.CallID)
	if !ok {
		c.logger.Debug("voicechild: clear for unknown call", "call_id", frame.CallID)
		return
	}
	p.clear()
}

// handleEnd tears down the call's pipeline.
func (c *Child) handleEnd(frame voiceframe.Frame) {
	p, ok := c.dropPipeline(frame.CallID)
	if !ok {
		c.logger.Debug("voicechild: end for unknown call", "call_id", frame.CallID)
		return
	}
	c.logger.Info("voicechild: pipeline torn down", "call_id", frame.CallID, "reason", frame.Reason)
	p.close(context.Background())
}
