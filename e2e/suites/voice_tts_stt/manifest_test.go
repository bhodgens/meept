//go:build e2e

// Root contract gate - task T3: the e2e manifest must register this suite
// and map the three C1/C2 implementation prefixes to it, so a diff to any
// of them selects the additive-wire gate (make e2e-affected / pre-push).
package voicettsstt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func manifestContains(suites []string, want string) bool {
	for _, s := range suites {
		if s == want {
			return true
		}
	}
	return false
}

// TestManifestRegistersVoiceTtsSttSuite - task T3.
// Given: the suite directory e2e/suites/voice_tts_stt.
// When: e2e/manifest.json is parsed.
// Then: path_map includes internal/voicechild/, internal/config/ mapped to this
// suite; for internal/daemon/ the catch-all keeps only whole-subject suites and
// exact-file keys (internal/daemon/voice_keys.go) map voice_tts_stt; the suite is registered.
func TestManifestRegistersVoiceTtsSttSuite(t *testing.T) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("T3: runtime.Caller failed; cannot locate e2e/manifest.json")
	}
	// self = <root>/e2e/suites/voice_tts_stt/manifest_test.go
	root := filepath.Clean(filepath.Join(filepath.Dir(self), "..", "..", ".."))
	manifestPath := filepath.Join(root, "e2e", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("T3: read manifest: %v", err)
	}
	var m struct {
		PathMap map[string][]string `json:"path_map"`
		Suites  []struct {
			Name   string `json:"name"`
			Dir    string `json:"dir"`
			Status string `json:"status"`
		} `json:"suites"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("T3: parse manifest: %v", err)
	}

	// internal/voicechild/ and internal/config/ are NOT narrow prefixes — catch-all must include voice_tts_stt
	for _, prefix := range []string{"internal/voicechild/", "internal/config/"} {
		if !manifestContains(m.PathMap[prefix], "voice_tts_stt") {
			t.Errorf("T3: manifest path_map[%q] does not include suite voice_tts_stt", prefix)
		}
	}
	// internal/daemon/ IS a narrow prefix — catch-all keeps only daemon-lifecycle;
	// exact-file key internal/daemon/voice_keys.go must map voice_tts_stt
	if manifestContains(m.PathMap["internal/daemon/"], "voice_tts_stt") {
		t.Error("T3: narrow prefix internal/daemon/ catch-all must not include voice_tts_stt (only daemon-lifecycle)")
	}
	if !manifestContains(m.PathMap["internal/daemon/voice_keys.go"], "voice_tts_stt") {
		t.Error("T3: exact-file key internal/daemon/voice_keys.go must include suite voice_tts_stt")
	}

	found := false
	for _, s := range m.Suites {
		if s.Name == "voice_tts_stt" && s.Dir == "e2e/suites/voice_tts_stt" && s.Status == "implemented" {
			found = true
			break
		}
	}
	if !found {
		t.Error("T3: manifest suites does not register voice_tts_stt at e2e/suites/voice_tts_stt with status implemented")
	}
}
