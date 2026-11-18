//go:build e2e

// Suite voice_clips scenarios (gate leaf 00T, Task 3). Frozen contracts
// quoted in helpers_test.go (C1/C2/C3/C5 from
// docs/plans/20261113-filler-clips-and-context-warm/master.md).
//
// Hermetic shape: scratch daemon (throwaway MEEPT_HOME), fake Telnyx
// webhook signing, loopback fake qwen3 TTS endpoint (configurable synth
// delay), and the suite-local clipchild (testdata/clipchild) which
// mirrors EVERY daemon->child C1 line raw + redacted so the exact wire
// shapes (clip_render clips, say clip_id) are observable.
//
// EXPECTED RED for the gate (pre-impl): every awaitMirrored on
// "clip_render" / clip_id-bearing say lines fails with
// "mirrored lines never carried ..." — an implementation reason (leaf 02
// emits no clip_render; leaf 04 sends no clip_id), NOT a compile or
// harness failure.
package voiceclips

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// c1Key mirrors the FROZEN C1 formula independently (stdlib crypto only)
// — same mirror as internal/voice/clipstore/clipkey_contract_test.go, so
// the suite never trusts the implementation to check the implementation.
func c1Key(t *testing.T, text, vendor, model, voice string) string {
	t.Helper()
	return clipGateKeyViaStdlib(text, vendor, model, voice)
}

// clipGatePool is the configured five-lane pool every scenario shares
// (farewell custom line, so the store-set equality check exercises the
// list key too).
var clipGatePool = map[string]any{
	"greeting":               "Hello there!",
	"filler":                 []string{"One moment.", "Just a sec."},
	"silence_prompt_lines":   []string{"Still there?"},
	"fragment_clarify_lines": []string{"Come again?"},
	"farewell":               []string{"See ya!"},
	"filler_grace_ms":        500,
	"supervisor_enabled":     false,
	"turn_confirm_ms":        0,
}

func clipGateIdentity() (vendor, model, voice string) {
	return "qwen3", "Qwen/Qwen3-TTS-test", "vivian"
}

func clipGateAllPoolLines() []string {
	return []string{"Hello there!", "One moment.", "Just a sec.", "Still there?", "Come again?", "See ya!"}
}

// VC-1 (R4): sidecar disabled => $MEEPT_HOME/voice_clips is NEVER
// created. The daemon boots, serves health, and zero clip machinery runs.
func TestVoiceClipsSidecarDisabledNoDir(t *testing.T) {
	fake := newFakeTelnyx(t)
	vendor, model, voice := clipGateIdentity()
	opts := []harness.StartOption{
		harness.WithConfigHook(func(cfg map[string]any) {
			voiceCfg := map[string]any{
				"enabled":      false, // the gate under test
				"tts_provider": vendor,
				"tts_model":    model,
				"tts_voice":    voice,
				"greeting":     "Hello there!",
				"filler":       []string{"One moment."},
			}
			cfg["voice"] = voiceCfg
		}),
		harness.WithExtraEnv(fake.extraEnv()),
	}
	for k, v := range fake.secretsOverlay() {
		opts = append(opts, harness.WithConfigOverlay(map[string]any{k: v}))
	}
	s := harness.Start(t, opts...)
	harness.WaitFor(t, 30*time.Second, "daemon healthy (voice disabled)", s.HealthOK)

	// Settle window: any store-touching path (planner, store, child
	// spawn) would have run or logged by now.
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(clipsDir(s)); !os.IsNotExist(err) {
		t.Fatalf("R4 violation: voice_clips dir exists with the sidecar disabled (err=%v, dir=%s)", err, clipsDir(s))
	}
	// Belt: a store read path never wrote through a side effect either.
	entries, _ := os.ReadDir(s.MeeptHome)
	for _, e := range entries {
		if e.Name() == "voice_clips" {
			t.Fatalf("voice_clips appeared under MEEPT_HOME: %v", e.Name())
		}
	}
}

