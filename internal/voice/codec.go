package voice

// Codec layer for the media splice (leaf 06): G.711 mu-law transcode and
// the Telnyx media WS frame envelope.
//
// Borrowed from uptractor (core/internal/mediabridge/mulaw.go and
// bridge.go), module-adapted. Borrow-with-changes ledger:
//   - mulawDecodeByte / linearToMulawByte / mulaw8ToPCM16: verbatim.
//   - pcm24ToMulaw8 (24 kHz TTS -> 8 kHz wire) is adapted here as
//     pcm16ToMulaw8 (16 kHz child PCM -> 8 kHz wire): pair-average
//     decimation plus the same encoder, because the meept child speaks
//     L16 at 16 kHz on the C2 socket, not 24 kHz TTS audio.
//   - DecodeMediaFrame is extended to surface sequence_number and
//     media.timestamp: the reorder window in splice.go orders on them and
//     the uptractor bridge forwarded in arrival order (this leaf adds
//     reorder, so the borrowed decode must carry the order keys).

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
)

// mulawDecodeByte decodes one G.711 mu-law byte to a 16-bit linear sample.
// Borrowed verbatim from uptractor mediabridge/mulaw.go.
func mulawDecodeByte(u byte) int16 {
	u = ^u
	neg := u&0x80 != 0
	exp := (u >> 4) & 0x07
	mant := u & 0x0F
	v := int(mant)<<(exp+3) + (0x84 << exp) - 0x84
	if neg {
		v = -v
	}
	return int16(v)
}

// linearToMulawByte encodes one 16-bit linear sample as G.711 mu-law.
// Borrowed verbatim from uptractor mediabridge/mulaw.go.
func linearToMulawByte(pcm int16) byte {
	const bias = 0x84
	v := int(pcm)
	var sign byte
	if v < 0 {
		v = -v
		sign = 0x80
	}
	v += bias
	if v > 32767 {
		v = 32767
	}
	exp := 7
	mask := 0x4000
	for exp > 0 && v&mask == 0 {
		exp--
		mask >>= 1
	}
	mant := (v >> (exp + 3)) & 0x0F
	return ^(sign | byte(exp)<<4 | byte(mant))
}

// mulaw8ToPCM16 doubles every 8 kHz decoded sample into 16 kHz PCM16 LE
// with a linear midpoint between samples. Borrowed verbatim from uptractor
// mediabridge/mulaw.go (including its documented edge: the final midpoint
// after the last sample is not emitted, so the output is 4*len-2 bytes for
// len mu-law bytes; the splice accumulates into fixed chunks, so the
// 2-byte shortfall per frame is absorbed by the chunk accumulator rather
// than causing drift).
func mulaw8ToPCM16(mu []byte) []byte {
	if len(mu) == 0 {
		return nil
	}
	out := make([]byte, 4*len(mu)-2)
	pos := 0
	for i := 0; i < len(mu); i++ {
		s := mulawDecodeByte(mu[i])
		out[pos] = byte(s)
		out[pos+1] = byte(s >> 8)
		pos += 2
		if i+1 < len(mu) {
			next := mulawDecodeByte(mu[i+1])
			mid := (int(s) + int(next)) / 2
			out[pos] = byte(mid)
			out[pos+1] = byte(mid >> 8)
			pos += 2
		}
	}
	return out
}

// pcm16ToMulaw8 converts child PCM16 LE at 16 kHz to mu-law at 8 kHz:
// average each pair of samples (crude low-pass ahead of the 8 kHz output
// Nyquist, the 16 kHz-rate adaptation of uptractor's windowed-sinc
// pcm24ToMulaw8 path), then encode. Incomplete trailing groups are dropped.
func pcm16ToMulaw8(pcm []byte) []byte {
	n := len(pcm) / 4 // one output per PAIR of s16le samples
	if n == 0 {
		return nil
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		s0 := int(int16(binary.LittleEndian.Uint16(pcm[4*i:])))
		s1 := int(int16(binary.LittleEndian.Uint16(pcm[4*i+2:])))
		out[i] = linearToMulawByte(int16((s0 + s1) / 2))
	}
	return out
}

