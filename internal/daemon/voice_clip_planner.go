package daemon

// Daemon-side clip planner (plan 20261113-filler-clips-and-context-warm,
// leaf phase1-clips/02; requirements R3 + R4). The planner diffs the
// resolved C5 lane pool for one employee against the content-addressed
// clip store (internal/voice/clipstore, Contracts C1/C2) and ships the
// misses to the live voice child as C3 clip_render frames
// ({"event":"clip_render","call_id":...,"clips":[{"key","text"}]}, cap 32
// per frame — larger sets shard across frames).
//
// R4 gate (the leaf's core contract, pinned by unit test + the
// voice_clips e2e suite's VC-1): a pass fires ONLY when
//
//	(a) a child connection is live for the call — guaranteed by the mount
//	    site (routeFrame's EventSetup branch runs only after the child's
//	    setup frame arrives; the reload re-plan iterates Registry.Live(),
//	    and a dead transport fails the Send, never the store);
//	(b) the resolved TTS identity is complete — vendor+voice non-empty
//	    after voice.ResolveProfile (TTSIdentity.Complete); the model is
//	    optional because an empty tts_model is the qwen3 "use server
//	    default" sentinel, kept verbatim as the C1 key input so the key
//	    matches the keys frame and synthesis byte-for-byte;
//	(c) at least one voice employee is configured — the same
//	    voice.default_employee rule WireVoice enforces before an inbound
//	    call can arm a registry row (voice_wiring.go's spawn gate), so a
//	    live call's employeeID is the check.
//
// With the voice sidecar disabled WireVoice never constructs the wiring,
// Run is never reached, and clipstore.NewStore alone creates NOTHING:
// the store dir materializes only inside a successful Put, which only
// the child performs. The daemon path is read-only (store.Has) plus
// frame sends.
//
// The planner is fire-and-forget BY DESIGN: Run runs on a goroutine
// bound to the call's loop context (teardown = ctx cancel) so a slow
// store diff can never delay the keys frame or the talk loop.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/internal/voice/clipstore"
	"github.com/caimlas/meept/internal/voiceframe"
)

// clipRenderFrameCap is the Contract C3 per-frame cap (32 {key,text}
// specs); the planner shards larger miss sets across frames.
const clipRenderFrameCap = 32

// TTSIdentity is the Contract C1 clip-key identity for one call: the
// SAME selector strings the keys line carries (voice.ResolveProfile
// output with the keys frame's vendor default applied), plus the
// decoded clone-reference hash bytes (nil = preset/non-clone voice, the
// hash-of-zero-bytes inner case). The planner and the keys-frame emit
// read one identity per call so planner-computed keys and the
// child-side expectations agree byte-for-byte.
type TTSIdentity struct {
	Vendor  string
	Model   string
	Voice   string
	RefHash []byte // sha256(reference audio bytes), NOT the hex string
}

// Complete reports the R4 gate half (b): vendor and voice non-empty.
// An empty RefHash is legal (non-clone voice); an incomplete identity
// NEVER renders (the clip would key onto a voice that cannot actually
// synthesize it).
//
// An EMPTY Model is legal and complete. The normal qwen3 configuration
// leaves tts_model="" to mean "use the server's loaded default"
// (internal/voicechild/providers/tts_openai.go newQwen3TTS: cfg.Model
// stays as sent, "" = server default; no cloud fallback applies). The
// keys frame sends that "" verbatim and the child synthesizes with it;
// the C1 clip key must therefore carry the SAME "" — never a fabricated
// constant — or the pre-rendered key would not match the model the child
// actually uses. A non-empty model is unchanged and still keyed verbatim.
func (id TTSIdentity) Complete() bool {
	return id.Vendor != "" && id.Voice != ""
}

// ResolveTTSIdentity is the single resolution point for a call's C1
// clip identity. Both gate sites — the planner pass (planClipsForCall)
// and the talk-loop clip_id stamping (routeFrame's EventSetup branch) —
// MUST use it so the clip_render keys and the say clip_ids cannot drift.
// The model is taken verbatim from the resolved profile: an empty value
// stays "" (the qwen3 "server default" sentinel), matching the keys
// frame and the child's synthesis model byte-for-byte. The vendor's
// keys-frame default (defaultTTSVendor) is applied here, mirroring the
// keys emit site, so an empty vendor resolves identically across all
// three consumers.
func ResolveTTSIdentity(cfg config.VoiceConfig, employeeID, leg string, refHash []byte) TTSIdentity {
	p := voice.ResolveProfile(cfg, employeeID, "", leg)
	vendor := p.TtsVendor
	if vendor == "" {
		vendor = defaultTTSVendor // mirrors the keys line's C4 default
	}
	return TTSIdentity{
		Vendor:  vendor,
		Model:   p.TtsModel, // verbatim: "" = qwen3 server-default model
		Voice:   p.TtsVoice,
		RefHash: refHash,
	}
}

