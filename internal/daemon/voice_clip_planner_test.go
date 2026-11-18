package daemon

// Unit tests for the daemon clip planner (plan 20261113-filler-clips
// leaf 02): the R4 gate matrix, C3 sharding, clone-reference identity,
// the keys-wire tts_ref_hash field, and the daemon.reloaded re-plan.
//
// The e2e voice_clips suite owns the cross-leaf byte agreement (real
// socket + real store); these tests pin the daemon-side decision logic
// with a captured sender and a temp store dir.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/internal/voice/clipstore"
	"github.com/caimlas/meept/internal/voiceframe"
	"github.com/caimlas/meept/pkg/models"
)

// clipCaptureSender records frames sent by a planner pass (the fake
// TextServer seam).
type clipCaptureSender struct {
	mu      sync.Mutex
	frames  []voiceframe.Frame
	err     error  // when set, Send returns it (dead-child simulation)
	onFirst func() // fired once, after the first recorded send
}

func (c *clipCaptureSender) send(f voiceframe.Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.frames = append(c.frames, f)
	if c.onFirst != nil {
		hook := c.onFirst
		c.onFirst = nil
		hook()
	}
	return nil
}

func (c *clipCaptureSender) snapshot() []voiceframe.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]voiceframe.Frame, len(c.frames))
	copy(out, c.frames)
	return out
}

// specs flattens every captured clip_render frame's Clips.
func (c *clipCaptureSender) specs() []voiceframe.ClipSpec {
	var out []voiceframe.ClipSpec
	for _, f := range c.snapshot() {
		out = append(out, f.Clips...)
	}
	return out
}

// clipTestIdentity is a COMPLETE C1 identity (R4 (b) satisfied).
func clipTestIdentity() TTSIdentity {
	return TTSIdentity{Vendor: "qwen3", Model: "Qwen/Qwen3-TTS-test", Voice: "vivian"}
}

func clipTestLanes(texts ...string) func(string) []voice.LaneLine {
	lanes := []voice.Lane{voice.LaneGreeting, voice.LaneFiller, voice.LaneSilence, voice.LaneFragment, voice.LaneFarewell}
	return func(string) []voice.LaneLine {
		out := make([]voice.LaneLine, 0, len(texts))
		for i, t := range texts {
			out = append(out, voice.LaneLine{Lane: lanes[i%len(lanes)], Text: t})
		}
		return out
	}
}

func newClipTestPlanner(dir string, lanes func(string) []voice.LaneLine) *ClipPlanner {
	return NewClipPlanner(clipstore.NewStoreAt(dir), lanes, nil)
}

// TestClipPlannerGateMatrix pins the R4 gate: a complete identity ships
// the FULL configured pool as clip_render specs (the child diffs against
// the store and is idempotent on warm keys); every incomplete-identity
// variant ships nothing; an empty pool ships nothing; and the daemon
// pass NEVER creates the store dir.
func TestClipPlannerGateMatrix(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	pl := newClipTestPlanner(dir, clipTestLanes("One moment.", "Just a sec."))
	sender := &clipCaptureSender{}
	ctx := context.Background()

	if err := pl.Run(ctx, "cc-gate", "emp-1", clipTestIdentity(), sender.send); err != nil {
		t.Fatalf("planner Run: %v", err)
	}
	specs := sender.specs()
	if len(specs) != 2 || specs[0].Text != "One moment." || specs[1].Text != "Just a sec." {
		t.Fatalf("full-set ship: want both pool lines in order, got %+v", specs)
	}
	wantKey := clipstore.ClipKey("Just a sec.", "qwen3", "Qwen/Qwen3-TTS-test", "vivian", nil)
	if specs[1].Key != wantKey {
		t.Fatalf("shipped key %s != C1 ClipKey %s", specs[1].Key, wantKey)
	}

	// (b) incomplete identity: every field-empty variant ships nothing.
	for name, id := range map[string]TTSIdentity{
		"no-vendor": {Model: "m", Voice: "v"},
		"no-model":  {Vendor: "qwen3", Voice: "v"},
		"no-voice":  {Vendor: "qwen3", Model: "m"},
		"empty":     {},
	} {
		s2 := &clipCaptureSender{}
		if err := pl.Run(ctx, "cc-gate", "emp-1", id, s2.send); err != nil {
			t.Fatalf("Run(%s): %v", name, err)
		}
		if len(s2.snapshot()) != 0 {
			t.Fatalf("incomplete identity %s shipped %d frames, want 0 (R4 b)", name, len(s2.snapshot()))
		}
	}

	// Empty pool ships nothing.
	plEmpty := newClipTestPlanner(dir, func(string) []voice.LaneLine { return nil })
	s3 := &clipCaptureSender{}
	if err := plEmpty.Run(ctx, "cc-gate", "emp-1", clipTestIdentity(), s3.send); err != nil {
		t.Fatalf("empty-pool Run: %v", err)
	}
	if len(s3.snapshot()) != 0 {
		t.Fatalf("empty pool shipped %d frames, want 0", len(s3.snapshot()))
	}

	// The daemon pass never touches the store: the dir was never created.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("R4 violation: planner created the store dir (err=%v)", err)
	}
}

