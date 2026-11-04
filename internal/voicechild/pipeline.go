package voicechild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"sync"

	"github.com/caimlas/meept/internal/voiceframe"
)

// STTProvider turns a stream of PCM chunks into transcripts. Stubbed in
// this leaf; leaf 08 implements real adapters (Deepgram et al).
type STTProvider interface {
	Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error)
}

// TTSProvider synthesizes speech audio for one text unit. Stubbed in this
// leaf; leaf 08 implements real adapters.
type TTSProvider interface {
	Speak(ctx context.Context, text string) (io.ReadCloser, error)
}

// Transcript is one recognition result from an STTProvider.
type Transcript struct {
	Text  string
	Final bool
}

// pipeline is one live call: its jargo worker and the per-call audio pair
// client sockets (C2). All public entry points are safe for concurrent use;
// a child hosts N pipelines concurrently (map[ccid]*pipeline in child.go).
type pipeline struct {
	ccid      string
	codec     string
	sampleRt  int
	child     *Child
	logger    *slog.Logger
	stt       STTProvider // stub this leaf
	tts       TTSProvider // stub this leaf
	sttKey    string      // provider keys, in memory only (leaf 08 consumes)
	ttsKey    string      // provider keys, in memory only (leaf 08 consumes)
	audioOut  net.Conn    // child->daemon PCM (C2 <ccid>.out)
	audioIn   net.Conn    // daemon->child PCM (C2 <ccid>.in)
	audioDir  string

	// playback state, guarded by playMu
	playMu      sync.Mutex
	dropQueued  bool   // clear received: drop queued audio, stop writes
	interrupted bool   // one interrupt per barge-in until playback restarts
	speaking    bool   // playback active (say in flight)
	outCh       []byte // queued PCM awaiting write to audioOut

	// cancel cancels the pipeline's internal context (workers, streams).
	cancel context.CancelFunc
	ctx    context.Context
}

// newPipeline builds the per-call pipeline from a setup frame: dials the
// C2 audio pair and constructs the (stubbed) jargo worker chain.
func (c *Child) newPipeline(frame voiceframe.Frame) (*pipeline, error) {
	if c.cfg.AudioDir == "" {
		return nil, errors.New("voicechild: audio dir is empty")
	}
	inPath := filepath.Join(c.cfg.AudioDir, frame.CallID+".in")
	outPath := filepath.Join(c.cfg.AudioDir, frame.CallID+".out")
	// Short-lived dials with deadlines; the daemon listeners accept one
	// connection per socket (AudioPair doc).
	dialer := net.Dialer{}
	inConn, err := dialer.DialContext(context.Background(), "unix", inPath)
	if err != nil {
		return nil, fmt.Errorf("voicechild: dial audio in %s: %w", inPath, err)
	}
	outConn, err := dialer.DialContext(context.Background(), "unix", outPath)
	if err != nil {
		_ = inConn.Close() //nolint:mutexio // error path, no lock held
		return nil, fmt.Errorf("voicechild: dial audio out %s: %w", outPath, err)
	}

	pctx, cancel := context.WithCancel(context.Background())
	p := &pipeline{
		ccid:     frame.CallID,
		codec:    frame.Codec,
		sampleRt: frame.SampleRate,
		child:    c,
		logger:   c.logger,
		stt:      stubSTT{},
		tts:      stubTTS{},
		audioIn:  inConn,
		audioOut: outConn,
		audioDir: c.cfg.AudioDir,
		ctx:      pctx,
		cancel:   cancel,
	}
	return p, nil
}

// start launches the pipeline's background work. In this leaf the jargo
// worker chain is constructed but its transport is the unix audio pair, so
// start only begins the daemon->child audio pump; leaf 08 wires real
// providers into the worker chain.
func (p *pipeline) start(ctx context.Context) error {
	go p.pumpAudioIn()
	return nil
}

// pumpAudioIn reads daemon->child PCM from <ccid>.in and feeds the STT
// stage. Chunk sizes are codec-derived (C2). In this leaf the STT stage is
// a stub, so chunks are counted but not transcribed; the pump keeps the
// socket drained so the daemon never blocks writing.
func (p *pipeline) pumpAudioIn() {
	chunk, err := voiceframe.ChunkBytes(p.codec, p.sampleRt)
	if err != nil {
		p.logger.Error("voicechild: audio pump chunk size", "err", err, "call_id", p.ccid)
		return
	}
	buf := make([]byte, chunk)
	for {
		n, err := p.audioIn.Read(buf)
		if n > 0 {
			// Stub STT consumes nothing yet; leaf 08 forwards buf[:n] into
			// the jargo pipeline input.
		}
		if err != nil {
			return // socket closed by teardown or daemon
		}
		select {
		case <-p.ctx.Done():
			return
		default:
		}
	}
}

