package voicechild

// Filler-clip cache: child-side render + playback (plan tree
// docs/plans/20261113-filler-clips-and-context-warm, leaf phase1-clips/03).
//
// Contract (master.md C1/C2/C3, FROZEN; orchestrator adjudications A1-A5
// are binding):
//
//   - A say frame may carry clip_id (the 64-hex clipstore key for its
//     text). On a store hit the cached PCM replaces the provider call
//     ENTIRELY (zero Speak, adjudication A2) and rides the EXACT same
//     playback lane as synthesized audio — pacing, barge-in clear, and the
//     played edge are untouched because the swap happens at the reader,
//     upstream of playbackStream. On a miss (or invalid key, or an open
//     error) the frame falls through to today's live tts.Speak(text) path,
//     byte-identical. No opportunistic backfill on the fallback (decided
//     out of scope by the orchestrator review).
//   - A clip_render frame carries up to 32 {key,text} specs. Entries the
//     store already has answer clip_ready ok:true with zero synth
//     (idempotent warm); misses render in the BACKGROUND through a
//     provider built with the canonical clip sample rate (adjudication
//     A1: 16000 Hz, mono s16le — never the call's negotiated rate, so a
//     PCMU@8k call can never poison the shared store), then store.Put +
//     clip_ready ok:true; any failure answers ok:false with a WARN.
//     Renders run at bounded concurrency ONE on a call-scoped context:
//     the worker dies with pipeline teardown (end frame / EOF), never
//     after.
//   - Renders need the TTS providers, which only exist after the keys
//     frame. A clip_render that arrives earlier queues (cap 256,
//     drop-oldest with a WARN) and drains once keys land.
//   - The clip path is audio-only: the say frame's text remains the
//     transcript source (R6), and no clip frame ever carries PCM.

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/caimlas/meept/internal/voice/clipstore"
	"github.com/caimlas/meept/internal/voiceframe"
)

// clipCanonicalRate is the STORE rate for every cached clip
// (adjudication A1): mono s16le at 16000 Hz. Render builds its
// ProviderConfig with exactly this rate; playback resamples from
// Meta.SampleRate to the call's negotiated rate.
const clipCanonicalRate = 16000

// clipRenderQueueCap bounds the pending render list before keys arrive
// (adjudication A3): the daemon shards pools at 32 clips/frame, so 256
// entries is ~8 warm-up frames deep; beyond that the OLDEST entries drop
// with a WARN (they are re-requested on the next planner pass / reload).
const clipRenderQueueCap = 256

// F10 yield settle: a clip_render re-request can land in the SAME instant a
// barge-in clear retires the live synth job. Settle briefly before deciding
// to defer, so a request that races the clear still renders this pass.
// Purely a scheduling settle (not a user knob): zero delay on the idle
// path, and p.ctx.Done() ends it early on teardown. No I/O under any lock.
const (
	clipYieldSettlePoll    = 20 * time.Millisecond
	clipYieldSettleRetries = 5
)

// Resampler converts an s16le mono PCM reader from inRate to outRate. The
// implementation lives in the providers package (jargo's streaming
// resampler); voicechild cannot import providers (the factory interfaces
// make that edge a cycle), so cmd/meept-voice injects it via
// Child.SetResampler. A child without a wired resampler never plays a
// clip whose stored rate differs from the call's negotiated rate — the
// say degrades to today's live-synth path instead of emitting pitched
// audio (fail-safe, adjudication A1).
type Resampler func(src io.Reader, inRate, outRate int) (io.ReadCloser, error)

// SetResampler injects the PCM rate converter used by the clip playback
// path. Call before Run; nil keeps the fail-safe described on Resampler.
func (c *Child) SetResampler(fn Resampler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resampler = fn
}

// resamplerFor reads the injected seam under the mu regime.
func (c *Child) resamplerFor() Resampler {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resampler
}

// resamplerForChild reads the child's injected resampler, nil-safe for
// hand-built pipelines (child == nil in some unit shapes).
func (p *pipeline) resamplerForChild() Resampler {
	if p.child == nil {
		return nil
	}
	return p.child.resamplerFor()
}

// ttsFactoryFor reads the TTS factory under the mu regime (the render
// path constructs a dedicated provider instance per batch; see
// renderProvider).
func (c *Child) ttsFactoryFor() TTSFactory {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ttsFactory
}

// closeClipReader closes a clip reader, logging a close error rather
// than discarding it (repo no-ignored-errors rule; the close runs off
// every lock at every call site — mutexio posture unchanged).
func closeClipReader(p *pipeline, rc io.Closer) {
	if rc == nil {
		return
	}
	if err := rc.Close(); err != nil {
		p.logger.Debug("voicechild: clip reader close failed", "call_id", p.ccid, "err", err)
	}
}

