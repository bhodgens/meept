package voicechild

// Gate tests T1 (feature F1): the extended C1 keys frame (master.md C1,
// leaf 02) carries vendor/model/base_url/voice for both legs, and the
// child stores them on the pipeline as the provider config the C3
// factories consume.
//
// RED style note (deliberate deviation from compile-level RED): the
// missing contract symbols are probed through RUNTIME REFLECTION so this
// package stays compilable and the sibling gate tests (T4/T5, same
// package) remain runnable today. Every missing symbol is named in its
// failure message. Missing symbols (leaf 02):
//   keysWire.{STTVendor,STTModel,STTBaseURL,TTSVendor,TTSModel,TTSBaseURL,TTSVoice}
//   pipeline.sttCfg / pipeline.ttsCfg
//   STTFactory / TTSFactory first parameter becomes ProviderConfig (C3)

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/voiceframe"
)

// keysWireWant maps the C1 contract Go field names to their exact json
// wire tags (contract C1, all seven new fields OPTIONAL, absent = "").
var keysWireWant = map[string]string{
	"STTVendor":  "stt_vendor",
	"STTModel":   "stt_model",
	"STTBaseURL": "stt_base_url",
	"TTSVendor":  "tts_vendor",
	"TTSModel":   "tts_model",
	"TTSBaseURL": "tts_base_url",
	"TTSVoice":   "tts_voice",
}

// pipelineCfgString reads one string field off p.sttCfg / p.ttsCfg via
// reflection (the fields do not exist until leaf 02 lands). ok=false means
// the contract symbol is missing; the caller names it.
func pipelineCfgString(p *pipeline, cfgField, name string) (string, bool) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	v := reflect.ValueOf(p).Elem().FieldByName(cfgField)
	if !v.IsValid() {
		return "", false
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

func TestKeysFrameExtendedFields(t *testing.T) {
	// Part 1: keysWire carries all seven C1 fields with exact json tags.
	wireType := reflect.TypeOf(keysWire{})
	var missing []string
	for name, tag := range keysWireWant {
		f, ok := wireType.FieldByName(name)
		if !ok {
			missing = append(missing, "keysWire."+name)
			continue
		}
		if got := f.Tag.Get("json"); got != tag {
			t.Errorf("keysWire.%s json tag = %q, want %q (contract C1)", name, got, tag)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("missing C1 symbols (leaf 02): %s - extended keys frame cannot be decoded", strings.Join(missing, ", "))
	}

	// Part 2: factory signatures take the full ProviderConfig (contract C3).
	for _, ft := range []struct {
		name string
		typ  reflect.Type
	}{
		{"STTFactory", reflect.TypeOf((*STTFactory)(nil)).Elem()},
		{"TTSFactory", reflect.TypeOf((*TTSFactory)(nil)).Elem()},
	} {
		if ft.typ.NumIn() != 2 {
			t.Fatalf("%s takes %d params, want 2 (contract C3: cfg + key)", ft.name, ft.typ.NumIn())
		}
		in0 := ft.typ.In(0)
		if in0.Kind() != reflect.Struct || in0.Name() != "ProviderConfig" {
			t.Fatalf("missing contract C3 shape on %s: first param = %s, want ProviderConfig (leaf 02)", ft.name, in0)
		}
		if ft.typ.In(1).Kind() != reflect.String {
			t.Fatalf("%s second param = %s, want string key", ft.name, ft.typ.In(1))
		}
	}

	// Part 3: live decode + store through the child read loop.
	const ccid = "kx1"
	d, child, childConn, p := newKeysTestPipeline(t, ccid)

	sendRaw(t, childConn, `{"call_id":"kx1","event":"keys","stt_key":"sk1","tts_key":"tk1",`+
		`"stt_vendor":"deepgram","stt_model":"nova-3","stt_base_url":"",`+
		`"tts_vendor":"qwen3","tts_model":"Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice",`+
		`"tts_base_url":"http://127.0.0.1:8880","tts_voice":"vivian"}`)

	// Preflight the store symbols so a timeout below still names them.
	pv := reflect.ValueOf(p).Elem()
	for _, f := range []string{"sttCfg", "ttsCfg"} {
		if !pv.FieldByName(f).IsValid() {
			t.Fatalf("missing symbol pipeline.%s (leaf 02): extended keys frame cannot be stored", f)
		}
	}

	waitFor(t, func() bool {
		v, ok := pipelineCfgString(p, "sttCfg", "Vendor")
		return ok && v == "deepgram"
	})
	wantSTT := map[string]string{"Vendor": "deepgram", "Model": "nova-3", "BaseURL": ""}
	for field, want := range wantSTT {
		if got, ok := pipelineCfgString(p, "sttCfg", field); !ok || got != want {
			t.Errorf("p.sttCfg.%s = %q (ok=%v), want %q (contract C1/C2)", field, got, ok, want)
		}
	}
	wantTTS := map[string]string{
		"Vendor":  "qwen3",
		"Model":   "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice",
		"BaseURL": "http://127.0.0.1:8880",
		"Voice":   "vivian",
	}
	for field, want := range wantTTS {
		if got, ok := pipelineCfgString(p, "ttsCfg", field); !ok || got != want {
			t.Errorf("p.ttsCfg.%s = %q (ok=%v), want %q (contract C1/C2)", field, got, ok, want)
		}
	}

	// Part 4: absent fields decode as "" (old two-field keys lines stay
	// compatible, C1 optionality rule).
	const ccid2 = "kx2"
	d.listenPair(ccid2)
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid2, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000})
	in2, out2 := d.acceptAll(ccid2)
	defer func() { _ = in2.Close() }()
	defer func() { _ = out2.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 2 })
	p2, ok := child.getPipeline(ccid2)
	if !ok {
		t.Fatalf("pipeline %s missing after setup", ccid2)
	}
	sendRaw(t, childConn, `{"call_id":"kx2","event":"keys","stt_key":"s2","tts_key":"t2"}`)
	waitFor(t, func() bool {
		v, ok := pipelineCfgString(p2, "sttCfg", "Vendor")
		return ok && v == ""
	})
	for _, cfgField := range []string{"sttCfg", "ttsCfg"} {
		for field := range keysWireWantGoFieldNames() {
			if got, ok := pipelineCfgString(p2, cfgField, field); !ok || got != "" {
				t.Errorf("absent wire field: p.%s.%s = %q (ok=%v), want \"\" (C1 absent = \"\")", cfgField, field, got, ok)
			}
		}
	}
}

