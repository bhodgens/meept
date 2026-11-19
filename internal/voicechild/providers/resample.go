package providers

import (
	"io"

	"github.com/gojargo/jargo/audio/resample"
)

// resampleChunkBytes bounds one Read call's work: the read loop feeds the
// streaming resampler at most this many source bytes before handing the
// caller what it has. 640 bytes = 20 ms at 16 kHz mono s16le, 13.3 ms at
// 24 kHz. The old 2880-byte (60 ms @ 24 kHz) granularity added up to 60 ms
// of first-audio latency and let the tail of a short utterance sit in this
// loop instead of reaching the caller.
const resampleChunkBytes = 640

// resampleClearAfter keeps the streaming filter's history across delivery
// gaps.
//
// TTS audio arrives in vendor HTTP chunks whose delivery intervals are gaps
// in DELIVERY, not gaps in the AUDIO, so an idle-window clear mid-utterance
// discards the filter history and is audible as a click. This leg wants a
// history that survives every delivery gap, which is what resample.NeverClear
// is for (config.go).
//
// jargo v0.1.0 read a zero ClearAfter as the 200 ms DefaultClearAfter, so
// "never clear" had to be spelled as a negative. v0.6.x inverted that: zero
// now never clears too, and NeverClear names the intent. Either value
// behaves identically here.
const resampleClearAfter = resample.NeverClear

// resampleBytesPerFrame is the frame size of the PCM this reader converts:
// s16le mono is 1 channel x 2 bytes. The leg is mono-only (both callers pass
// a mono rate pair), so the carry below never exceeds one byte.
const resampleBytesPerFrame = 2

// resampleCarryHeadroom is the spare prefix in the reusable scratch buffer
// where a carried sub-sample byte is placed just before the input goes to
// Process. s16le mono carries at most resampleBytesPerFrame-1 == 1 byte, so
// one spare byte suffices. Verified against jargo v0.6.1: Resampler.Process
// (sincStream.process) COPIES the input into per-channel []float32 buffers
// and returns a freshly allocated result - it never retains the input slice.
// Reusing scratch every pass is therefore safe. The carry itself lives in a
// SEPARATE tiny buffer so that a rate-pair passthrough (where Process returns
// the input slice itself) never has its pending output corrupted by the
// next carry stash.
const resampleCarryHeadroom = resampleBytesPerFrame - 1

// newResamplingReader wraps src (s16le mono at inRate) in a reader that
// emits s16le mono at outRate. It uses jargo's stateful streaming Resampler
// (pure Go), so conversion is chunked and incremental - the caller starts
// receiving converted audio without the whole stream being buffered.
type resamplingReader struct {
	src         io.Reader
	rs          *resample.Resampler
	pending     []byte // converted audio not yet handed to the caller
	scratch     []byte // reusable read/process buffer: source lands at scratch[headroom:], carry is prepended before it
	carry       []byte // sub-sample residue from a short source read (0..bytesPerFrame-1 bytes); own backing array
	outBuf      []byte // reusable staging buffer for passthrough output (Process returns `in` when rates match)
	passthrough bool   // inRate==outRate or resampler failed to build: Process returns the input slice itself
	srcErr      error  // sticky source error after pending drains
	done        bool
}

// ResampleS16Mono is the exported seam for newResamplingReader: it wraps
// src (s16le mono at inRate) in a reader that emits s16le mono at outRate.
// voicechild cannot import this package (providers imports voicechild for
// the factory interfaces — the reverse would cycle), so cmd/meept-voice
// injects this function through Child.SetResampler for the clip-playback
// path (plan 20261113-filler-clips leaf 03, adjudication A1: the clip
// store is canonical 16000 Hz; a call negotiated at any other rate
// resamples on read).
func ResampleS16Mono(src io.Reader, inRate, outRate int) (io.ReadCloser, error) {
	return newResamplingReader(src, inRate, outRate), nil
}

func newResamplingReader(src io.Reader, inRate, outRate int) *resamplingReader {
	// Allocate the read scratch, carry and passthrough buffers ONCE per
	// reader instead of a fresh make([]byte, 640) on every Read pass (F8).
	r := &resamplingReader{
		src:     src,
		scratch: make([]byte, resampleCarryHeadroom+resampleChunkBytes),
		carry:   make([]byte, 0, resampleBytesPerFrame),
		outBuf:  make([]byte, resampleChunkBytes+resampleBytesPerFrame),
	}
	rs, err := resample.NewWithConfig(inRate, outRate, 1, resample.Config{ClearAfter: resampleClearAfter})
	if err != nil {
		// The converter failed to build; degrade to passthrough rather
		// than fail the call - the rate mismatch surfaces as pitch shift,
		// which the daemon's rate negotiation prevents in practice.
		// rs stays nil; the carry path is inert, but Process-style
		// passthrough still returns `in`, so mark it accordingly.
		r.passthrough = true
		return r
	}
	// jargo's Resampler passes audio through unchanged (returning the input
	// slice itself) when the rates match (sincStream.f.passthrough). Detect
	// that case: the output must be copied out of the shared scratch before
	// the next Read overwrites it, or it corrupts pending audio.
	if inRate == outRate {
		r.passthrough = true
	}
	r.rs = rs
	return r
}