// openClip resolves a say frame's clip_id into a playback reader
// (adjudication A2). ok=false means "miss or unusable": the caller falls
// through to the live Speak path unchanged. A stored clip whose
// Meta.SampleRate differs from the call's negotiated rate is resampled
// (A1); without a wired resampler that entry is treated as a miss rather
// than played at the wrong pitch. File I/O happens with NO lock held
// (mutexio posture).
func (p *pipeline) openClip(key string) (io.ReadCloser, string, bool) {
	if p.clipStore == nil || key == "" {
		return nil, "", false
	}
	rc, meta, err := p.clipStore.Get(key)
	if err != nil {
		// store.Get already cleans corrupt pairs (Contract C2: a corrupt
		// entry is a miss that regenerates). Log at debug: the live-synth
		// fallback makes this invisible to the call.
		p.logger.Debug("voicechild: clip store miss", "call_id", p.ccid, "key", key, "err", err)
		return nil, "", false
	}
	rate := meta.SampleRate
	if rate <= 0 {
		// Meta written by an unknown producer without a rate: trust the
		// canonical contract value only when the call negotiated it, else
		// fall back to live synth.
		rate = clipCanonicalRate
	}
	if rate != p.sampleRt {
		rs := p.resamplerForChild()
		if rs == nil {
			closeClipReader(p, rc)
			p.logger.Debug("voicechild: clip rate mismatch and no resampler wired; live synth",
				"call_id", p.ccid, "key", key, "clip_rate", rate, "call_rate", p.sampleRt)
			return nil, "", false
		}
		out, rerr := rs(rc, rate, p.sampleRt)
		if rerr != nil {
			closeClipReader(p, rc)
			p.logger.Debug("voicechild: clip resample failed; live synth",
				"call_id", p.ccid, "key", key, "err", rerr)
			return nil, "", false
		}
		return out, key, true
	}
	return rc, key, true
}

// emitClipPlayed logs the cached-playback INFO line (adjudication A5):
// printed by the playback worker the moment a clip-sourced reader enters
// the pacing loop, so `grep 'say played from clip'` proves the store
// served audio for the live smoke (root Integration Test Plan step 4).
// key only — never provider material beyond the existing posture.
func (p *pipeline) emitClipPlayed(key string) {
	p.logger.Info("voicechild: say played from clip", "call_id", p.ccid, "key", key)
}

// handleClipRender routes a C1 clip_render frame to its call's pipeline.
// An unknown call is a debug no-op: the planner fires on setup, and a
// frame racing pipeline registration (or arriving after teardown) must
// never error the child (fail the call, not the child — here not even
// the call).
func (c *Child) handleClipRender(frame voiceframe.Frame) {
	p, ok := c.getPipeline(frame.CallID)
	if !ok {
		c.logger.Debug("voicechild: clip_render for unknown call", "call_id", frame.CallID)
		return
	}
	p.enqueueClipRenders(frame.Clips)
}

// enqueueClipRenders appends the frame's specs to the pipeline's render
// queue and lazily starts the single-per-pipeline render worker. The
// queue cap drops the OLDEST entries with a WARN (they ride the next
// planner pass); the worker serializes everything, so render concurrency
// is bounded at ONE for the whole call by construction.
func (p *pipeline) enqueueClipRenders(specs []voiceframe.ClipSpec) {
	if len(specs) == 0 {
		return
	}
	p.clipMu.Lock()
	p.clipQueue = append(p.clipQueue, specs...)
	if overflow := len(p.clipQueue) - clipRenderQueueCap; overflow > 0 {
		// Drop the OLDEST entries (bounded, drop-oldest with a WARN —
		// adjudication A3): the daemon planner re-requests every miss on
		// the next call setup / config-reload pass, so a dropped entry
		// costs a later render, never a permanent hole.
		p.clipQueue = append(p.clipQueue[:0:0], p.clipQueue[overflow:]...)
		p.logger.Warn("voicechild: clip render queue full; dropped oldest",
			"call_id", p.ccid, "cap", clipRenderQueueCap, "dropped", overflow)
	}
	workerUp := p.clipWorkerUp
	if !workerUp {
		p.clipWorkerUp = true
	}
	p.clipMu.Unlock()

	if !workerUp {
		go p.clipRenderWorker()
	}
}

