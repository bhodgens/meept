package providers

// Gate tests (STT branch 00T T3, feature F5, contract C3): opt-in critical
// entity priming. ProviderConfig.Vocabulary is a NEW additive selector:
//   - nil/empty -> wire requests BYTE-IDENTICAL to legacy (R4 pin).
//   - non-empty -> Deepgram "keyterm" params on the listen path and a
//     Whisper "-p" prompt argument on the exec command.
//
// RED STYLE (deliberate, matches the foundation gate tests): the selector
// does not exist yet, so this package MUST keep compiling. The missing
// symbol is probed via reflection with a named failure; no direct reference
// to a field that does not yet exist, no t.Skip. Tests are RED today and
// turn GREEN when leaf 03 lands, with zero edits here.
//
// Clauses: C3 (02-stt-path/orchestrator.md), 03-entity-priming.md T1/T2,
// F5, R4 (empty vocabulary = byte-identical wire).

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// wantVocabularySelector pins ProviderConfig.Vocabulary as a []string
// selector. Absent or wrong-kind is RED.
func wantVocabularySelector(t *testing.T) {
	t.Helper()
	f, ok := reflect.TypeFor[ProviderConfig]().FieldByName("Vocabulary")
	if !ok {
		t.Errorf("missing symbol ProviderConfig.Vocabulary (contract C3, 03-entity-priming T1/T2): cannot prime critical entities")
		return
	}
	if f.Type.Kind() != reflect.Slice || f.Type.Elem().Kind() != reflect.String {
		t.Errorf("ProviderConfig.Vocabulary is %s, want []string (contract C3)", f.Type.Kind())
	}
}

// setVocabulary sets the selector via reflection when present. ok=false
// means the selector is absent (the RED condition).
func setVocabulary(cfg ProviderConfig, terms []string) (ProviderConfig, bool) {
	v := reflect.ValueOf(&cfg).Elem()
	f := v.FieldByName("Vocabulary")
	if !f.IsValid() || f.Kind() != reflect.Slice || !f.CanSet() {
		return cfg, false
	}
	s := reflect.MakeSlice(f.Type(), len(terms), len(terms))
	for i, term := range terms {
		s.Index(i).SetString(term)
	}
	f.Set(s)
	return cfg, true
}

// buildListenPath calls the REAL listenPath on a constructed DeepgramSTT
// with the given config, so the test observes whatever query params the
// implementation produces. Uses NewSTT (no dial) — listenPath is pure.
func buildListenPath(t *testing.T, cfg ProviderConfig) string {
	t.Helper()
	s, err := NewSTT(cfg, "test-key")
	if err != nil {
		t.Fatalf("construct Deepgram STT: %v", err)
	}
	dg, ok := s.(*DeepgramSTT)
	if !ok {
		t.Fatalf("adapter type = %T, want *DeepgramSTT", s)
	}
	return dg.listenPath()
}

// TestDeepgramKeytermParams: Given a vocabulary list, When the Deepgram
// listen path is built, Then keyterm params are present for each term. Given
// an empty vocabulary, the URL is identical to the legacy shape (no keyterm).
func TestDeepgramKeytermParams(t *testing.T) {
	wantVocabularySelector(t)

	baseCfg := ProviderConfig{Vendor: "deepgram", Model: "nova-3", BaseURL: "api.deepgram.com", SampleRate: 16000}
	legacy := buildListenPath(t, baseCfg)

	// Empty vocabulary: byte-identical to legacy (R4).
	emptyCfg, ok := setVocabulary(baseCfg, nil)
	if !ok {
		t.Fatalf("cannot set ProviderConfig.Vocabulary: missing selector (contract C3)")
	}
	if got := buildListenPath(t, emptyCfg); got != legacy {
		t.Errorf("empty-vocabulary listen path = %q, want legacy %q (R4: empty = byte-identical)", got, legacy)
	}

	// Non-empty vocabulary: keyterm params must be present.
	vocabCfg, ok := setVocabulary(baseCfg, []string{"Ada Lovelace", "0xABC123"})
	if !ok {
		t.Fatalf("cannot set ProviderConfig.Vocabulary: missing selector (contract C3)")
	}
	got := buildListenPath(t, vocabCfg)
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "keyterm") {
		t.Errorf("listen path with vocabulary = %q, want keyterm params", got)
	}
	for _, term := range []string{"Ada+Lovelace", "ada%20lovelace", "0xabc123"} {
		if strings.Contains(lower, strings.ToLower(term)) {
			return // at least one term rendered on the wire — contract met
		}
	}
	t.Errorf("listen path %q carries keyterm but no vocabulary term rendered", got)
}

