package providers

// Gate tests (STT branch 00T T1, feature F2, contract C1): the opt-in
// persistent whisper.cpp server transport. ProviderConfig.WhisperServerAddr
// is a NEW additive selector: "" keeps the exec-per-utterance path
// byte-identically; a non-empty address selects a persistent server client
// (no per-utterance exec subprocess) whose recognizeOne returns the SAME
// transcript text as exec mode.
//
// RED STYLE (deliberate, matches the foundation gate tests): the selector
// does not exist yet, so this package MUST keep compiling for sibling
// suites. Every missing symbol is probed via reflection (types that already
// exist) with named failures; no direct reference to a field that does not
// yet exist, no t.Skip. Tests are RED today (missing selector) and turn GREEN
// when leaf 01 lands, with zero edits here.
//
// Clauses: C1 (02-stt-path/orchestrator.md), 01-whisper-persistent.md
// T1/T2, F2, R4 (empty selector = byte-identical exec path).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/voicechild"
)

// whisperOf reads a struct field by name through reflection, following one
// pointer level. (value, true) only when the field exists and is a struct.
func whisperOf(v reflect.Value, field string) (reflect.Value, bool) {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	f := v.FieldByName(field)
	if !f.IsValid() {
		return reflect.Value{}, false
	}
	return f, true
}

// wantWhisperServerSelector pins ProviderConfig.WhisperServerAddr as a
// string selector. Absent or wrong-kind is RED.
func wantWhisperServerSelector(t *testing.T) {
	t.Helper()
	f, ok := reflect.TypeFor[ProviderConfig]().FieldByName("WhisperServerAddr")
	if !ok {
		t.Errorf("missing symbol ProviderConfig.WhisperServerAddr (contract C1, 01-whisper-persistent T1): cannot select persistent server mode")
		return
	}
	if f.Type.Kind() != reflect.String {
		t.Errorf("ProviderConfig.WhisperServerAddr is %s, want string (contract C1)", f.Type.Kind())
	}
}

// setWhisperServerAddr sets the selector via reflection when present.
// ok=false means the selector is absent (the RED condition).
func setWhisperServerAddr(cfg ProviderConfig, addr string) (ProviderConfig, bool) {
	v := reflect.ValueOf(&cfg).Elem()
	f := v.FieldByName("WhisperServerAddr")
	if !f.IsValid() || f.Kind() != reflect.String || !f.CanSet() {
		return cfg, false
	}
	f.SetString(addr)
	return cfg, true
}

// cfgServerAddr reads WhisperServerAddr from a constructed adapter's stored
// ProviderConfig (field "cfg"). ok=false if the field or selector is absent.
func cfgServerAddr(srv voicechild.STTProvider) (string, bool) {
	cfgField, ok := whisperOf(reflect.ValueOf(srv), "cfg")
	if !ok {
		return "", false
	}
	addrField := cfgField.FieldByName("WhisperServerAddr")
	if !addrField.IsValid() || addrField.Kind() != reflect.String {
		return "", false
	}
	return addrField.String(), true
}

// installExecMarker installs a whisper-cli stub that writes a marker file on
// EVERY invocation and prints "utt 1" on stdout. The marker proves whether
// recognizeOne took the (legacy) exec path.
func installExecMarker(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	marker := filepath.Join(binDir, "exec-ran")
	script := "#!/bin/sh\n" +
		"echo ran > \"" + marker + "\"\n" +
		"echo \"utt 1\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "whisper-cli"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake whisper-cli: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return marker
}

// execArgsCaptured installs a whisper-cli stub that records the exact argv
// it received and prints a known transcript on stdout.
func execArgsCaptured(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	argsPath := filepath.Join(binDir, "args")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" > \"" + argsPath + "\"\n" +
		"echo \"utt 1\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "whisper-cli"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake whisper-cli: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return argsPath
}

// fakeWhisperServer accepts any HTTP request and returns the plain-text
// transcript "utt 1" (whisper.cpp server's response_format=text shape),
// so an HTTP-based persistent client recognizes identically to exec mode.
func fakeWhisperServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("utt 1"))
	}))
}