// clipRenderWorker drains the render queue serially (concurrency ONE) on
// the pipeline ctx: a call teardown (end frame / EOF -> p.close cancels
// p.ctx) kills an in-flight render AND strands nothing — the daemon
// planner re-requests on the next call or config reload. The first item
// waits for the keys frame (providers do not exist before it).
func (p *pipeline) clipRenderWorker() {
	// Wait once for keys (the render provider needs the per-call TTS
	// identity from the keys frame). p.ctx bounds the wait: a call that
	// dies before keys simply never renders. A nil keysReady (a
	// hand-built pipeline that skipped newPipeline) waits on p.ctx only.
	if p.keysReady != nil {
		select {
		case <-p.keysReady:
		case <-p.ctx.Done():
			p.clipMu.Lock()
			p.clipWorkerUp = false
			p.clipQueue = nil
			p.clipMu.Unlock()
			return
		}
	} else if p.ctx != nil && p.ctx.Err() != nil {
		p.clipMu.Lock()
		p.clipWorkerUp = false
		p.clipQueue = nil
		p.clipMu.Unlock()
		return
	}
	for {
		p.clipMu.Lock()
		if len(p.clipQueue) == 0 {
			p.clipWorkerUp = false
			p.clipMu.Unlock()
			return
		}
		p.clipMu.Unlock()

		// F10: never start a background render Speak while the turn lane
		// is busy — a live synth job is outstanding (head-of-burst Speak or
		// a prefetch) OR audio is actively speaking. Two concurrent Speaks
		// on the same provider starve each other (and the shared qwen3
		// server), and a caller interruption must never queue behind filler.
		// CHECK BEFORE POPPING: on a busy lane the spec stays at the queue
		// head and the worker retires, so the daemon's next clip_render pass
		// re-requests it (renders are idempotent, so deferral loses nothing).
		// A barge-in clear cancels live synths via playGen but never touches
		// clipQueue, so a deferred spec is never lost by an interrupt.
		// liveSynthBusy may settle-sleep under playMu only — never clipMu.
		if p.liveSynthBusy() {
			p.clipMu.Lock()
			p.clipWorkerUp = false
			p.clipMu.Unlock()
			return
		}

		p.clipMu.Lock()
		// Only this worker pops, so the queue is still non-empty here; the
		// re-check keeps the empty-queue exit correct if state ever changes.
		if len(p.clipQueue) == 0 {
			p.clipWorkerUp = false
			p.clipMu.Unlock()
			return
		}
		spec := p.clipQueue[0]
		p.clipQueue = p.clipQueue[1:]
		p.clipMu.Unlock()

		if p.ctx.Err() != nil {
			// Teardown mid-queue: retire the worker without rendering.
			// The clear-on-exit is not needed — the ctx is dead and the
			// pipeline map entry is already gone.
			p.clipMu.Lock()
			p.clipWorkerUp = false
			p.clipMu.Unlock()
			return
		}
		p.renderClip(spec)
	}
}

// liveSynthBusy reports whether the turn lane has live synthesis work: an
// outstanding Speak (head-of-burst or a bounded prefetch in synthJobs) or
// audio actively playing (speaking). Both flags are read under the SINGLE
// playMu regime — no new lock, and no I/O under the lock (C3 lock rule).
// A nil ctx (hand-built pipeline) skips the settle: no live turn can be in
// flight there, so the idle check below is decisive.
func (p *pipeline) liveSynthBusy() bool {
	busy := func() bool {
		p.playMu.Lock()
		active := len(p.synthJobs) > 0 || p.speaking
		p.playMu.Unlock()
		return active
	}()
	if !busy || p.ctx == nil {
		return busy
	}
	// Settle only on the overlap path: a re-request racing a barge-in clear
	// sees the retired job drain from synthJobs within a frame or two and
	// renders this pass instead of waiting for a whole extra planner pass.
	// Teardown ends the settle early; nothing is held across the sleeps.
	for i := 0; i < clipYieldSettleRetries && busy; i++ {
		timer := time.NewTimer(clipYieldSettlePoll)
		select {
		case <-p.ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return true // teardown: caller never renders
		case <-timer.C:
		}
		busy = func() bool {
			p.playMu.Lock()
			active := len(p.synthJobs) > 0 || p.speaking
			p.playMu.Unlock()
			return active
		}()
	}
	return busy
}