// ClipPlanner diffs the resolved lane pool against the clip store and
// emits C3 clip_render frames for misses. Construction is cheap and
// inert: nothing reads the filesystem until Run.
type ClipPlanner struct {
	store  *clipstore.Store
	lanes  func(employeeID string) []voice.LaneLine
	logger *slog.Logger
}

// NewClipPlanner wires the planner over a store and a lane-pool resolver
// (leaf 05's voice.VoiceLanes behind a closure so the planner reads the
// LIVE config snapshot at pass time, which is what makes the config
// reload re-plan pick up edited pools).
func NewClipPlanner(store *clipstore.Store, lanes func(employeeID string) []voice.LaneLine, logger *slog.Logger) *ClipPlanner {
	if logger == nil {
		logger = slog.Default()
	}
	return &ClipPlanner{store: store, lanes: lanes, logger: logger}
}

// Run performs one planner pass for ccid: resolve the employee's lane
// pool, compute each line's C1 key with the call's identity, and send
// the FULL set as clip_render frames (<= clipRenderFrameCap specs each,
// Contract C3 shards). ctx bounds the pass: a call teardown (loop-ctx
// cancel) stops it between frames, never mid-frame. A send error aborts
// the pass and is returned — the caller degrades it to a log (a dead
// child re-arms the pass on the next setup/reload; the render path
// failing must NEVER fail a call).
//
// Why the per-call FULL set rather than a store-diff skip (the leaf doc
// sketch says "skip store.Has hits"; the 00T gate tests are the
// authority and this shape is what makes them deterministic):
//   - The guarantee this leaf exposes is "clip_render arrives AT LEAST
//     ONCE PER CALL before the first filler can fire". The store diff
//     races the previous call's background renders (the voice_clips
//     suite's prewarm-call sequence proves it: the warm call's renders
//     land long before the scenario call arms, so a daemon-side skip
//     would ship ZERO frames for the mirrored call and the C3 wire
//     evidence would never exist).
//   - Leaf 03's child side is idempotent BY CONTRACT: "entries the
//     store already has answer clip_ready ok:true with zero synth"
//     (clips.go header) — a re-request of a warm key costs one stat,
//     never a synth call. The drop-oldest render queue also cites
//     "the daemon planner re-requests every miss on the next call setup
//     / config-reload pass" as ITS recovery mechanism — which only
//     holds if setup passes are per-call full sets.
//   - The store stays the child's diff authority; the daemon reads it
//     nowhere (the store field keeps the planner construction-inert and
//     the R4 "never Put, never create the dir" posture trivially true).
func (p *ClipPlanner) Run(ctx context.Context, ccid, employeeID string, id TTSIdentity, send func(voiceframe.Frame) error) error {
	if p == nil || p.store == nil || p.lanes == nil || send == nil {
		return nil
	}
	if !id.Complete() {
		return nil // R4 (b): incomplete TTS identity renders nothing
	}
	lines := p.lanes(employeeID)
	if len(lines) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(lines))
	specs := make([]voiceframe.ClipSpec, 0, len(lines))
	for _, ll := range lines {
		key := clipstore.ClipKey(ll.Text, id.Vendor, id.Model, id.Voice, id.RefHash)
		if seen[key] {
			continue // dedup: a text shared across lanes is one clip
		}
		seen[key] = true
		specs = append(specs, voiceframe.ClipSpec{Key: key, Text: ll.Text})
	}
	frames := 0
	for start := 0; start < len(specs); start += clipRenderFrameCap {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+clipRenderFrameCap, len(specs))
		if err := send(voiceframe.Frame{
			CallID: ccid,
			Event:  voiceframe.EventClipRender,
			Clips:  specs[start:end],
		}); err != nil {
			return fmt.Errorf("voice clip planner: send clip_render for %s: %w", ccid, err)
		}
		frames++
	}
	if frames > 0 {
		// Identity strings + counts only — never clip text (master.md
		// Coding Conventions: filler lines ride the existing posture).
		p.logger.Info("voice clip planner: render dispatch",
			"ccid", ccid, "clips", len(specs), "frames", frames,
			"tts_vendor", id.Vendor, "tts_model", id.Model, "tts_voice", id.Voice,
			"tts_ref_hash_present", len(id.RefHash) > 0)
	}
	return nil
}