// VC-2 (C5 render set / R3): configured pools => after a call the store
// carries a clip file for every pool line and NO others; the child saw
// at least one C3 clip_render frame whose {key,text} specs match C1
// exactly (byte-for-byte cross-leaf agreement, observed on the REAL
// socket).
func TestVoiceClipsPoolsRendered(t *testing.T) {
	fake := newFakeTelnyx(t)
	tts := newFakeTTS(t)
	s, ctrlPath := bootClipStack(t, fake, tts, clipGatePool)
	const ccid = "cc-clips-1"
	prewarmCall(t, s, fake, ctrlPath, "cc-clips-warm", ccid)

	// 1. The planner shipped at least one C3 clip_render frame, with the
	// frozen wire shape: {"event":"clip_render","call_id":...,"clips":
	// [{"key":"<64 hex>","text":...}]}.
	renderIdx := fake.awaitMirrored(ccid, `"clip_render"`, 30*time.Second)
	line := fake.mirroredRawAt(renderIdx)
	for _, want := range []string{`"event":"clip_render"`, `"call_id":"` + ccid + `"`, `"clips":[`, `"key":"`, `"text":"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("clip_render line missing %q shape element: %s", want, line)
		}
	}

	// 2. Wait until every pool line has a stored clip (renders are
	// background; the clipchild answers clip_ready ok:true per spec).
	vendor, model, voice := clipGateIdentity()
	wantKeys := map[string]string{} // key -> text
	for _, text := range clipGateAllPoolLines() {
		wantKeys[c1Key(t, text, vendor, model, voice)] = text
	}
	deadline := time.Now().Add(60 * time.Second)
	var have map[string]bool
	for time.Now().Before(deadline) {
		have, _ = storedClipKeys(s)
		if len(have) == len(wantKeys) && allPresent(have, wantKeys) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for key, text := range wantKeys {
		if !have[key] {
			t.Errorf("pool line %q never rendered to the store (missing key %s)", text, key[:12])
		}
	}
	// 3. And NO others (finite configured pools only — R2).
	for key := range have {
		if _, ok := wantKeys[key]; !ok {
			t.Errorf("store carries an unexpected clip key %s (pool lines only, R2)", key)
		}
	}
	// 4. Meta pairs exist for every clip (C2: .json beside .pcm).
	for key := range wantKeys {
		if _, err := os.Stat(filepath.Join(clipsDir(s), key+".json")); err != nil {
			t.Errorf("meta json missing for rendered key %s: %v", key[:12], err)
		}
	}
	fake.deliverHangup(s, ccid)
}

// VC-3 (R3 invalidation): a voice identity change (tts_voice edit +
// SIGHUP reload) => NEW keys appear (pool re-rendered for the new
// identity), the OLD keys survive untouched, and the turn path still
// works (keys line carries the new voice; a say frame still flows).
func TestVoiceClipsIdentityChangeRegenerates(t *testing.T) {
	fake := newFakeTelnyx(t)
	tts := newFakeTTS(t)
	s, ctrlPath := bootClipStack(t, fake, tts, clipGatePool)
	const ccid1 = "cc-ident-1"
	prewarmCall(t, s, fake, ctrlPath, "cc-ident-warm", ccid1)
	vendor, model, voice := clipGateIdentity()
	wantOld := map[string]string{}
	for _, text := range clipGateAllPoolLines() {
		wantOld[c1Key(t, text, vendor, model, voice)] = text
	}
	harness.WaitFor(t, 60*time.Second, "first-identity pool fully rendered", func() bool {
		have, _ := storedClipKeys(s)
		return allPresent(have, wantOld)
	})
	fake.deliverHangup(s, ccid1)
	awaitLogLine(t, s, "voice call ended", 20*time.Second)

	// Edit tts_voice and reload (SIGHUP, the config-reload suite's path).
	cfgPath := filepath.Join(s.MeeptHome, "meept.json5")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	replaced := regexp.MustCompile(`("tts_voice":\s*)"vivian"`).ReplaceAllString(string(raw), `${1}"ryan"`)
	if replaced == string(raw) {
		t.Fatal("config edit found no tts_voice=\"vivian\" to replace")
	}
	if err := os.WriteFile(cfgPath, []byte(replaced), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := syscall.Kill(s.Daemon.Pid(), syscall.SIGHUP); err != nil {
		t.Fatalf("SIGHUP: %v", err)
	}
	harness.WaitFor(t, 30*time.Second, "health ok after reload", s.HealthOK)

	// Second call under the NEW identity: keys line must carry
	// tts_voice=ryan (mirrored), and the new keys must render.
	const ccid2 = "cc-ident-2"
	fake.deliverCallInitiated(s, ccid2, "incoming")
	fake.dialMedia(s, ccid2)
	awaitLogLine(t, s, "voice media: start frame", 20*time.Second)
	fake.armCall(ccid2)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 20*time.Second)
	keysLine := fake.awaitMirrored(ccid2, `"tts_voice":"ryan"`, 30*time.Second)
	_ = keysLine

	wantNew := map[string]string{}
	for _, text := range clipGateAllPoolLines() {
		wantNew[c1Key(t, text, vendor, model, "ryan")] = text
	}
	harness.WaitFor(t, 90*time.Second, "new-identity pool fully rendered", func() bool {
		have, _ := storedClipKeys(s)
		return allPresent(have, wantNew)
	})
	// Old identity files survive (invalidation is additive, never
	// destructive — C1 key change => regenerate, not rewrite).
	have, _ := storedClipKeys(s)
	for key := range wantOld {
		if !have[key] {
			t.Errorf("old-identity clip %s disappeared after the voice change", key[:12])
		}
	}
	// Turn still works: a say frame flowed for the new call (the
	// greeting, filler-class, is guaranteed once the loop arms).
	fake.awaitMirrored(ccid2, `"event":"say"`, 30*time.Second)
	fake.deliverHangup(s, ccid2)
}

// VC-4 (R1 latency): with the store warm, filler/greeting audio comes
// from the store, not the synth lane. The fake TTS endpoint is slowed to
// 2 s/request AFTER warming; the child's cached playback completes in
// <300 ms and the endpoint receives ZERO requests for that line.
func TestVoiceClipsCachedFillerUnder300msVsSlowTTS(t *testing.T) {
	fake := newFakeTelnyx(t)
	tts := newFakeTTS(t)
	s, ctrlPath := bootClipStack(t, fake, tts, clipGatePool)
	const ccid1 = "cc-lat-1"
	prewarmCall(t, s, fake, ctrlPath, "cc-lat-warm", ccid1)

	vendor, model, voice := clipGateIdentity()
	warm := map[string]string{}
	for _, text := range clipGateAllPoolLines() {
		warm[c1Key(t, text, vendor, model, voice)] = text
	}
	harness.WaitFor(t, 60*time.Second, "pool warmed", func() bool {
		have, _ := storedClipKeys(s)
		return allPresent(have, warm)
	})
	fake.deliverHangup(s, ccid1)
	awaitLogLine(t, s, "voice call ended", 20*time.Second)

	// Slow the synth lane to 2 s per request. A live-synth greeting/filler
	// would now measurably block; a cached clip must not touch it.
	tts.setDelay(2 * time.Second)
	before := tts.totalCount()

	const ccid2 = "cc-lat-2"
	fake.deliverCallInitiated(s, ccid2, "incoming")
	fake.dialMedia(s, ccid2)
	awaitLogLine(t, s, "voice media: start frame", 20*time.Second)
	fake.armCall(ccid2)

	// The greeting say must now carry clip_id (leaf 04) — its absence is
	// the implementation gap this gate pins.
	sayIdx := fake.awaitMirrored(ccid2, `"clip_id":"`, 30*time.Second)
	sayLine := fake.mirroredRawAt(sayIdx)
	if !strings.Contains(sayLine, `"event":"say"`) {
		t.Fatalf("clip_id-bearing line is not a say frame: %s", sayLine)
	}
	greetKey := c1Key(t, "Hello there!", vendor, model, voice)
	if !strings.Contains(sayLine, greetKey) {
		t.Fatalf("greeting say clip_id != C1 key of %q (%s): %s", "Hello there!", greetKey[:12], sayLine)
	}

	// Child-side proof: the SAME line's playback reports source=clip with
	// sub-300 ms completion despite the 2 s synth lane.
	playLog := fake.awaitChildLog(ccid2, `source=clip key="`+greetKey+`"`, 30*time.Second)
	var ms int
	if _, err := fmt.Sscanf(regexp.MustCompile(`ms=(\d+)`).FindString(playLog), "ms=%d", &ms); err != nil {
		t.Fatalf("cannot parse ms from child log %q: %v", playLog, err)
	}
	if ms >= 300 {
		t.Fatalf("cached filler playback took %d ms (want <300; a 2 s synth lane proves the 300 ms bound discriminates store-vs-synth)", ms)
	}
	// And the endpoint saw no request in the cached window.
	if after := tts.totalCount(); after != before {
		t.Fatalf("slow TTS endpoint served %d extra request(s) during cached playback (want 0 — PCM came from the store)", after-before)
	}

	fake.deliverHangup(s, ccid2)
}

// VC-5 (R5): caller says "bye" => a farewell-pool line is spoken (as a
// clip-bearing say) BEFORE the C1 end frame, and the release fan-out
// still lands exactly once.
func TestVoiceClipsFarewellAudioPrecedesEndFrame(t *testing.T) {
	fake := newFakeTelnyx(t)
	tts := newFakeTTS(t)
	s, ctrlPath := bootClipStack(t, fake, tts, clipGatePool)
	const ccid1 = "cc-fare-1"
	prewarmCall(t, s, fake, ctrlPath, "cc-fare-warm", ccid1)
	vendor, model, voice := clipGateIdentity()
	warm := map[string]string{}
	for _, text := range clipGateAllPoolLines() {
		warm[c1Key(t, text, vendor, model, voice)] = text
	}
	harness.WaitFor(t, 60*time.Second, "farewell pool warmed", func() bool {
		have, _ := storedClipKeys(s)
		return allPresent(have, warm)
	})
	fake.deliverHangup(s, ccid1)
	awaitLogLine(t, s, "voice call ended", 20*time.Second)

	// Live call 2: the caller's final utterance is a bye.
	const ccid2 = "cc-fare-2"
	fake.deliverCallInitiated(s, ccid2, "incoming")
	fake.dialMedia(s, ccid2)
	awaitLogLine(t, s, "voice media: start frame", 20*time.Second)
	fake.armCall(ccid2)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 20*time.Second)

	fake.childPrompt(ccid2, "bye")

	// Farewell say (one of the pool lines, clip_id-bearing) BEFORE the
	// end frame. Pre-impl this await times out: RED (leaf 04).
	farewellSayIdx := -1
	for _, line := range []string{"See ya!"} {
		if idx := fake.awaitMirroredContainsOr(ccid2, `"event":"say"`, line, 45*time.Second); idx >= 0 {
			farewellSayIdx = idx
			break
		}
	}
	if farewellSayIdx < 0 {
		t.Fatalf("no farewell say frame carried the pool line before teardown (R5)")
	}
	sayLine := fake.mirroredRawAt(farewellSayIdx)
	if !strings.Contains(sayLine, `"clip_id":"`) {
		t.Fatalf("farewell say must carry clip_id (leaf 04): %s", sayLine)
	}

	endIdx := fake.awaitMirrored(ccid2, `"event":"end"`, 30*time.Second)
	if farewellSayIdx >= endIdx {
		t.Fatalf("ordering violation: farewell say at mirror #%d, end frame at #%d — goodbye audio must precede end", farewellSayIdx, endIdx)
	}

	// Exactly-once release fan-out (AGENTS.md hangup invariant): one
	// "voice call ended" for ccid2 in the daemon log.
	fake.deliverHangup(s, ccid2)
	awaitLogLine(t, s, "voice call ended", 20*time.Second)
	logData, _ := os.ReadFile(s.Daemon.LogPath())
	n := 0
	for _, l := range strings.Split(string(logData), "\n") {
		if strings.Contains(l, "voice call ended") && strings.Contains(l, ccid2) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("release fan-out logged %d 'voice call ended' lines for %s, want exactly 1", n, ccid2)
	}
}