// renderClip renders one {key,text} spec into the store. Every failure
// mode (no store, provider construction, Speak, Put) answers
// clip_ready ok:false and NEVER touches the turn lane — a broken clip
// path must never fail a call (branch orchestrator rule).
func (p *pipeline) renderClip(spec voiceframe.ClipSpec) {
	sendOK := func(ok bool) {
		if p.child == nil {
			return
		}
		if err := p.child.emitClipReady(p.ctx, p.ccid, spec.Key, ok); err != nil {
			p.logger.Warn("voicechild: emit clip_ready", "err", err, "call_id", p.ccid, "key", spec.Key)
		}
	}

	if spec.Key == "" {
		p.logger.Warn("voicechild: clip_render entry with empty key")
		sendOK(false)
		return
	}
	store := p.clipStore
	if store == nil {
		p.logger.Warn("voicechild: clip render without store configured")
		sendOK(false)
		return
	}
	// Already warm: idempotent ack, zero synth work.
	if store.Has(spec.Key) {
		p.logger.Debug("voicechild: clip already warm", "call_id", p.ccid, "key", spec.Key)
		sendOK(true)
		return
	}

	tts, err := p.renderProvider()
	if err != nil {
		p.logger.Warn("voicechild: clip render provider failed", "err", err, "call_id", p.ccid, "key", spec.Key)
		sendOK(false)
		return
	}

	// The render Speak runs on a child ctx of p.ctx: pipeline teardown
	// cancels it mid-generation (the providers all honor ctx at the
	// transport), so a pending render can never outlive the call.
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	start := time.Now()
	reader, err := tts.Speak(WithTTSContext(ctx, p.ccid), spec.Text)
	if err != nil {
		if p.ctx.Err() != nil {
			return // teardown: the frame is moot, the planner re-requests
		}
		p.logger.Warn("voicechild: clip render speak failed", "err", err, "call_id", p.ccid, "key", spec.Key)
		sendOK(false)
		return
	}
	if reader == nil {
		p.logger.Warn("voicechild: clip render speak returned nil reader", "call_id", p.ccid, "key", spec.Key)
		sendOK(false)
		return
	}

	cfg := p.ttsConfigSnapshot()
	meta := clipstore.Meta{
		Text:       spec.Text,
		Vendor:     cfg.Vendor,
		Model:      cfg.Model,
		Voice:      cfg.Voice,
		SampleRate: clipCanonicalRate, // A1: the store is canonical 16k
		// Bytes: Put stamps the actual byte count (a truncated stream is
		// discarded as an error, never a corrupt entry — leaf 01 store).
	}
	if err := store.Put(spec.Key, meta, reader); err != nil {
		closeClipReader(p, reader)
		if p.ctx.Err() != nil {
			return // teardown mid-write: half-rendered entry is not stored
		}
		p.logger.Warn("voicechild: clip store put failed", "err", err, "call_id", p.ccid, "key", spec.Key)
		sendOK(false)
		return
	}
	closeClipReader(p, reader)
	p.logger.Info("voicechild: clip rendered", "call_id", p.ccid, "key", spec.Key,
		"render_ms", time.Since(start).Milliseconds(), "sample_rate", clipCanonicalRate)
	sendOK(true)
}

// renderProvider returns the TTS provider the render path speaks through,
// built with the CANONICAL clip rate (adjudication A1) — never the call's
// negotiated rate, so a PCMU@8k call cannot poison the shared store.
// When no real factory is wired (unit-test children) the pipeline's
// installed provider stands in: the fake records Speak calls exactly like
// the turn lane, and its byte output is rate-agnostic by test contract.
func (p *pipeline) renderProvider() (TTSProvider, error) {
	if p.child == nil {
		return nil, errNoTTSProvider
	}
	factory := p.child.ttsFactoryFor()
	if factory == nil {
		p.playMu.Lock()
		tts := p.tts
		p.playMu.Unlock()
		if tts == nil {
			return nil, errNoTTSProvider
		}
		return tts, nil
	}
	cfg := p.ttsConfigSnapshot()
	cfg.SampleRate = clipCanonicalRate
	p.playMu.Lock()
	ttsKey := p.ttsKey
	p.playMu.Unlock()
	tts, err := factory(cfg, ttsKey)
	if err != nil {
		return nil, fmt.Errorf("voicechild: construct clip render provider: %w", err)
	}
	return tts, nil
}

// ttsConfigSnapshot reads the call's stored TTS provider config under the
// playMu regime (the same lock constructProviders uses for the provider
// fields — audit fix 2 single-regime rule). No I/O under the lock.
func (p *pipeline) ttsConfigSnapshot() ProviderConfig {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return p.ttsCfg
}

// emitClipReady sends one clip_ready result frame (C3):
// {"call_id":"<ccid>","event":"clip_ready","key":"<64 hex>","ok":true|false}.
func (c *Child) emitClipReady(ctx context.Context, ccid, key string, ok bool) error {
	okv := ok
	return c.sendFrame(ctx, voiceframe.Frame{
		CallID: ccid,
		Event:  voiceframe.EventClipReady,
		Key:    key,
		OK:     &okv,
	})
}

// decodeTTSRefHash turns the keys frame's tts_ref_hash wire field into
// the refHash bytes Contract C1 feeds ClipKey. The daemon sends
// hex(sha256(reference-audio-bytes)) — or "" for a preset (non-clone)
// voice, which per the leaf-01 convention is the empty-bytes case: ""
// decodes to nil, and ClipKey(nil) == ClipKey([]byte{}) is pinned in
// clipstore/key_test.go. Malformed hex degrades to nil (the non-clone
// identity) rather than failing the call — the worst outcome is a clip
// key that never hits, which falls through to live synth.
func decodeTTSRefHash(s string) []byte {
	if s == "" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}