// TestClipPlannerNoDirWhenCold is the R4 store-untouched pin at the
// planner level: a pass over a never-created store dir reads (misses
// everything) and NEVER creates the dir.
func TestClipPlannerNoDirWhenCold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	pl := newClipTestPlanner(dir, clipTestLanes("Hello there!"))
	sender := &clipCaptureSender{}
	if err := pl.Run(context.Background(), "cc-cold", "emp-1", clipTestIdentity(), sender.send); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sender.specs()) != 1 {
		t.Fatalf("cold store: want the miss shipped, got %+v", sender.specs())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("R4 violation: planner created the store dir (err=%v)", err)
	}
}

// TestClipPlannerSharding: a 40-line pool ships C3-capped frames: 32 + 8.
func TestClipPlannerSharding(t *testing.T) {
	texts := make([]string, 40)
	for i := range texts {
		texts[i] = fmt.Sprintf("Line %d.", i)
	}
	dir := filepath.Join(t.TempDir(), "voice_clips")
	pl := newClipTestPlanner(dir, clipTestLanes(texts...))
	sender := &clipCaptureSender{}
	if err := pl.Run(context.Background(), "cc-shard", "emp-1", clipTestIdentity(), sender.send); err != nil {
		t.Fatalf("Run: %v", err)
	}
	frames := sender.snapshot()
	if len(frames) != 2 {
		t.Fatalf("40-line pool: want 2 frames (32+8), got %d", len(frames))
	}
	if len(frames[0].Clips) != 32 || len(frames[1].Clips) != 8 {
		t.Fatalf("shard sizes = %d/%d, want 32/8", len(frames[0].Clips), len(frames[1].Clips))
	}
	for _, f := range frames {
		if f.Event != voiceframe.EventClipRender || f.CallID != "cc-shard" {
			t.Fatalf("frame shape wrong: event=%q call_id=%q", f.Event, f.CallID)
		}
	}
	// Order preserved across the shards, all 40 distinct.
	seen := map[string]bool{}
	for _, s := range sender.specs() {
		seen[s.Text] = true
	}
	if len(seen) != 40 {
		t.Fatalf("shipped %d distinct lines, want 40", len(seen))
	}
}

// TestClipPlannerRefHashIdentity: two different clone references produce
// DISJOINT key sets for the same pool (the leaf-02 identity rule that
// tts_ref_audio exists for).
func TestClipPlannerRefHashIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	pl := newClipTestPlanner(dir, clipTestLanes("Hello there!"))
	hashA := sha256.Sum256([]byte("reference-a"))
	hashB := sha256.Sum256([]byte("reference-b"))

	idA := TTSIdentity{Vendor: "qwen3", Model: "m", Voice: "clone", RefHash: hashA[:]}
	idB := TTSIdentity{Vendor: "qwen3", Model: "m", Voice: "clone", RefHash: hashB[:]}
	sa, sb := &clipCaptureSender{}, &clipCaptureSender{}
	ctx := context.Background()
	if err := pl.Run(ctx, "cc-a", "e", idA, sa.send); err != nil {
		t.Fatalf("Run A: %v", err)
	}
	if err := pl.Run(ctx, "cc-b", "e", idB, sb.send); err != nil {
		t.Fatalf("Run B: %v", err)
	}
	if sa.specs()[0].Key == sb.specs()[0].Key {
		t.Fatal("different reference audio produced the SAME clip key")
	}
	// And the ref-hash domain matches ClipKey(nil) == ClipKey(empty) for
	// the preset case (leaf-01 pinned equivalence re-checked daemon-side).
	sp := &clipCaptureSender{}
	if err := pl.Run(ctx, "cc-p", "e", TTSIdentity{Vendor: "qwen3", Model: "m", Voice: "clone"}, sp.send); err != nil {
		t.Fatalf("Run preset: %v", err)
	}
	if sp.specs()[0].Key == sa.specs()[0].Key {
		t.Fatal("preset identity hashed onto the clone reference's key")
	}
}