// VC-6 (R4 model-default): an empty tts_model — the normal qwen3 "use
// the server's loaded default" config — must NOT silently disable clip
// prewarm. The keys line carries tts_model="", a clip_render frame still
// ships, and its C1 keys are computed with that SAME empty model (the
// model the child synthesizes with), byte-for-byte.
func TestVoiceClipsEmptyModelStillPrewarms(t *testing.T) {
	fake := newFakeTelnyx(t)
	tts := newFakeTTS(t)
	// bootClipStack overlays the pool map last; forcing tts_model="" here
	// overrides the suite's default explicit model.
	pool := make(map[string]any, len(clipGatePool))
	for k, v := range clipGatePool {
		pool[k] = v
	}
	pool["tts_model"] = ""
	s, ctrlPath := bootClipStack(t, fake, tts, pool)
	const ccid = "cc-empty-model"
	prewarmCall(t, s, fake, ctrlPath, "cc-empty-model-warm", ccid)

	// The keys line must carry tts_model="" verbatim (server default).
	keysIdx := fake.awaitMirrored(ccid, `"tts_model":""`, 30*time.Second)
	if keysIdx < 0 {
		t.Fatal("mirrored keys line never carried an empty tts_model")
	}

	// A clip_render MUST still ship — this is the regression: the old R4
	// gate treated the empty model as incomplete and rendered nothing.
	renderIdx := fake.awaitMirrored(ccid, `"clip_render"`, 30*time.Second)
	line := fake.mirroredRawAt(renderIdx)

	// The greeting clip key must be built with the SAME empty model the
	// keys line sent (and the child synthesizes with).
	vendor, _, voice := clipGateIdentity() // vendor=qwen3, voice=vivian
	wantKey := c1Key(t, "Hello there!", vendor, "", voice)
	if !strings.Contains(line, wantKey) {
		t.Fatalf("empty-model clip_render line missing C1 key %s (model=\"\"): %s", wantKey, line)
	}

	// And it actually renders into the store with that key.
	harness.WaitFor(t, 60*time.Second, "empty-model greeting clip rendered", func() bool {
		have, _ := storedClipKeys(s)
		return have[wantKey]
	})
	fake.deliverHangup(s, ccid)
}

// ---- suite-local assertion helpers ----

func allPresent(have map[string]bool, want map[string]string) bool {
	for key := range want {
		if !have[key] {
			return false
		}
	}
	return len(have) >= len(want)
}

func (f *fakeTelnyx) mirroredRawAt(idx int) string {
	f.childMu.Lock()
	defer f.childMu.Unlock()
	if idx < 0 || idx >= len(f.frames) {
		f.t.Fatalf("mirror index %d out of range", idx)
	}
	return f.frames[idx].Raw
}

// awaitMirroredContainsOr polls for a ccid line containing substr1 AND
// any of the alternatives; returns the ordered mirror index or -1 on
// timeout (never fails — ordering scenarios decide from the result).
func (f *fakeTelnyx) awaitMirroredContainsOr(ccid, must string, orSub string, within time.Duration) int {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		f.childMu.Lock()
		for i, ln := range f.frames {
			if ln.CallID == ccid && strings.Contains(ln.Raw, must) && strings.Contains(ln.Raw, orSub) {
				f.childMu.Unlock()
				return i
			}
		}
		f.childMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	return -1
}
