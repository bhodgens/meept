// Package voiceframe holds the voice sidecar frame codec shared by the
// daemon side (internal/voice) and the child side (internal/voicechild,
// cmd/meept-voice). Contract references (C1, C2) point at
// docs/plans/20261102-voice-sidecar/master.md.
//
// This package is deliberately dependency-free (stdlib only): neither side
// of the socket may drag the other's world in through the shared codec.
// jargo is never imported here (import pin, gate test T9).
package voiceframe

import (
	"encoding/json"
	"fmt"
)

// Frame is one line of JSON on the shared text socket. Every frame carries
// CallID so a single child connection can host N concurrent call pipelines.
//
// Last is *bool so an omitted "last" key (setup, interrupt, end) is
// distinguishable from an explicit false: only final prompts
// (last:true) start an employee turn.
//
// The JSON keys here are byte-exact per contract C1; a wrong key silently
// breaks the child.
type Frame struct {
	CallID     string `json:"call_id"`
	Event      string `json:"event"` // setup|prompt|interrupt|end|say|clear|played|open
	Text       string `json:"text,omitempty"`
	Last       *bool  `json:"last,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Direction  string `json:"direction,omitempty"` // inbound|outbound
	Codec      string `json:"codec,omitempty"`     // L16|PCMU
	SampleRate int    `json:"sample_rate,omitempty"`
	Reason     string `json:"reason,omitempty"`

	// Per-call provider profile (local-leg plan CL1, additive). The daemon
	// sets these on the open frame; zero values are omitted, so existing
	// frames stay byte-identical on the wire.
	Local      bool   `json:"local,omitempty"`
	SttVendor  string `json:"stt_vendor,omitempty"`
	SttModel   string `json:"stt_model,omitempty"`
	TtsVendor  string `json:"tts_vendor,omitempty"`
	TtsModel   string `json:"tts_model,omitempty"`
	TtsVoice   string `json:"tts_voice,omitempty"`
	BargeInRms int    `json:"barge_in_rms,omitempty"`

	// Filler-clip cache (plan 20261113-filler-clips-and-context-warm,
	// Contract C3, ALL fields additive + omitempty). An old child ignores
	// clip_id/clip_render/clip_ready and falls back to live synth —
	// today's byte-identical behavior; an old daemon never sends them.
	//
	// ClipID rides a say frame: the 64-hex clipstore key of the pre-rendered
	// PCM for Text. Text stays the transcript source (R6): the clip is an
	// audio-only optimization, never a second speech path.
	ClipID string `json:"clip_id,omitempty"`
	// Clips carries the render list on a clip_render frame (daemon ->
	// child). Capped at 32 entries per frame per Contract C3; the daemon
	// shards larger pools across frames and the cap is enforced there.
	Clips []ClipSpec `json:"clips,omitempty"`
	// Key + OK are the clip_ready result fields (child -> daemon):
	// Key is the 64-hex clip key that was rendered (or found warm), OK
	// reports success. OK is *bool so an explicit false ("ok":false) is
	// distinguishable from an absent field, exactly like Last.
	Key string `json:"key,omitempty"`
	OK  *bool  `json:"ok,omitempty"`
}

// ClipSpec is one {key,text} entry of a clip_render frame (Contract C3).
// The key is pre-computed by the daemon planner with the pinned C1
// formula; the child renders Text through its installed TTS provider and
// stores the result under Key — the child never recomputes the key.
type ClipSpec struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// Text-socket event values (C1).
const (
	EventSetup     = "setup"
	EventPrompt    = "prompt"
	EventInterrupt = "interrupt"
	EventEnd       = "end"
	EventSay       = "say"
	EventClear     = "clear"

	// EventOpen is the daemon -> child open frame (local-leg plan CL1):
	// per call, at pipeline registration, for both legs. The child creates
	// the pipeline, dials the C2 audio pair, and answers with setup.
	EventOpen = "open"

	// EventPlayed is the child -> daemon end-of-turn audio marker (turn-final
	// drain fix): the child's playback worker sends exactly ONE played frame
	// (last:true) after the FINAL successful audioOut.Write of a last:true
	// say frame's stream, on the text socket. It is the audio-truth signal
	// that this turn's speech cannot produce any more PCM: the daemon arms
	// the splice's drain watch from this edge instead of blindly on the
	// turn_final TEXT edge, where the child still has queued sentences whose
	// inter-sentence synth gaps (measured 1.4-3.8 s) exceed the quiescence
	// window and fake a mid-reply drain (link-2fdc3f5fd8d81805: 10.94 s of
	// both-audible overlap). Older child binaries never send it; the
	// coordinator's fallback margin keeps bounding those.
	// Unknown-event decode tolerance means a daemon that predates this
	// constant drops the frame silently.
	EventPlayed = "played"

	// EventClipRender is the daemon -> child clip-warming request (Contract
	// C3, plan 20261113-filler-clips-and-context-warm): one frame carries
	// up to 32 Clips {key,text} specs the daemon's planner diffed against
	// the clip store. The child renders misses in the background (never on
	// the turn lane) and answers each spec with one EventClipReady frame.
	// An old child decodes the frame as an unknown event and drops it
	// silently (C3 back-compat).
	EventClipRender = "clip_render"

	// EventClipReady is the child -> daemon result of one clip_render
	// entry: {"call_id":"<ccid>","event":"clip_ready","key":"<64 hex>",
	// "ok":true|false}. ok:false means the render or store write failed;
	// the line stays playable via live synth (the clip path never fails
	// a turn). An old daemon drops the unknown event silently.
	EventClipReady = "clip_ready"
)

// EncodeFrame serializes f as one JSON line: compact JSON plus '\n'.
func EncodeFrame(f Frame) ([]byte, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("encode frame: %w", err)
	}
	return append(b, '\n'), nil
}

// DecodeFrame parses one JSON line into a Frame. Unknown event values
// (including "dtmf") decode without error: a voicemail-tree "press 1" must
// never error the call; handling of unrecognized events is the handler's
// business, not the codec's.
func DecodeFrame(line []byte) (Frame, error) {
	var f Frame
	if err := json.Unmarshal(line, &f); err != nil {
		return Frame{}, fmt.Errorf("decode frame: %w", err)
	}
	return f, nil
}

// ChunkBytes returns the raw-PCM chunk size in bytes for a 20 ms frame of
// the negotiated codec (C2). L16 is s16le at the negotiated rate (NOT
// big-endian RTP L16); PCMU is 8 kHz mu-law.
func ChunkBytes(codec string, sampleRate int) (int, error) {
	switch {
	case codec == "L16" && sampleRate == 16000:
		return 640, nil
	case codec == "PCMU" && sampleRate == 8000:
		return 160, nil
	default:
		return 0, fmt.Errorf("voiceframe: unsupported codec %q at sample rate %d", codec, sampleRate)
	}
}
