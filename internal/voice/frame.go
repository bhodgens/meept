// Package voice implements the daemon side of the voice sidecar protocol:
// text frames over a shared unix socket (C1) and per-call raw-PCM audio
// socket pairs (C2). Package-internal contract references (C1, C2) point at
// docs/plans/20261102-voice-sidecar/master.md.
//
// The frame codec itself lives in internal/voiceframe (moved there by leaf
// 07 so the child side can share it without importing the daemon package);
// the names below are package aliases kept so existing internal/voice code
// and tests keep working unchanged.
package voice

import (
	"github.com/caimlas/meept/internal/voiceframe"
)

// Frame is one line of JSON on the shared text socket (C1). Alias for
// voiceframe.Frame, the shared codec type.
type Frame = voiceframe.Frame

// Text-socket event values (C1).
const (
	EventSetup     = voiceframe.EventSetup
	EventPrompt    = voiceframe.EventPrompt
	EventInterrupt = voiceframe.EventInterrupt
	EventEnd       = voiceframe.EventEnd
	EventSay       = voiceframe.EventSay
	EventClear     = voiceframe.EventClear
)

// EncodeFrame serializes f as one JSON line: compact JSON plus '\n'.
func EncodeFrame(f Frame) ([]byte, error) { return voiceframe.EncodeFrame(f) }

// DecodeFrame parses one JSON line into a Frame. Unknown event values
// (including "dtmf") decode without error.
func DecodeFrame(line []byte) (Frame, error) { return voiceframe.DecodeFrame(line) }

// ChunkBytes returns the raw-PCM chunk size in bytes for a 20 ms frame of
// the negotiated codec (C2).
func ChunkBytes(codec string, sampleRate int) (int, error) {
	return voiceframe.ChunkBytes(codec, sampleRate)
}