// captureWhisperArg installs a whisper-cli stub that records argv and
// prints a fixed transcript. Returns the argv-capture path.
func captureWhisperArg(t *testing.T) string {
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

// TestWhisperPromptArg: Given a vocabulary, When whisper recognizeOne
// execs, Then a "-p" prompt arg carrying the vocabulary is included. Given
// an empty vocabulary, args are identical to legacy (-m model -f wav -nt,
// no -p).
func TestWhisperPromptArg(t *testing.T) {
	wantVocabularySelector(t)

	baseCfg := ProviderConfig{Vendor: "whisper", Model: "/tmp/fake-model.bin", SampleRate: whisperTestRate}

	// Empty vocabulary: legacy argv (no -p).
	legacyArgsPath := captureWhisperArg(t)
	emptyCfg, ok := setVocabulary(baseCfg, nil)
	if !ok {
		t.Fatalf("cannot set ProviderConfig.Vocabulary: missing selector (contract C3)")
	}
	emptyS, err := newWhisperSTT(emptyCfg)
	if err != nil {
		t.Fatalf("construct whisper STT (empty vocab): %v", err)
	}
	ew, ok := emptyS.(*WhisperSTT)
	if !ok {
		t.Fatalf("adapter type = %T, want *WhisperSTT", emptyS)
	}
	if _, err := ew.recognizeOne(t.Context(), t.TempDir(), 1, chunkOf(speechChunkMs, loudAmplitude)); err != nil {
		t.Fatalf("recognizeOne (empty vocab): %v", err)
	}
	legacyArgs, err := os.ReadFile(legacyArgsPath)
	if err != nil {
		t.Fatalf("read legacy argv: %v", err)
	}
	legacyArgStr := strings.TrimSpace(string(legacyArgs))
	if strings.Contains(legacyArgStr, " -p ") || strings.HasPrefix(strings.TrimSpace(legacyArgStr), "-p ") {
		t.Errorf("empty-vocabulary exec argv = %q, want no -p prompt (R4: empty = byte-identical)", legacyArgStr)
	}

	// Non-empty vocabulary: -p prompt required.
	vocabArgsPath := captureWhisperArg(t)
	vocabCfg, ok := setVocabulary(baseCfg, []string{"Ada Lovelace", "0xABC123"})
	if !ok {
		t.Fatalf("cannot set ProviderConfig.Vocabulary: missing selector (contract C3)")
	}
	vocabS, err := newWhisperSTT(vocabCfg)
	if err != nil {
		t.Fatalf("construct whisper STT (vocab): %v", err)
	}
	vw, ok := vocabS.(*WhisperSTT)
	if !ok {
		t.Fatalf("adapter type = %T, want *WhisperSTT", vocabS)
	}
	if _, err := vw.recognizeOne(t.Context(), t.TempDir(), 1, chunkOf(speechChunkMs, loudAmplitude)); err != nil {
		t.Fatalf("recognizeOne (vocab): %v", err)
	}
	vocabArgsData, err := os.ReadFile(vocabArgsPath)
	if err != nil {
		t.Fatalf("read vocab argv: %v", err)
	}
	vocabArgStr := strings.TrimSpace(string(vocabArgsData))
	fields := strings.Fields(vocabArgStr)
	hasP := false
	for _, f := range fields {
		if f == "-p" {
			hasP = true
			break
		}
	}
	if !hasP {
		t.Errorf("vocabulary exec argv = %q, want -p prompt argument (03-entity-priming T2)", vocabArgStr)
	}
	if !strings.Contains(strings.ToLower(vocabArgStr), "ada") && !strings.Contains(strings.ToLower(vocabArgStr), "0xabc123") {
		t.Errorf("vocabulary exec argv = %q, want at least one vocabulary term in the prompt", vocabArgStr)
	}
}

// TestVocabEmptyWireUnchanged pins the exact legacy listen path shape (no
// keyterm, no vocabulary leakage) independently of the compare in
// TestDeepgramKeytermParams, so an implementation that garbles BOTH paths
// cannot pass by making them equal. The TestVocab prefix is deliberate: the
// leaf's T3 verify command is `-run TestVocab`.
func TestVocabEmptyWireUnchanged(t *testing.T) {
	wantVocabularySelector(t)
	cfg := ProviderConfig{Vendor: "deepgram", Model: "nova-3", BaseURL: "api.deepgram.com", SampleRate: 16000}
	got := buildListenPath(t, cfg)
	for _, forbidden := range []string{"keyterm", "vocabulary", "keywords=ada"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Errorf("legacy listen path %q unexpectedly contains %q (R4: empty vocabulary = byte-identical wire)", got, forbidden)
		}
	}
}
