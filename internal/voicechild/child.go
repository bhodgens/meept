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

	"github.com/caimlas/meept/internal/voiceframe"
)

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

	mu        sync.Mutex
	pipelines map[string]*pipeline // keyed by call_control_id
	conn      net.Conn             // live text socket; nil until Run dials
	closed    bool
}

// writeLine writes one encoded frame line to the text socket, serialized
// across concurrent pipelines (prompt/interrupt emitters race freely).
func (c *Child) writeLine(ctx context.Context, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return errors.New("voicechild: text socket not connected")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("voicechild: write cancelled: %w", err)
	}
	if _, err := c.conn.Write(b); err != nil {
		return fmt.Errorf("voicechild: write text socket: %w", err)
	}
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
	}
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
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pipelines[ccid]
	return p, ok
}

// registerPipeline stores a pipeline for ccid.
func (c *Child) registerPipeline(p *pipeline) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pipelines[p.ccid] = p
}

// dropPipeline removes and returns the pipeline for ccid, if live.
func (c *Child) dropPipeline(ccid string) (*pipeline, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pipelines[ccid]
	if ok {
		delete(c.pipelines, ccid)
	}
	return p, ok
}

// pipelineCount reports how many live pipelines exist (test seam).
func (c *Child) pipelineCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pipelines)
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
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.logger.Info("voicechild: connected to daemon text socket", "path", c.cfg.TextSocketPath)

	readErr := c.serve(ctx, conn)
	c.mu.Lock()
	c.conn = nil
	c.mu.Unlock()
	// Tear down every live pipeline with the connection gone.
	c.mu.Lock()
	live := make([]*pipeline, 0, len(c.pipelines))
	for _, p := range c.pipelines {
		live = append(live, p)
	}
	c.pipelines = make(map[string]*pipeline)
	c.mu.Unlock()
	for _, p := range live {
		p.close(context.Background())
	}
	return readErr
}

// serve reads newline-delimited frames until ctx is cancelled, EOF, or a
// decode error, dispatching each to its pipeline.
func (c *Child) serve(ctx context.Context, conn net.Conn) error {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	lineCh := make(chan string, 16)
	errCh := make(chan error, 1)
	go func() {
		br := bufio.NewScanner(conn)
		br.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for br.Scan() {
			lineCh <- br.Text()
		}
		if err := br.Err(); err != nil {
			errCh <- fmt.Errorf("voicechild: read text socket: %w", err)
			return
		}
		errCh <- nil // clean EOF: daemon closed the connection
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
			if err := c.handleLine(readCtx, line); err != nil {
				return err
			}
		}
	}
}

// handleLine decodes one C1 frame and routes it by event.
func (c *Child) handleLine(ctx context.Context, line string) error {
	frame, err := voiceframe.DecodeFrame([]byte(line))
	if err != nil {
		return fmt.Errorf("voicechild: %w", err)
	}
	switch frame.Event {
	case voiceframe.EventSetup:
		return c.handleSetup(ctx, frame)
	case voiceframe.EventSay:
		return c.handleSay(frame)
	case voiceframe.EventClear:
		c.handleClear(frame)
		return nil
	case voiceframe.EventEnd:
		c.handleEnd(frame)
		return nil
	default:
		// Unknown events (dtmf and friends) decode fine per C1; log and
		// continue so a voicemail-tree "press 1" never errors the call.
		c.logger.Debug("voicechild: unhandled frame event", "event", frame.Event, "call_id", frame.CallID)
		return nil
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