// transcodeInbound converts one Telnyx media payload to the child's native
// L16 s16le at 16 kHz. L16 payloads pass through BYTE-IDENTICAL: L16 on the
// Telnyx media stream is s16le, NOT big-endian RTP L16, so bytes are never
// swapped (contract C2). PCMU payloads are mu-law at 8 kHz and are decoded
// and upsampled. Only the negotiated codec from the start frame selects the
// path; nothing sniffs bytes.
func transcodeInbound(payload []byte, codec string) []byte {
	if codec == "PCMU" {
		return mulaw8ToPCM16(payload)
	}
	return payload
}

// transcodeOutbound converts one child chunk (L16 s16le at 16 kHz) to the
// negotiated wire codec. L16 passes through byte-identical (no swap); PCMU
// is encoded from mu-law-decoded-rate-paired samples at 8 kHz.
func transcodeOutbound(chunk []byte, codec string) []byte {
	if codec == "PCMU" {
		return pcm16ToMulaw8(chunk)
	}
	return chunk
}

// MediaFrame is one decoded Telnyx media WS frame. SequenceNumber and
// TimestampMs are nil when the frame omits them; the reorder window prefers
// sequence_number and falls back to media.timestamp (spec "Frame order").
type MediaFrame struct {
	Payload        []byte
	SequenceNumber *int64
	TimestampMs    *float64
}

// mediaFrameEnvelope mirrors the Telnyx media WS frame shape: inbound is
// {"event":"media","sequence_number":N,"media":{"payload":"<base64>",
// "timestamp":M}}. sequence_number and timestamp are pointers so an omitted
// key is distinguishable from a zero value.
type mediaFrameEnvelope struct {
	Event          string `json:"event"`
	SequenceNumber *int64 `json:"sequence_number"`
	Media          struct {
		Payload   string   `json:"payload"`
		Timestamp *float64 `json:"timestamp"`
	} `json:"media"`
}

// mediaFrameOut is the outbound envelope written to the Telnyx media WS:
// {"event":"media","media":{"payload":"<base64>"}} (borrowed shape from
// uptractor bridge.go EncodeMediaFrame).
type mediaFrameOut struct {
	Event string       `json:"event"`
	Media mediaPayload `json:"media"`
}

type mediaPayload struct {
	Payload string `json:"payload"`
}

// EncodeMediaFrame encodes wire-codec audio bytes as a Telnyx media frame:
// media.payload is standard base64 of the (already transcoded) audio bytes.
func EncodeMediaFrame(pcm []byte) ([]byte, error) {
	frame := mediaFrameOut{
		Event: "media",
		Media: mediaPayload{
			Payload: base64.StdEncoding.EncodeToString(pcm),
		},
	}
	encoded, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("voice: encode media frame: %w", err)
	}
	return encoded, nil
}

// DecodeMediaFrame parses one inbound Telnyx media WS frame. Non-media
// events (mark, clear, dtmf) are an error here so the caller can skip them
// without treating them as audio; garbage JSON is an error.
func DecodeMediaFrame(frame []byte) (MediaFrame, error) {
	var env mediaFrameEnvelope
	if err := json.Unmarshal(frame, &env); err != nil {
		return MediaFrame{}, fmt.Errorf("voice: decode media frame: %w", err)
	}
	if env.Event != "media" {
		return MediaFrame{}, fmt.Errorf("voice: decode media frame: event %q is not media", env.Event)
	}
	payload, err := base64.StdEncoding.DecodeString(env.Media.Payload)
	if err != nil {
		return MediaFrame{}, fmt.Errorf("voice: decode media payload: %w", err)
	}
	return MediaFrame{
		Payload:        payload,
		SequenceNumber: env.SequenceNumber,
		TimestampMs:    env.Media.Timestamp,
	}, nil
}

// SilenceFor returns n zero bytes representing duration of L16 s16le PCM at
// the given sample rate: the silence fill used by the reorder window when a
// sequence gap persists past reorder_gap_ms. Zero-filled by make.
func SilenceFor(duration time.Duration, sampleRate int) []byte {
	if duration <= 0 || sampleRate <= 0 {
		return nil
	}
	n := int(int64(duration) * int64(sampleRate) * 2 / int64(time.Second))
	if n <= 0 {
		return nil
	}
	return make([]byte, n)
}
