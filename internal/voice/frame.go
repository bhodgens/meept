// Package voice implements the daemon side of the voice sidecar protocol:
// text frames over a shared unix socket (C1) and per-call raw-PCM audio
// socket pairs (C2). Package-internal contract references (C1, C2) point at
// docs/plans/20261102-voice-sidecar/master.md.
//
// The socket layer moves bytes only; reordering and pacing live in
// splice.go (leaf 06).
package voice

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
	Event      string `json:"event"` // setup|prompt|interrupt|end|say|clear
	Text       string `json:"text,omitempty"`
	Last       *bool  `json:"last,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Direction  string `json:"direction,omitempty"` // inbound|outbound
	Codec      string `json:"codec,omitempty"`     // L16|PCMU
	SampleRate int    `json:"sample_rate,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// Text-socket event values (C1).
const (
	EventSetup     = "setup"
	EventPrompt    = "prompt"
	EventInterrupt = "interrupt"
	EventEnd       = "end"
	EventSay       = "say"
	EventClear     = "clear"
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
		return 0, fmt.Errorf("voice: unsupported codec %q at sample rate %d", codec, sampleRate)
	}
}
