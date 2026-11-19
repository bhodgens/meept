package providers

import (
	"context"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

// STTBaseFactory builds a fresh batch STT provider for one recognition
// snapshot. Provider-agnostic: the decorator depends on this shape and on
// voicechild.STTProvider only - no vendor type switch, no vendor import.
type STTBaseFactory func(cfg ProviderConfig, key string) (voicechild.STTProvider, error)

// PseudoStreamSTT wraps any batch voicechild.STTProvider and turns it into a
// client-side pseudo-stream: on every PartialIntervalMs tick the accumulated
// PCM is snapshotted and recognized by a FRESH base provider, emitted as a
// Final:false partial. A Final:true is emitted when an utterance closes: on
// pcm close (with a last flush), on a FlushNow kick, or when the accumulated
// window hits the pseudoStreamMaxWindowBytes cap. FlushNow is
// flush-and-keep-streaming (the Whisper/Rumble base FlushNow contract): a
// kick recognizes and resets the open window, then the loop CONTINUES — it
// never closes the transcript stream, so repeated barge-in kicks never deafen
// the call. Strictly opt-in (cfg.PartialIntervalMs > 0).
type PseudoStreamSTT struct {
	cfg      ProviderConfig
	key      string
	base     STTBaseFactory
	interval time.Duration

	// kickMu guards the per-stream flush-kick channels. FlushNow only
	// performs a non-blocking buffered send under it; no recognition and
	// no I/O happens while the lock is held (mutexio contract).
	kickMu sync.Mutex
	kicks  map[chan struct{}]struct{}
}

var _ voicechild.STTProvider = (*PseudoStreamSTT)(nil)

// newPseudoStreamSTT builds the opt-in decorator. Called by NewSTT only when
// PartialIntervalMs > 0, so the non-positive path stays byte-identical.
func newPseudoStreamSTT(cfg ProviderConfig, key string, base STTBaseFactory) (voicechild.STTProvider, error) {
	interval := time.Duration(cfg.PartialIntervalMs) * time.Millisecond
	if interval <= 0 || base == nil {
		// No meaningful cadence or no factory: nothing to decorate. The
		// caller guards against this, but stay fail-soft rather than panic.
		return nil, nil
	}
	return &PseudoStreamSTT{cfg: cfg, key: key, base: base, interval: interval}, nil
}

// pseudoStreamMaxWindowBytes caps one accumulated PCM window at 10 seconds
// of 16 kHz s16le mono (the canonical child leg rate: 16000 * 2 * 10). Every
// tick re-recognizes the WHOLE buffer, so without a cap memory and CPU grow
// O(n^2) on a never-silent source (bughunt 2026-11-19 M2). On overflow the
// window force-finalizes and resets — the same path as a FlushNow kick.
const pseudoStreamMaxWindowBytes = 320000

// FlushNow force-finalizes the open window of every live stream (early-kick
// for barge-in) WITHOUT closing it: each stream recognizes the open window,
// emits Final:true when non-empty, resets, and keeps streaming (the base
// Whisper/Rumble FlushNow contract). A second pending kick coalesces into the
// first; with no live stream it is a no-op. No I/O under kickMu.
func (p *PseudoStreamSTT) FlushNow() {
	p.kickMu.Lock()
	for ch := range p.kicks {
		select {
		case ch <- struct{}{}:
		default: // a kick is already pending: coalesce
		}
	}
	p.kickMu.Unlock()
}

// registerKick adds one stream's flush channel and returns its retire closure
// (deferred by run). Mirrors the whisper/rumble early-flush seam.
func (p *PseudoStreamSTT) registerKick() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	p.kickMu.Lock()
	if p.kicks == nil {
		p.kicks = make(map[chan struct{}]struct{})
	}
	p.kicks[ch] = struct{}{}
	p.kickMu.Unlock()
	return ch, func() {
		p.kickMu.Lock()
		delete(p.kicks, ch)
		p.kickMu.Unlock()
	}
}

// Stream implements voicechild.STTProvider. It returns a channel carrying
// Final:false partials and a Final:true whenever an utterance closes (pcm
// close, a FlushNow kick, or the max-window cap). The channel closes ONLY on
// pcm close (with a final flush) or context cancellation (the latter closes
// without a final) — a FlushNow kick flushes but never closes the stream.
func (p *PseudoStreamSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan voicechild.Transcript, error) {
	// Buffered so a partial emission never stalls the run loop: the PCM
	// feeder must keep being drained even when the downstream consumer
	// pauses reading (e.g. barge-in). Matches the base adapters' buffer.
	out := make(chan voicechild.Transcript, 16)
	kick, retire := p.registerKick()
	go p.run(ctx, pcm, out, kick, retire)
	return out, nil
}