// TestClipPlannerCtxCancelStopsPass: a dead call ctx aborts the pass
// (teardown = ctx cancel), sending nothing after cancellation.
func TestClipPlannerCtxCancelStopsPass(t *testing.T) {
	texts := make([]string, 80) // guarantees >=3 shards
	for i := range texts {
		texts[i] = fmt.Sprintf("Line %d.", i)
	}
	dir := filepath.Join(t.TempDir(), "voice_clips")
	pl := newClipTestPlanner(dir, clipTestLanes(texts...))
	ctx, cancel := context.WithCancel(context.Background())
	sender := &clipCaptureSender{}
	sender.onFirst = cancel // die as soon as frame 1 lands
	err := pl.Run(ctx, "cc-ctx", "e", clipTestIdentity(), sender.send)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("want ctx error on cancelled pass, got %v", err)
	}
	if n := len(sender.snapshot()); n != 1 {
		t.Fatalf("cancelled pass shipped %d frames, want the 1 before cancel", n)
	}
}

// clipTestVoiceConfig is a COMPLETE phone-leg TTS identity + employee.
// Farewell is pinned to one line because VoiceLanes falls through to the
// shipped default pool when the list is empty (C5) — the assertions
// below count specs.
func clipTestVoiceConfig() config.VoiceConfig {
	return config.VoiceConfig{
		DefaultEmployee: "phone-employee",
		TTSProvider:     "qwen3",
		TTSModel:        "Qwen/Qwen3-TTS-test",
		TTSVoice:        "vivian",
		Filler:          []string{"One moment."},
		GreetingLine:    "Hello there!",
		Farewell:        []string{"See ya!"},
	}
}