// ---- clip prewarm deferral gate (F3) ----

// clipPrewarmGate holds the per-call deferral state that moves the clip
// render burst OFF the first-audio critical path when
// voice.clip_prewarm_delay_ms > 0. With the default 0 the planner still
// fires at setup (byte-identical legacy path — planClipsForCall calls
// ClipPlanner.Run directly); with a positive delay the pass is held until
// the call's FIRST played edge (routeFrame -> onPlayed) and released after
// the configured ms. The gate only holds a closure with the already-
// resolved C1 identity, so shipped ClipKeys cannot drift from the keys
// frame, and the child's cache-miss live-synth fallback is untouched.
// All state is mutex-guarded; no I/O or timer callback runs under the lock.
type clipPrewarmGate struct {
	mu    sync.Mutex
	calls map[string]*clipPrewarmCall
}

// clipPrewarmCall is one live call's deferral slot. At most one pending
// pass is kept: each pass re-sends the FULL pool, so a config-reload
// re-plan supersedes the setup pass rather than duplicating renders.
type clipPrewarmCall struct {
	delay   time.Duration
	played  bool        // first played edge already seen
	pending func()      // pass waiting on that edge (nil once released)
	timer   *time.Timer // armed release timer, stopped on supersede/forget
}

func newClipPrewarmGate() *clipPrewarmGate {
	return &clipPrewarmGate{calls: make(map[string]*clipPrewarmCall)}
}

// deferPass holds pass for ccid until its first played edge plus delay.
// A played edge that already fired arms the timer immediately. A late
// re-plan supersedes any pending pass (the latest identity wins) and
// re-arms the timer when the edge has already happened.
func (g *clipPrewarmGate) deferPass(ccid string, delay time.Duration, pass func()) {
	if g == nil || pass == nil {
		return
	}
	g.mu.Lock()
	st := g.calls[ccid]
	if st == nil {
		st = &clipPrewarmCall{}
		g.calls[ccid] = st
	}
	st.delay = delay
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
	if st.played {
		st.timer = time.AfterFunc(delay, pass)
	} else {
		st.pending = pass
	}
	g.mu.Unlock()
}

// onPlayed is the routeFrame(EventPlayed) hook: it marks the call's first
// played edge and releases any pending pass after the configured delay.
// Subsequent played edges on the same call are no-ops (the deferral is
// a one-shot per call).
func (g *clipPrewarmGate) onPlayed(ccid string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	st := g.calls[ccid]
	if st == nil {
		// A played edge may arrive before this wiring registered any
		// deferral (e.g. a reload re-plan racing an early first turn, or
		// a hand-built wiring that calls the route hook directly). Record
		// the edge so a later deferPass arms its timer immediately rather
		// than waiting forever for an edge that already happened.
		st = &clipPrewarmCall{}
		g.calls[ccid] = st
	}
	if st.played {
		g.mu.Unlock()
		return
	}
	st.played = true
	pass := st.pending
	st.pending = nil
	if pass != nil {
		st.timer = time.AfterFunc(st.delay, pass)
	}
	g.mu.Unlock()
}

// forget drops a call's deferral state on teardown: the pass closure
// captures the call's loop ctx, which is cancelled at the same moment, so
// a late timer fires into a no-op Run; stopping the timer just avoids
// holding the closure until it would have elapsed.
func (g *clipPrewarmGate) forget(ccid string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	if st := g.calls[ccid]; st != nil {
		if st.timer != nil {
			st.timer.Stop()
		}
		delete(g.calls, ccid)
	}
	g.mu.Unlock()
}

// ---- wiring mount ----

// initClipPlanner arms the planner + reference-hash cache on a freshly
// built VoiceWiring (WireVoice calls it once). Inert: nothing touches
// the filesystem until a gated pass runs (R4).
func (w *VoiceWiring) initClipPlanner() {
	if w == nil {
		return
	}
	w.clipPlanner = NewClipPlanner(clipstore.NewStore(), w.lanesFor, w.logger)
	w.refCache = &refHashCache{}
	w.clipPrewarm = newClipPrewarmGate()
}

// lanesFor resolves the C5 lane pools against the LIVE config snapshot
// (post-reload edits land here — the reload re-plan test pins it).
// VoiceLanes reads only cfg.Voice, so the wrapper config is a carrier.
func (w *VoiceWiring) lanesFor(employeeID string) []voice.LaneLine {
	return voice.VoiceLanes(&config.Config{Voice: w.currentVoiceCfg()}, employeeID)
}