// recognize runs one full recognition pass over a copied snapshot using a
// FRESH base provider. Fail-soft: any construction/Stream error or a nil
// channel yields an empty string; the last non-empty final (or, lacking one,
// the last non-empty transcript) wins. No lock is held across this call.
func (p *PseudoStreamSTT) recognize(ctx context.Context, snapshot []byte) string {
	if len(snapshot) == 0 {
		return ""
	}
	base, err := p.base(p.cfg, p.key)
	if err != nil || base == nil {
		return ""
	}
	in := make(chan []byte, 1)
	in <- snapshot
	close(in)

	out, err := base.Stream(ctx, in)
	if err != nil || out == nil {
		return ""
	}
	var finalText, anyText string
	read := func() string {
		if finalText != "" {
			return finalText
		}
		return anyText
	}
	for {
		select {
		case t, ok := <-out:
			if !ok {
				return read()
			}
			if t.Text == "" {
				continue
			}
			anyText = t.Text
			if t.Final {
				finalText = t.Text
			}
		case <-ctx.Done():
			return read()
		}
	}
}

// run is Stream's body. The buffer is guarded by a local mutex used ONLY
// around append/copy; every base.Stream call and channel emission happens
// with that mutex released.
func (p *PseudoStreamSTT) run(ctx context.Context, pcm <-chan []byte, out chan<- voicechild.Transcript, kick <-chan struct{}, retire func()) {
	defer close(out)
	defer retire()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	var (
		mu  sync.Mutex
		buf []byte
	)
	appendChunk := func(chunk []byte) {
		if len(chunk) == 0 {
			return
		}
		mu.Lock()
		buf = append(buf, chunk...)
		mu.Unlock()
	}
	snapshot := func() []byte {
		mu.Lock()
		s := append([]byte(nil), buf...)
		mu.Unlock()
		return s
	}
	resetWindow := func() {
		mu.Lock()
		buf = nil
		mu.Unlock()
	}
	bufLen := func() int {
		mu.Lock()
		n := len(buf)
		mu.Unlock()
		return n
	}

	// lastText holds the text of the last successful recognition and is
	// used for the single final emission (empty allowed). It is touched
	// only by this goroutine.
	var lastText string

	// finalize emits exactly one Final:true, recognizing a last snapshot
	// first so the utterance tail is not lost.
	finalize := func() {
		if text := p.recognize(ctx, snapshot()); text != "" {
			lastText = text
		}
		select {
		case out <- voicechild.Transcript{Text: lastText, Final: true}:
		case <-ctx.Done():
		}
	}

	// flushWindow recognizes the open window and, when the result is
	// non-empty, emits Final:true — then RESETS the buffer and lastText
	// and returns; the run loop keeps streaming. Shared by the FlushNow
	// kick and the max-window cap (bughunt 2026-11-19 H3/M2). An empty or
	// failed pass emits nothing (the sttFlusher contract: never fire on
	// an empty window) but still resets, bounding memory. No lock is
	// held across recognize or the channel send (mutexio).
	flushWindow := func() {
		text := p.recognize(ctx, snapshot())
		resetWindow()
		lastText = ""
		if text == "" {
			return
		}
		select {
		case out <- voicechild.Transcript{Text: text, Final: true}:
		case <-ctx.Done():
		}
	}

	for {
		select {
		case <-ctx.Done():
			return // cancellation: close WITHOUT a final
		case chunk, ok := <-pcm:
			if !ok {
				if ctx.Err() == nil {
					finalize()
				}
				return
			}
			appendChunk(chunk)
			// M2: a never-silent source must not grow the re-recognized
			// window without bound — at the cap, force the same
			// finalize-and-reset the kick uses.
			if bufLen() >= pseudoStreamMaxWindowBytes && ctx.Err() == nil {
				flushWindow()
			}
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			text := p.recognize(ctx, snapshot())
			if text == "" {
				continue // fail-soft: no partial on an empty/failed pass
			}
			lastText = text
			select {
			case out <- voicechild.Transcript{Text: text, Final: false}:
			case <-ctx.Done():
				return
			}
		case <-kick:
			// H3: flush-and-keep-streaming. The pipeline kicks FlushNow on
			// EVERY sustained-energy window (barge-in attempt); closing
			// the stream here deafened the call after the first kick
			// because the transcript consumer never reopens a cleanly
			// closed channel. Recognize the open window, emit Final:true
			// if non-empty, reset, and CONTINUE the loop.
			if ctx.Err() == nil {
				flushWindow()
			}
		}
	}
}
