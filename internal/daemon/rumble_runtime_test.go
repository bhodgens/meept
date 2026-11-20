package daemon

// H1 closure pins (bughunt 2026-11-19, SUPERSEDED branch). The former
// F5 gate (TestRumbleRuntimeConfigCap) and spawn-arg clamp gate
// (TestRumbleSpawnArgsClamp) targeted the daemon-side spawn config and
// renderer, both DELETED when leaf 02 T2 was superseded by the native
// WS adapter (vLLM cannot serve /v1/audio/transcriptions/stream for this
// checkpoint; the daemon never spawns a Rumble server). The surviving
// contract lives in config: the GPU-fraction clamp gates stay in
// internal/config/voice_fields_test.go (TestNormalizeVoiceRumbleDefaults,
// TestRumbleClampWiredAtDaemonBoundary) and the frozen-defaults + BaseURL
// gates in TestVoiceRumbleRuntimeConfigDefaults there. Here we pin the
// LIVE seam: the keys-line stt_base_url derivation, including the opt-in
// byte-identity rule (nothing changes for any non-rumble vendor, and an
// explicit operator stt_base_url always wins).

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestResolveSTTBaseURL pins the derivation precedence table.
func TestResolveSTTBaseURL(t *testing.T) {
	frozen := config.DefaultVoiceRumbleRuntimeConfig()
	cases := []struct {
		name       string
		vendor     string
		configured string
		rr         config.VoiceRumbleRuntimeConfig
		want       string
	}{
		// Opt-in byte-identity: unset / whisper / deepgram vendors keep
		// the configured value verbatim, including empty.
		{"unset_vendor_empty_url", "", "", frozen, ""},
		{"whisper_empty_url", "whisper", "", frozen, ""},
		{"deepgram_empty_url", "deepgram", "", frozen, ""},
		{"whisper_explicit_url", "whisper", "http://127.0.0.1:9000", frozen, "http://127.0.0.1:9000"},
		{"deepgram_explicit_url", "deepgram", "https://api.deepgram.com", frozen, "https://api.deepgram.com"},
		// The H1 fix: rumble with no URL derives the loopback default.
		{"rumble_empty_url_derives", "rumble", "", frozen, "http://" + config.RumbleLoopbackAddr},
		{"rumble_zero_block_derives_frozen", "rumble", "", config.VoiceRumbleRuntimeConfig{}, "http://" + config.RumbleLoopbackAddr},
		// Explicit operator URL ALWAYS wins - the live transformers
		// server path must stay byte-identical.
		{"rumble_explicit_url_wins", "rumble", "http://socrates:8000", frozen, "http://socrates:8000"},
		// A customized endpoint key moves the derived URL with it.
		{
			"rumble_custom_endpoint_key",
			"rumble", "",
			config.VoiceRumbleRuntimeConfig{EndpointKey: "rumble:127.0.0.1:8099"},
			"http://127.0.0.1:8099",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSTTBaseURL(tc.vendor, tc.configured, tc.rr); got != tc.want {
				t.Fatalf("resolveSTTBaseURL(%q, %q) = %q, want %q", tc.vendor, tc.configured, got, tc.want)
			}
		})
	}
}

// TestRumbleSpawnSuperseded pins that the derivation NEVER consults a
// spawn surface: the config block may carry an unsafe GPU fraction, but
// resolveSTTBaseURL is pure string derivation - no clamp needed here
// because nothing renders spawn args anymore (leaf 02 T2 SUPERSEDED).
// The clamp for operator reuse stays pinned config-side.
func TestRumbleSpawnSuperseded(t *testing.T) {
	rr := config.VoiceRumbleRuntimeConfig{
		GPUMemoryUtilization: 0.92, // unsafe for a MANUAL vLLM run; clamped by config.ValidateAll
		MaxModelLen:          4096,
		EndpointKey:          "rumble:" + config.RumbleLoopbackAddr,
	}
	if got := resolveSTTBaseURL("rumble", "", rr); got != "http://"+config.RumbleLoopbackAddr {
		t.Fatalf("derived URL = %q, want the frozen loopback default", got)
	}
}