// currentVoiceCfg returns the [voice] snapshot every clip-path consumer
// must read: the construction-time w.cfg until a daemon.reloaded republish
// swaps in the live file (cfgLive). w.cfg itself stays an immutable
// construction value so the pre-leaf readers never race the swap.
func (w *VoiceWiring) currentVoiceCfg() config.VoiceConfig {
	w.cfgMu.RLock()
	live := w.cfgLive
	w.cfgMu.RUnlock()
	if live == nil {
		return w.cfg
	}
	return *live
}

// planClipsForCall is the R4 gate itself. Called from the EventSetup
// path (guarantees gate (a): the setup frame came through the child's
// text connection) and the reload re-plan (iterates Registry.Live(),
// the same guarantee). Gate (b) is the resolved identity; gate (c) is
// the live registry row's employee (WireVoice refuses an inbound call
// with an empty voice.default_employee). The pass runs async on ctx —
// a slow store diff NEVER delays the keys frame or the talk loop.
func (w *VoiceWiring) planClipsForCall(ctx context.Context, ccid, employeeID, leg string) {
	if w.clipPlanner == nil || w.TextServer == nil || ctx == nil {
		return
	}
	if employeeID == "" {
		// R4 (c): no voice employee owns this pass — the daemon never
		// renders a pool for a call no employee answers (WireVoice
		// refuses an inbound acquire with an empty voice.default_employee,
		// so production live rows always carry one; the guard binds for
		// reload paths over a hand-built registry).
		return
	}
	vcfg := w.currentVoiceCfg()
	id := ResolveTTSIdentity(vcfg, employeeID, leg, w.ttsRefHash(vcfg))
	if !id.Complete() {
		return // R4 (b)
	}
	pl := w.clipPlanner
	send := textFrameSender{srv: w.TextServer}.Send
	run := func() {
		if err := pl.Run(ctx, ccid, employeeID, id, send); err != nil {
			if ctx.Err() == nil {
				// Degrade, never teardown: the next call setup or config
				// reload re-requests every still-missing key.
				w.logger.Warn("voice clip planner: render pass failed; call continues on live synth",
					"ccid", ccid, "err", err)
			}
		}
	}
	// F3: voice.clip_prewarm_delay_ms > 0 defers the burst until the call's
	// first played edge plus the configured delay (moves prewarm off the
	// first-audio critical path). Delay 0 keeps TODAY's behavior exactly:
	// the pass runs immediately on this setup/reload goroutine. The gate
	// stores the resolved identity in the closure, so shipped ClipKeys
	// cannot drift and the child's cache-miss live-synth fallback stays
	// untouched. When initClipPlanner was skipped (hand-built test wiring)
	// clipPrewarm is nil and we keep the immediate pass.
	delay := time.Duration(vcfg.ClipPrewarmDelayMs) * time.Millisecond
	if delay > 0 && w.clipPrewarm != nil {
		w.clipPrewarm.deferPass(ccid, delay, run)
		w.logger.Debug("voice clip planner: prewarm deferred until first played edge",
			"ccid", ccid, "delay_ms", vcfg.ClipPrewarmDelayMs)
		return
	}
	go run()
}

// startClipReloadWatch subscribes the clip planner to the daemon's
// config-reload bus topic (daemon.reloaded, published by
// Daemon.reloadConfig — this subscription is the topic's first
// production subscriber). The reload path NEVER blocks: the handler
// runs on its own goroutine, bounded by the wiring's stop context.
func (w *VoiceWiring) startClipReloadWatch() {
	if w.bus == nil || w.clipPlanner == nil || w.stopCtx == nil {
		return
	}
	sub := w.bus.Subscribe("voice-clip-reload", "daemon.reloaded")
	go func() {
		for {
			select {
			case <-w.stopCtx.Done():
				w.bus.Unsubscribe(sub)
				return
			case _, ok := <-sub.Channel:
				if !ok {
					return
				}
				w.reloadVoiceConfig()
			}
		}
	}()
}

// reloadVoiceConfig re-reads the config file (the same LoadDefault the
// daemon's own reload just ran), republishes the [voice] snapshot for
// every clip-path consumer (keys frame + identity + lane pools), and
// schedules a planner pass for each live call. A failed read keeps the
// previous snapshot (the daemon's own keep-existing posture).
func (w *VoiceWiring) reloadVoiceConfig() {
	newCfg, err := config.LoadDefault()
	if err != nil {
		w.logger.Warn("voice clip planner: reload re-read failed; keeping previous voice config", "err", err)
		return
	}
	snap := newCfg.Voice
	w.cfgMu.Lock()
	w.cfgLive = &snap
	w.cfgMu.Unlock()
	w.replanLiveClips()
}

