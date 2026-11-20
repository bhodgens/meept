package daemon

// Rumble STT base-URL derivation (bughunt 2026-11-19 H1, SUPERSEDED branch).
//
// History: plan 20261118-rumble-stt-provider leaf 02 promised a supervised
// vLLM sidecar (RuntimeProcess spawn guards, orphan sweep, /health). The
// config structs and spawn-arg renderer landed tested but never wired. The
// follow-up tree 20261119-rumble-native-streaming then REPLACED the batch
// adapter with a native WebSocket streaming client against an
// operator-deployed FastAPI + transformers server. vLLM cannot serve that
// native route for this checkpoint (see docs/research/rumble2flash-review.md:
// stream=True is output-token streaming after the FULL audio arrives, and
// /v1/realtime requires causal encoders while Rumble's is bidirectional),
// so a daemon-spawned vLLM sidecar would be health-green but unusable by
// the only shipped client. Leaf 02 T2 is therefore SUPERSEDED, not wired;
// the dead spawn helpers were deleted per the deadcode-wiring-triage
// triple gate (unreachable in every main, zero textual callers, plan
// evidence). ONE config source of truth remains:
// config.VoiceRumbleRuntimeConfig ([voice.rumble_runtime]).
//
// What IS wired here: the missing stt_base_url derivation. The child's
// newRumbleSTT fails EVERY call's provider construction without a base
// URL (ErrMissingRumbleBaseURL), so setting stt_provider = "rumble" alone
// used to leave the leg deaf. resolveSTTBaseURL closes that gap: the
// operator's explicit voice.stt_base_url always wins byte-identically
// (the live transformers server path), and only a rumble vendor with an
// EMPTY stt_base_url derives the frozen loopback default from the config
// block. Every other vendor keeps today's wire byte-identically: nothing
// new spawns, ever, for any provider.

import (
	"github.com/caimlas/meept/internal/config"
)

// rumbleSTTVendor is the C1 keys-line vendor selector for the Rumble
// native streaming adapter (child: providers.NewSTT case "rumble").
const rumbleSTTVendor = "rumble"

// resolveSTTBaseURL picks the stt_base_url for the C1 keys line.
// Precedence:
//  1. An explicit operator stt_base_url ALWAYS wins, for every vendor
//     (the live native-streaming deployment sets it to the transformers
//     server; that path stays byte-identical).
//  2. stt_vendor == "rumble" with an empty stt_base_url derives the
//     legacy loopback default from [voice.rumble_runtime].EndpointKey.
//     The operator must be running a Rumble server there; the daemon
//     never spawns one (leaf 02 T2 SUPERSEDED, see file comment).
//  3. Everything else returns the configured value unchanged ("" for
//     unset), so whisper/deepgram/empty-vendor lines are byte-identical.
func resolveSTTBaseURL(vendor, configured string, rr config.VoiceRumbleRuntimeConfig) string {
	if configured != "" {
		return configured
	}
	if vendor == rumbleSTTVendor {
		return rr.BaseURL()
	}
	return configured
}