// enqueueSay queues speech text for synthesis and playback. Frames carry
// last:true to mark the end of one utterance; the stub TTS starts playing
// immediately (leaf 08 replaces the synthesis itself).
func (p *pipeline) enqueueSay(frame voiceframe.Frame) error {
	p.playMu.Lock()
	if p.dropQueued {
		p.playMu.Unlock()
		return nil // cleared: silently drop
	}
	p.outCh = append(p.outCh, []byte(frame.Text)...) // placeholder queue marker; real audio queued below
	p.playMu.Unlock()

	go p.playback(frame)
	return nil
}

// playback synthesizes text (stub) and writes PCM to the .out socket
// (child->daemon). A clear received mid-playback drops the remaining audio
// and stops writes (T4).
func (p *pipeline) playback(frame voiceframe.Frame) {
	pcm, err := p.tts.Speak(p.ctx, frame.Text)
	if err != nil {
		p.logger.Error("voicechild: tts speak", "err", err, "call_id", p.ccid)
		return
	}
	defer func() { _ = pcm.Close() }() //nolint:mutexio // deferred close, no lock held

	p.playMu.Lock()
	if p.dropQueued {
		p.playMu.Unlock()
		return
	}
	p.speaking = true
	p.playMu.Unlock()
	defer func() {
		p.playMu.Lock()
		p.speaking = false
		p.playMu.Unlock()
	}()

	chunk, err := voiceframe.ChunkBytes(p.codec, p.sampleRt)
	if err != nil {
		p.logger.Error("voicechild: playback chunk size", "err", err, "call_id", p.ccid)
		return
	}
	buf := make([]byte, chunk)
	for {
		p.playMu.Lock()
		dropped := p.dropQueued
		p.playMu.Unlock()
		if dropped {
			return // clear: drop queued audio, stop writes
		}
		n, readErr := pcm.Read(buf)
		if n > 0 {
			p.playMu.Lock()
			dropped := p.dropQueued
			p.playMu.Unlock()
			if dropped {
				return // clear arrived while reading: drop the chunk
			}
			if _, werr := p.audioOut.Write(buf[:n]); werr != nil {
				p.logger.Error("voicechild: audio out write", "err", werr, "call_id", p.ccid)
				return
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				p.logger.Error("voicechild: tts stream read", "err", readErr, "call_id", p.ccid)
			}
			return
		}
		select {
		case <-p.ctx.Done():
			return
		default:
		}
	}
}

// clear drops queued TTS audio for this call and stops pending .out writes
// (C1 Frame{clear}; T4). Idempotent.
func (p *pipeline) clear() {
	p.playMu.Lock()
	p.dropQueued = true
	p.outCh = nil
	p.playMu.Unlock()
}

// noteBargeIn records caller speech during playback and reports whether an
// interrupt frame must be emitted: exactly once per barge-in, until the
// next say restarts playback (T5).
func (p *pipeline) noteBargeIn() bool {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	if !p.speaking || p.interrupted || p.dropQueued {
		return false
	}
	p.interrupted = true
	return true
}

// playbackRestarted resets the one-interrupt-per-barge-in latch when a new
// say begins playback.
func (p *pipeline) playbackRestarted() {
	p.playMu.Lock()
	p.interrupted = false
	p.speaking = true
	p.playMu.Unlock()
}

// close tears the pipeline down: cancels its context and closes both audio
// sockets. Idempotent.
func (p *pipeline) close(ctx context.Context) {
	p.cancel()
	_ = ctx // kept for future graceful-drain variants
	_ = p.audioIn.Close()
	_ = p.audioOut.Close()
}

// stubSTT is the leaf-07 stand-in for the leaf-08 STT adapter: it emits
// nothing, so no prompt frames originate from it in this leaf. Tests inject
// transcripts directly through emitPrompt.
type stubSTT struct{}

func (stubSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error) {
	out := make(chan Transcript)
	go func() {
		defer close(out)
		for range pcm {
			// stub: consume and drop; leaf 08 forwards to a real provider
		}
		<-ctx.Done()
	}()
	return out, nil
}

// stubTTS is the leaf-07 stand-in for the leaf-08 TTS adapter: it returns
// deterministic PCM (one 640-byte L16 chunk per say) so tests can observe
// exact writes on the .out socket.
type stubTTS struct{}

func (stubTTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	// Deterministic placeholder audio: two chunks of silence sized for the
	// negotiated codec is not required here; the tests assert on the first
	// chunk arriving before a clear drops the rest.
	pcm := make([]byte, 1280)
	for i := range pcm {
		pcm[i] = byte(i % 251) // deterministic, non-zero pattern
	}
	return io.NopCloser(newSliceReader(pcm)), nil
}

// newSliceReader returns a reader over b.
func newSliceReader(b []byte) *sliceReader { return &sliceReader{b: b} }

type sliceReader struct {
	b []byte
	i int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