// Read implements io.Reader over the resampled stream.
//
// It pulls at most resampleChunkBytes from the source per pass and returns
// as soon as converted audio is available: io.ReadFull here would block
// until a whole 60 ms (now 640 B) frame arrived, delaying the first byte of
// audio and buffering an utterance shorter than one frame entirely. An
// empty pass (a source chunk with no whole sample, or a rate pair whose
// conversion yields nothing yet) loops back for more input rather than
// returning (0, nil), which io.Reader forbids.
//
// Sub-sample carry: the source is an HTTP body stream (qwen3-tts at 24 kHz)
// or a piper subprocess pipe, and neither is sample-aligned - a Read lands
// on whatever byte boundary the vendor chunk or the kernel pipe buffer
// happened to end at, so an ODD byte count is routine. jargo's Process
// computes inFrames = len(in)/bytesPerFrame and silently drops the trailing
// odd byte, which flips the parity of every subsequent boundary: from that
// point on the high byte of sample N is decoded as the low byte of sample
// N+1 for the REST OF THE UTTERANCE. That is permanent and self-propagating,
// and it is the audible fuzz/crackle the operator reported on locally played
// audio (the direct-from-vendor stream never enters this reader, so it stays
// clean). Measured on a 3 s 440 Hz tone: with all-even reads, 0 sample jumps
// over 2x the natural delta; with a SINGLE odd read anywhere, ~18 000 bad
// jumps and a worst jump of 15.5x natural, in every window after the odd
// read. So each pass is realigned here: any carried residue is prepended,
// the frame-aligned prefix goes to Process, and the leftover 0..1 bytes are
// stashed for the next pass. A trailing residue at EOF is a truncated
// half-sample holding no valid audio, so it is discarded, never fed on.
// Pinned by TestResamplingReaderOddReadsPreserveAlignment.
//
// This seam is shared by both local TTS vendors: the qwen3/OpenAI-shaped 24 kHz
// body stream (tts_openai.go) and a piper model whose native rate differs from
// the negotiated rate (tts_piper.go, newResamplingReader at :101). Fixing it
// here fixes both.
func (r *resamplingReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.done {
			return 0, r.srcErr
		}
		// Read into the reusable scratch, leaving the carry headroom at the
		// front. Source bytes land at scratch[headroom:headroom+n].
		n, err := r.src.Read(r.scratch[resampleCarryHeadroom : resampleCarryHeadroom+resampleChunkBytes])
		if n > 0 {
			out := r.convertAligned(r.scratch[resampleCarryHeadroom : resampleCarryHeadroom+n])
			if len(out) > 0 {
				r.pending = out
				break // hand the caller what we have; the rest stays in pending
			}
		}
		if err != nil {
			r.done = true
			// Discard a half-sample left at the stream end: Process would
			// drop it anyway, and keeping it alive would let it misalign a
			// stream that has already ended.
			r.carry = r.carry[:0]
			if err == io.EOF {
				r.srcErr = io.EOF
			} else {
				r.srcErr = err
			}
			if r.rs != nil {
				r.rs.Close()
			}
			// pending may hold audio converted from the tail above;
			// the loop condition drains it before reporting the error.
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// Close releases the source.
func (r *resamplingReader) Close() error {
	if r.rs != nil {
		r.rs.Close()
	}
	rc, ok := r.src.(io.ReadCloser)
	if ok {
		return rc.Close()
	}
	return nil
}

// convert runs one frame-aligned chunk through the streaming resampler
// (passthrough when the converter never built or the rates match).
func (r *resamplingReader) convert(in []byte) []byte {
	if r.passthrough {
		// Process (or the degraded no-resampler path) returns `in` itself,
		// which aliases the shared scratch. Stage a copy in outBuf so the
		// next Read cannot overwrite audio still pending for the caller.
		n := copy(r.outBuf, in)
		return r.outBuf[:n]
	}
	out := r.rs.Process(in)
	if out == nil {
		return nil
	}
	return out
}

// convertAligned realigns one source chunk to whole s16le frames, prepending
// any residue carried from the previous chunk and stashing the residue this
// chunk leaves over. See Read for why skipping this is a permanent corruption
// rather than a dropped byte.
func (r *resamplingReader) convertAligned(in []byte) []byte {
	if r.rs == nil {
		// Passthrough degrade path: with no resampler there is no frame
		// boundary to honour, so bytes are forwarded unchanged and nothing
		// is carried.
		return r.convert(in)
	}
	// Prepend any carried residue. The carry lives in its own small buffer;
	// copy it into the scratch headroom immediately before `in` so the
	// frame-aligned sequence handed to Process is byte-identical to the old
	// "joined := carry + in" path, without allocating.
	if len(r.carry) > 0 {
		copy(r.scratch[resampleCarryHeadroom-len(r.carry):resampleCarryHeadroom], r.carry)
		in = r.scratch[resampleCarryHeadroom-len(r.carry) : resampleCarryHeadroom+len(in)]
		r.carry = r.carry[:0]
	}
	rem := len(in) % resampleBytesPerFrame
	if rem > 0 {
		// Stash the trailing sub-sample byte in the dedicated carry buffer,
		// never inside `in`: on a rate-pair passthrough Process can return
		// `in` itself, so writing the carry into it would corrupt the
		// audio still pending for the caller. append reuses the preallocated
		// backing array (cap >= rem), so this does not allocate.
		r.carry = append(r.carry, in[len(in)-rem:]...)
		in = in[:len(in)-rem]
	}
	if len(in) == 0 {
		return nil
	}
	return r.convert(in)
}

// compile-time: the reader satisfies io.ReadCloser.
var _ io.ReadCloser = (*resamplingReader)(nil)