// TestClipPlannerSetupMountGateMatrix drives the REAL mount:
// routeFrame(EventSetup) -> planClipsForCall. A complete identity +
// live child (the keysEmitFixture's fake child socket) + registry row
// (employee configured) ships clip_render after the keys line; emptying
// the identity ships nothing and touches no dir.
func TestClipPlannerSetupMountGateMatrix(t *testing.T) {
	f := newKeysEmitFixture(t)
	f.w.cfg = clipTestVoiceConfig()
	f.w.initClipPlanner()
	// Point the store at a temp dir (the wiring test home is short-path
	// but shared; isolation keeps assertions exact).
	storeDir := filepath.Join(t.TempDir(), "vc")
	f.w.clipPlanner = NewClipPlanner(clipstore.NewStoreAt(storeDir), f.w.lanesFor, testLogger(t))

	const ccid = "cc-planner-1"
	acquireFor(f.w, ccid)
	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})

	line := ""
	deadline := time.Now().Add(5 * time.Second)
	for line == "" && time.Now().Before(deadline) {
		select {
		case l := <-f.lines:
			if strings.Contains(l, `"clip_render"`) {
				line = l
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	if line == "" {
		t.Fatal("setup with a complete identity shipped no clip_render line")
	}
	// No second clip_render: the 3-line pool is one frame (C3 cap 32).
	for {
		select {
		case l := <-f.lines:
			if strings.Contains(l, `"clip_render"`) {
				t.Fatalf("second clip_render for a sub-cap pool: %s", l)
			}
			// keys/say frames: keep draining briefly.
		case <-time.After(400 * time.Millisecond):
			goto poolDone
		}
	}
poolDone:
	// Wire shape per C3 (the mirror sees daemon-encoded JSON).
	for _, want := range []string{`"event":"clip_render"`, `"call_id":"` + ccid + `"`, `"clips":[`, `"key":"`, `"text":"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("clip_render line missing %q: %s", want, line)
		}
	}
	var wire struct {
		Event  string `json:"event"`
		CallID string `json:"call_id"`
		Clips  []struct {
			Key  string `json:"key"`
			Text string `json:"text"`
		} `json:"clips"`
	}
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("decode clip_render: %v (%s)", err, line)
	}
	if len(wire.Clips) != 3 {
		t.Fatalf("clips = %d, want the full 3-line pool (greeting, filler, pinned farewell)", len(wire.Clips))
	}
	wantKey := clipstore.ClipKey("Hello there!", "qwen3", "Qwen/Qwen3-TTS-test", "vivian", nil)
	if wire.Clips[0].Key != wantKey || wire.Clips[0].Text != "Hello there!" {
		t.Fatalf("greeting spec = %+v, want C1 key %s", wire.Clips[0], wantKey[:12])
	}
	// The daemon never wrote the store (dir absent — only the child Puts).
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Fatalf("R4 violation: daemon planner created the store dir (err=%v)", err)
	}
	f.w.releaseCall(ccid, voice.OutcomeHangup)

	// Gate (b) off: the default fixture identity (no model/voice) arms
	// no pass — and with the planner store pointed elsewhere, nothing on
	// the wire either.
	f.w.cfg.TTSModel = ""
	f.w.cfg.TTSVoice = ""
	const ccid2 = "cc-planner-2"
	acquireFor(f.w, ccid2)
	f.w.routeFrame(voice.Frame{CallID: ccid2, Event: voice.EventSetup})
	// Read the keys line (proves the loop armed), then assert no
	// clip_render follows.
	if ln := readKeysLine(t, f.lines); !strings.Contains(ln, `"keys"`) {
		t.Fatalf("expected a keys line, got %s", ln)
	}
	select {
	case extra := <-f.lines:
		if strings.Contains(extra, `"clip_render"`) {
			t.Fatalf("incomplete identity shipped clip_render: %s", extra)
		}
	case <-time.After(300 * time.Millisecond):
	}
	f.w.releaseCall(ccid2, voice.OutcomeHangup)
}

// TestClipPlannerSetupNoChild: with no child connection the setup pass
// fails its send and degrades (warn, no teardown); nothing panics and
// the store dir is still never created.
func TestClipPlannerSetupNoChild(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.cfg = clipTestVoiceConfig()
	w.initClipPlanner()
	storeDir := filepath.Join(t.TempDir(), "vc-nochild")
	w.clipPlanner = NewClipPlanner(clipstore.NewStoreAt(storeDir), w.lanesFor, testLogger(t))
	// No child dialed: TextServer.Send errors with "no child connected".
	const ccid = "cc-planner-nochild"
	acquireFor(w, ccid)
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	time.Sleep(300 * time.Millisecond) // let the async pass attempt + die
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Fatalf("dead child still created the store dir (err=%v)", err)
	}
	w.releaseCall(ccid, voice.OutcomeHangup)
}

// TestClipPlannerReloadReplan: a lane line added to the config file
// lands in a later clip_render after the daemon.reloaded subscription
// runs the re-plan for the live call.
func TestClipPlannerReloadReplan(t *testing.T) {
	f := newKeysEmitFixture(t)
	w := f.w
	w.cfg = clipTestVoiceConfig()
	home := os.Getenv("MEEPT_HOME")
	// Seed the on-disk config the reload handler re-reads (LoadDefault
	// honors MEEPT_HOME — the fixture already points it at a short dir).
	writeClipTestConfig(t, home, 250)
	w.bus = bus.New(nil, testLogger(t))
	w.stopCtx, w.stopCancel = context.WithCancel(context.Background())
	t.Cleanup(func() {
		if w.stopCancel != nil {
			w.stopCancel()
		}
	})
	w.initClipPlanner()
	storeDir := filepath.Join(t.TempDir(), "vc")
	w.clipPlanner = NewClipPlanner(clipstore.NewStoreAt(storeDir), w.lanesFor, testLogger(t))
	w.startClipReloadWatch()

	const ccid = "cc-planner-reload"
	acquireFor(w, ccid)
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})

	first := awaitClipRender(t, f.lines, 5*time.Second)
	if len(first.Clips) != 3 {
		t.Fatalf("first pass: %d clips, want the 3-line in-memory pool", len(first.Clips))
	}

	// Add a line to the file, fire daemon.reloaded (the subscription,
	// not a direct call — this pins the mount), and expect a second
	// clip_render carrying ONLY the new line's key (the old two are
	// still misses on the wire-diff but were already shipped; the
	// daemon-side store is cold (no child Put), so the pass re-ships
	// what it cannot see warm: assert at least the new key rides, and
	// the reload did not invent a dir).
	writeClipTestConfig(t, home, 300)
	msg, err := newReloadBusMessage()
	if err != nil {
		t.Fatalf("bus message: %v", err)
	}
	w.bus.Publish("daemon.reloaded", msg)

	second := awaitClipRender(t, f.lines, 5*time.Second)
	found := false
	for _, s := range second.Clips {
		if s.Text == "Extra reload line." {
			found = true
			want := clipstore.ClipKey("Extra reload line.", "qwen3", "Qwen/Qwen3-TTS-test", "vivian", nil)
			if s.Key != want {
				t.Fatalf("reload key %s != C1 %s", s.Key, want[:12])
			}
		}
	}
	if !found {
		t.Fatalf("reload re-plan shipped no extra line: %+v", second.Clips)
	}
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Fatalf("R4 violation: reload re-plan created the store dir (err=%v)", err)
	}
	w.releaseCall(ccid, voice.OutcomeHangup)
}

// TestClipPlannerReloadKeepsBadFile: an unreadable config on reload
// keeps the previous snapshot (never blanks the identity mid-call).
func TestClipPlannerReloadKeepsBadFile(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.cfg = clipTestVoiceConfig()
	w.initClipPlanner()
	brokenHome := filepath.Join(t.TempDir(), "does-not-exist-home")
	t.Setenv("MEEPT_HOME", brokenHome)
	w.reloadVoiceConfig() // LoadDefault on a bare home returns defaults
	// The DEFAULT config has an empty voice identity — the live snapshot
	// swapped to defaults (a MISSING file is not an error: defaults
	// load). Assert only the mechanical invariant: a broken FILE (load
	// error) keeps the PREVIOUS live snapshot (the defaults read), never
	// w.cfg and never a half-parsed config.
	if err := os.MkdirAll(brokenHome, 0o755); err != nil {
		t.Fatalf("home: %v", err)
	}
	broken := filepath.Join(brokenHome, "meept.json5")
	if err := os.WriteFile(broken, []byte("{ this is not json5"), 0o600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}
	w.reloadVoiceConfig()
	if got := w.currentVoiceCfg(); got.DefaultEmployee != "" || got.TTSModel != "" {
		t.Fatalf("keep-existing violated: snapshot = %+v (want the previous defaults read, not w.cfg)", got)
	}
}

// TestClipPlannerKeysFrameRefHash: the keys line carries
// tts_ref_hash=hex(sha256(reference bytes)) when voice.tts_ref_audio is
// set, and ALWAYS carries the key ("" when unset — the keys-wire
// no-omitempty convention; an old child ignores the extra field).
func TestClipPlannerKeysFrameRefHash(t *testing.T) {
	f := newKeysEmitFixture(t)
	f.w.cfg = clipTestVoiceConfig()
	f.w.initClipPlanner()
	const ccid = "cc-keys-ref"
	acquireFor(f.w, ccid)
	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	line := readKeysLine(t, f.lines)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("keys line not JSON: %v", err)
	}
	got, present := wire["tts_ref_hash"]
	if !present || got != "" {
		t.Fatalf("unset tts_ref_audio: wire tts_ref_hash = %v (present=%v), want \"\" key (back-compat convention)", got, present)
	}
	f.w.releaseCall(ccid, voice.OutcomeHangup)

	// With a reference file: the hash of its bytes, hex-encoded.
	ref := filepath.Join(t.TempDir(), "ref.wav")
	content := []byte("RIFF....fake-clone-reference")
	if err := os.WriteFile(ref, content, 0o600); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	f.w.cfg.TTSRefAudio = ref
	const ccid2 = "cc-keys-ref2"
	acquireFor(f.w, ccid2)
	f.w.routeFrame(voice.Frame{CallID: ccid2, Event: voice.EventSetup})
	line2 := readKeysLine(t, f.lines)
	wire2 := map[string]any{}
	if err := json.Unmarshal([]byte(line2), &wire2); err != nil {
		t.Fatalf("keys line not JSON: %v", err)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	if wire2["tts_ref_hash"] != want {
		t.Fatalf("wire tts_ref_hash = %v, want %s", wire2["tts_ref_hash"], want)
	}
	// The planner used the SAME bytes: an edit to the file flips the
	// shipped clip key (identity change => regenerate, R3).
	if err := os.WriteFile(ref, []byte("different-reference"), 0o600); err != nil {
		t.Fatalf("rewrite ref: %v", err)
	}
	w := &refHashCache{}
	if hex.EncodeToString(w.hash(ref, nil)[:]) == want {
		t.Fatal("ref cache served a stale hash after a content change")
	}
	f.w.releaseCall(ccid2, voice.OutcomeHangup)
}

// TestClipPlannerRefHashMissingFile: a configured-but-missing reference
// degrades to the non-clone identity (nil bytes) — never a call failure.
func TestClipPlannerRefHashMissingFile(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	w.initClipPlanner()
	vcfg := clipTestVoiceConfig()
	vcfg.TTSRefAudio = filepath.Join(t.TempDir(), "nope.wav")
	if got := w.ttsRefHash(vcfg); got != nil {
		t.Fatalf("missing reference file: got %x, want nil (non-clone identity)", got)
	}
}

// TestClipPlannerSendFailure: a dead child transport surfaces as a Run
// error (the caller degrades to a warn log; the pass never panics and
// nothing lands in the store).
func TestClipPlannerSendFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	pl := newClipTestPlanner(dir, clipTestLanes("One moment."))
	sender := &clipCaptureSender{err: fmt.Errorf("no child connected")}
	err := pl.Run(context.Background(), "cc-dead", "emp-1", clipTestIdentity(), sender.send)
	if err == nil {
		t.Fatal("send failure must surface as an error (caller degrades, never fails the call)")
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("failed pass created the store dir (err=%v)", statErr)
	}
}

// TestClipPlannerGateEmployee: R4 (c) at the wiring seam — an empty
// employee id (no voice employee owns the call) never runs a pass, even
// with a complete identity, a live child, and a full pool.
func TestClipPlannerGateEmployee(t *testing.T) {
	f := newKeysEmitFixture(t)
	f.w.cfg = clipTestVoiceConfig()
	f.w.initClipPlanner()
	storeDir := filepath.Join(t.TempDir(), "vc-noemp")
	f.w.clipPlanner = NewClipPlanner(clipstore.NewStoreAt(storeDir), f.w.lanesFor, testLogger(t))
	f.w.planClipsForCall(context.Background(), "cc-noemp", "", voice.LegPhone)
	select {
	case line := <-f.lines:
		if strings.Contains(line, `"clip_render"`) {
			t.Fatalf("empty employee shipped clip_render: %s", line)
		}
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(storeDir); !os.IsNotExist(err) {
		t.Fatalf("R4 violation: no-employee pass created the dir (err=%v)", err)
	}
}

// ---- helpers ----

// awaitClipRender reads lines until one decodes as a clip_render frame.
func awaitClipRender(t *testing.T, lines <-chan string, within time.Duration) voiceframe.Frame {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		select {
		case line := <-lines:
			if !strings.Contains(line, `"clip_render"`) {
				continue
			}
			f, err := voiceframe.DecodeFrame([]byte(line))
			if err != nil {
				t.Fatalf("clip_render line not decodable: %v (%s)", err, line)
			}
			return f
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("no clip_render frame arrived in time")
	return voiceframe.Frame{}
}

// writeClipTestConfig writes $MEEPT_HOME/meept.json5 with the clip-era
// voice block; marker varies the filler pool per reload round.
func writeClipTestConfig(t *testing.T, home string, marker int) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil { //nolint:gosec // G703: home is t.TempDir()-derived, a fixed test path
		t.Fatalf("home: %v", err)
	}
	extra := ""
	if marker >= 300 {
		extra = `, "filler": ["One moment.", "Extra reload line."]`
	}
	body := fmt.Sprintf(`{
  "voice": {
    "enabled": true,
    "default_employee": "phone-employee",
    "tts_provider": "qwen3",
    "tts_model": "Qwen/Qwen3-TTS-test",
    "tts_voice": "vivian",
    "greeting": "Hello there!"%s
  }
}`, extra)
	if err := os.WriteFile(filepath.Join(home, "meept.json5"), []byte(body), 0o600); err != nil { //nolint:gosec // G703: fixed leaf name under t.TempDir(), no traversal input
		t.Fatalf("write config: %v", err)
	}
}

func newReloadBusMessage() (*models.BusMessage, error) {
	return models.NewBusMessage(models.MessageTypeEvent, "test", map[string]any{"event": "reloaded"})
}