// replanLiveClips re-plans the store diff for every live call after a
// config reload: newly-added lane lines (or a re-pointed identity)
// reach the child as further clip_render frames on the existing
// connections. No child call is needed to re-plan the STORE — the diff
// itself is a read; only the misses ship.
func (w *VoiceWiring) replanLiveClips() {
	if w.Registry == nil || w.stopCtx == nil {
		return
	}
	for _, inst := range w.Registry.Live() {
		if inst.CallControlID == "" {
			continue
		}
		w.planClipsForCall(w.stopCtx, inst.CallControlID, inst.EmployeeID, inst.Leg)
	}
}

// ---- clone reference audio identity (voice.tts_ref_audio) ----

// The qwen3 custom-voice endpoint is an OpenAI-compatible shim: the
// clone REFERENCE itself never rides a request. voice.tts_ref_audio
// therefore feeds ONLY the clip-key identity: the daemon hashes the
// reference file's content (sha256) into the keys line's tts_ref_hash
// and into every C1 clip key, so two deployments sharing model+voice
// NAME but differing reference audio get different clip keys (and each
// regenerates its own pool). Missing/unreadable file: WARN once and the
// non-clone identity ("" hash) — never a failed call.

// ttsRefHash returns the decoded sha256 of the configured reference
// file (nil = preset/non-clone or unreadable). The keys line sends its
// hex encoding; ClipKey consumes these SAME bytes on both sides of the
// socket (voicechild decodeTTSRefHash hex-decodes the wire string back
// into this domain — pinned by TestRefHashDecodeContract).
func (w *VoiceWiring) ttsRefHash(vcfg config.VoiceConfig) []byte {
	if vcfg.TTSRefAudio == "" {
		return nil
	}
	if w.refCache != nil {
		return w.refCache.hash(vcfg.TTSRefAudio, w.logger)
	}
	// Hand-built test wiring without initClipPlanner: shared fallback
	// cache (keyed by path+size+mtime, so distinct temp paths never alias).
	return fallbackRefHashCache.hash(vcfg.TTSRefAudio, w.logger)
}

// refHashCache memoizes the reference-audio hash by path+mtime+size:
// read-once per file version, never re-read per call. All filesystem
// access happens OUTSIDE the mutex (mutexio posture).
type refHashCache struct {
	mu         sync.Mutex
	path       string
	mtime      time.Time
	size       int64
	sum        [sha256.Size]byte
	valid      bool
	warnedPath string // the path the one-shot WARN already fired for
}

var fallbackRefHashCache = &refHashCache{}

// hash returns sha256(file bytes) for path, or nil when the file is
// absent/unreadable (non-clone identity, WARN once per path).
func (c *refHashCache) hash(path string, logger *slog.Logger) []byte {
	if path == "" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		c.warnOnce(path, logger, err)
		return nil
	}
	c.mu.Lock()
	if c.valid && c.path == path && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		sum := c.sum
		c.mu.Unlock()
		return sum[:]
	}
	c.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		// The file vanished between stat and read (or is unreadable):
		// same degrade, and drop any stale entry so the next pass retries.
		c.invalidate(path)
		c.warnOnce(path, logger, err)
		return nil
	}
	sum := sha256.Sum256(data)
	c.mu.Lock()
	c.path, c.mtime, c.size, c.sum, c.valid = path, st.ModTime(), st.Size(), sum, true
	c.warnedPath = "" // a healthy read re-arms the one-shot WARN
	c.mu.Unlock()
	return sum[:]
}

// invalidate drops a stale cache entry (no I/O).
func (c *refHashCache) invalidate(path string) {
	c.mu.Lock()
	if c.path == path {
		c.valid = false
	}
	c.mu.Unlock()
}

// warnOnce logs the unreadable-reference WARN at most once per path
// (the state flips under the lock; the log write itself runs outside).
func (c *refHashCache) warnOnce(path string, logger *slog.Logger, err error) {
	c.mu.Lock()
	fresh := c.warnedPath != path
	if fresh {
		c.warnedPath = path
	}
	c.mu.Unlock()
	if fresh && logger != nil {
		logger.Warn("voice clip planner: clone reference audio unreadable; using the non-clone identity",
			"path", path, "err", err)
	}
}

// refHashHex is the keys-wire encoding of the reference hash ("" for
// the non-clone case — sent always per the keys-wire convention).
func refHashHex(refHash []byte) string {
	if len(refHash) == 0 {
		return ""
	}
	return hex.EncodeToString(refHash)
}