// keysWireWantGoFieldNames lists the C2 config field names the keys frame
// populates (Vendor/Model/BaseURL per leg + TTS Voice).
func keysWireWantGoFieldNames() map[string]struct{} {
	return map[string]struct{}{
		"Vendor":  {},
		"Model":   {},
		"BaseURL": {},
		"Voice":   {},
	}
}

// newKeysTestPipeline registers one live pipeline for ccid (setup frame
// through the fake daemon) and returns the pieces the keys tests need.
func newKeysTestPipeline(t *testing.T, ccid string) (*fakeDaemon, *Child, net.Conn, *pipeline) {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000})
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() {
		_ = in.Close()
		_ = out.Close()
	})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, ok := child.getPipeline(ccid)
	if !ok {
		t.Fatalf("pipeline %s missing after setup", ccid)
	}
	t.Cleanup(func() {
		if err := childConn.Close(); err != nil {
			t.Errorf("close child conn: %v", err)
		}
		if runErr := <-runDone; runErr != nil {
			t.Errorf("Run returned error on close: %v", runErr)
		}
	})
	return d, child, childConn, p
}

// netConnRemoved: the helper now returns net.Conn directly.

var _ = fmt.Sprintf // keep fmt linked for helper error strings

// TestKeysBaseURLUserinfoScrub (audit fix 7): an OpenAI-compatible base
// URL is free-form user config and can embed userinfo credential material
// in its authority. String/LogValue must never print that half, while
// host+path stay debuggable; the STORED config keeps the full URL (the
// adapter needs it to dial).
func TestKeysBaseURLUserinfoScrub(t *testing.T) {
	const secret = "hunter" + "2"
	k := KeysFrame{
		CallID:     "cc-scrub",
		STTBaseURL: "https://ingest.example.com/v1",
		TTSBaseURL: "https://agent:" + secret + "@tts.internal:8880/compat/v1",
	}
	for _, s := range []string{k.String(), redactedKeys{k}.String(), fmt.Sprintf("%v", k)} {
		if strings.Contains(s, secret) {
			t.Fatalf("base_url rendering leaked userinfo secret: %s", s)
		}
	}
	str := k.String()
	if !strings.Contains(str, "tts.internal:8880") {
		t.Fatalf("scrubbed URL lost the host (misconfig must stay debuggable): %s", str)
	}
	if !strings.Contains(str, "https://"+Redacted+"@tts.internal:8880/compat/v1") {
		t.Fatalf("scrub shape wrong: %s", str)
	}
	if !strings.Contains(str, "https://ingest.example.com/v1") {
		t.Fatalf("credential-free URL must print untouched: %s", str)
	}

	// scrubURL unit edges: no scheme, no '@', '@' after the authority, port.
	if got := scrubURL("127.0.0.1:8880"); got != "127.0.0.1:8880" {
		t.Errorf("no-scheme URL rewritten: %s", got)
	}
	if got := scrubURL("http://127.0.0.1:8880"); got != "http://127.0.0.1:8880" {
		t.Errorf("clean URL rewritten: %s", got)
	}
	if got := scrubURL("http://host/path@a/b"); got != "http://host/path@a/b" {
		t.Errorf("'@' after the authority rewritten: %s", got)
	}
	if got := scrubURL(""); got != "" {
		t.Errorf("empty URL rewritten: %s", got)
	}

	// The STORED provider config keeps the full URL (dial needs it);
	// only the display path scrubs.
	_, _, childConn, p := newKeysTestPipeline(t, "cc-scrub-live")
	sendRaw(t, childConn, `{"call_id":"cc-scrub-live","event":"keys","stt_key":"s","tts_key":"t",`+
		`"tts_base_url":"https://agent:`+secret+`@tts.internal:8880/compat/v1"}`)
	waitFor(t, func() bool {
		v, ok := pipelineCfgString(p, "ttsCfg", "BaseURL")
		return ok && v == "https://agent:"+secret+"@tts.internal:8880/compat/v1"
	})
}