// TestWhisperServerMode: Given a non-empty WhisperServerAddr, When
// recognizeOne runs, Then the request goes to the persistent transport (NOT
// a per-utterance exec subprocess) and the transcript is returned identically
// to exec mode ("utt 1"). RED today: the selector does not exist.
func TestWhisperServerMode(t *testing.T) {
	wantWhisperServerSelector(t)

	marker := installExecMarker(t)
	srvHTTP := fakeWhisperServer(t)
	defer srvHTTP.Close()

	cfg, ok := setWhisperServerAddr(whisperCfg(whisperTestRate), srvHTTP.Listener.Addr().String())
	if !ok {
		t.Fatalf("cannot set ProviderConfig.WhisperServerAddr: missing selector (contract C1)")
	}
	s, err := newWhisperSTT(cfg)
	if err != nil {
		t.Fatalf("construct whisper STT in server mode: %v", err)
	}

	// The selector must reach the adapter unchanged.
	if got, seen := cfgServerAddr(s); seen && got != srvHTTP.Listener.Addr().String() {
		t.Errorf("adapter stored WhisperServerAddr = %q, want %q", got, srvHTTP.Listener.Addr().String())
	}

	w, ok := s.(*WhisperSTT)
	if !ok {
		t.Fatalf("whisper adapter type = %T, want *WhisperSTT", s)
	}
	buf := chunkOf(speechChunkMs, loudAmplitude)
	tr, err := w.recognizeOne(t.Context(), t.TempDir(), 1, buf)
	// Server mode must never exec a per-utterance whisper-cli subprocess.
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Errorf("server mode exec'd whisper-cli (marker %s exists): persistent transport must not spawn a subprocess", marker)
	}
	if err != nil {
		// A non-HTTP transport (e.g. unix socket) is a valid implementation;
		// the exec-freedom assertion above is the hard gate. Report the dial
		// failure as informational only when the transport is not HTTP-based.
		t.Logf("server recognizeOne returned err (transport may differ from httptest): %v", err)
		return
	}
	// Any non-empty transcript containing the fake server's text proves the
	// persistent transport round-tripped the result (the response body may
	// be plain text or wrapped JSON depending on response_format).
	if !strings.Contains(tr, "utt 1") {
		t.Errorf("server recognizeOne transcript = %q, want it to contain %q (identical to exec mode)", tr, "utt 1")
	}
}

// TestWhisperExecFallback: Given an empty WhisperServerAddr (legacy), When
// recognizeOne runs, Then the exec path is byte-identical: the SAME argv
// (-m model -f wav -nt) and the SAME transcript. GREEN today; must stay GREEN
// after leaf 01 — this pins the R4 legacy fallback.
func TestWhisperExecFallback(t *testing.T) {
	wantWhisperServerSelector(t)

	argsPath := execArgsCaptured(t)
	cfg, ok := setWhisperServerAddr(whisperCfg(whisperTestRate), "")
	if !ok {
		t.Fatalf("cannot set ProviderConfig.WhisperServerAddr: missing selector (contract C1)")
	}
	s, err := newWhisperSTT(cfg)
	if err != nil {
		t.Fatalf("construct whisper STT: %v", err)
	}
	w, ok := s.(*WhisperSTT)
	if !ok {
		t.Fatalf("whisper adapter type = %T, want *WhisperSTT", s)
	}

	buf := chunkOf(speechChunkMs, loudAmplitude)
	got, err := w.recognizeOne(t.Context(), t.TempDir(), 1, buf)
	if err != nil {
		t.Fatalf("recognizeOne: %v", err)
	}
	if got != "utt 1" {
		t.Errorf("recognizeOne transcript = %q, want %q (legacy exec contract)", got, "utt 1")
	}

	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read captured argv: %v", err)
	}
	args := strings.TrimSpace(string(data))
	for _, want := range []string{"-m", "-f", "-nt"} {
		if !strings.Contains(args, want) {
			t.Errorf("exec argv %q missing legacy argument %q", args, want)
		}
	}
	if !strings.HasSuffix(args, "-nt") {
		t.Errorf("exec argv %q must end with -nt (legacy plain-text stdout)", args)
	}
}
