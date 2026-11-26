package signalleg

// bughunt_1125_resampler_pin_test.go — A4-4 (bughunt 2026-11-25): the
// 48k->16k resampler used to discard the un-consumed input tail of every
// chunk (2 of 320 samples at 48k->16k), losing ~0.6 % of the stream and
// re-zeroing the interpolation phase per chunk. The fix carries the tail
// (r.rem) into the next Write. Pins:
//
//  1. no sample loss by total count: a long ramp fed in 320-sample
//     chunks produces the SAME output sample count as one single-Write
//     pass over the identical bytes (the with-fix vs reference equality,
//     exact, not approximate).
//  2. no per-chunk phase jump: the chunked output is byte-identical to
//     the single-Write output — a phase re-zero changes values at every
//     chunk boundary, so this fails loudly on any regression.
//  3. the OLD loss shape is documented by the assertion margin: the
//     removed behavior dropped 2 samples per 320 (0.625 %); the count
//     check pins the exact equality the carry guarantees.

import (
	"encoding/binary"
	"testing"
)

// rampPCM builds n s16le samples of a low-frequency ramp (values climb
// slowly so interpolation results are sensitive to phase — a phase jump
// changes output values, not just counts).
func rampPCM(n int) []byte {
	b := make([]byte, 2*n)
	for i := range n {
		// ~1 kHz-ish progression at 48k: wraps every 480 samples, so the
		// linear interpolator sees real slope at every position.
		v := int16((i % 480) * 60)
		binary.LittleEndian.PutUint16(b[2*i:], uint16(v)) //nolint:gosec // G115: test fixture
	}
	return b
}

func TestResamplerChunkTailCarry(t *testing.T) {
	const total = 320 * 40 // 40 chunks of 320 samples (the exact defect geometry: 320/3 = 106 steps + 2 leftover samples per chunk)
	src := rampPCM(total)

	// Reference: ONE Write over the whole stream.
	ref, err := NewResampler(48000, 16000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	want := ref.Write(src)

	// Chunked: same bytes through fresh instance in 320-sample chunks.
	ch, err := NewResampler(48000, 16000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	var got []byte
	for i := 0; i < len(src); i += 640 { // 640 bytes = 320 samples
		end := i + 640
		if end > len(src) {
			end = len(src)
		}
		got = append(got, ch.Write(src[i:end])...)
	}

	// (1) no sample loss by total count — EXACT equality with the
	// single-Write reference (the old shape lost ~2/320 = 0.625 %).
	if len(got) != len(want) {
		t.Fatalf("chunked output = %d samples, single-Write reference = %d samples (the un-consumed chunk tail is being dropped again)",
			len(got)/2, len(want)/2)
	}
	// (2) no per-chunk phase jump: byte-identical values.
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunked output diverges at byte %d (sample %d): got %d want %d — interpolation phase restarted at a chunk boundary",
				i, i/2, got[i], want[i])
		}
	}
}

func TestResamplerTailCarryOversample(t *testing.T) {
	// Upsampling (16k->48k, step = 1/3) with the same chunk shape. The
	// last-sample CLAMP posture is pinned production behavior (a deferred
	// final output would strand the partial wire chunk unflushed at EOF —
	// TestOutboundAudioBackpressureEndsLeg), so chunked output may differ
	// from the single-Write reference ONLY by the clamp error at chunk
	// boundaries: at most one divergent output per boundary, and each
	// divergence bounded by one ramp step (frac * slope < slope). A
	// re-zeroed per-chunk phase (the old shape) diverges by whole ramp
	// spans at every edge and fails the count check outright.
	const chunks = 30
	const perChunk = 320
	const slope = 3
	total := chunks * perChunk
	src := make([]byte, 2*total)
	for i := range total {
		binary.LittleEndian.PutUint16(src[2*i:], uint16(int16(i*slope))) //nolint:gosec // G115: fixture within range (9600*3 < 32767)
	}

	ref, err := NewResampler(16000, 48000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	want := ref.Write(src)

	ch, err := NewResampler(16000, 48000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	var got []byte
	for i := 0; i < len(src); i += 640 {
		end := i + 640
		if end > len(src) {
			end = len(src)
		}
		got = append(got, ch.Write(src[i:end])...)
	}
	// (1) EXACT count: no sample loss (the old shape re-zeroed pos per
	// chunk; the carry must keep the 3x expansion continuous).
	if len(got) != len(want) {
		t.Fatalf("upsample chunked output = %d samples, reference = %d (count drift)", len(got)/2, len(want)/2)
	}
	// (2) divergence ONLY at chunk boundaries, bounded by one ramp step.
	samp := func(b []byte, i int) int16 { return int16(binary.LittleEndian.Uint16(b[2*i:])) } //nolint:gosec // G115: test helper
	diverged := 0
	for i := range len(want) / 2 {
		a, b := samp(got, i), samp(want, i)
		if a != b {
			diverged++
			if d := int(a) - int(b); d > slope || d < -slope {
				t.Fatalf("sample %d diverges by %d (> one %d-step ramp slope): got %d want %d — phase discontinuity", i, d, slope, a, b)
			}
		}
	}
	if diverged > chunks+1 {
		t.Fatalf("%d divergent samples over %d chunk boundaries (>1 per edge): the carry phase is wrong", diverged, chunks)
	}
}

func TestResamplerTailCarryWithOddSplit(t *testing.T) {
	// The A1 odd-byte carry and the A4-4 sample carry must COEXIST: feed
	// a stream split at an ODD byte boundary per chunk (a sample spans two
	// Writes) and compare against the single-Write reference.
	const total = 321 * 25 // odd sample counts per chunk region
	src := rampPCM(total)

	ref, err := NewResampler(48000, 16000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	want := ref.Write(src)

	ch, err := NewResampler(48000, 16000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	var got []byte
	for i := 0; i < len(src); i += 643 { // 643 = odd: the A1 split shape
		end := i + 643
		if end > len(src) {
			end = len(src)
		}
		got = append(got, ch.Write(src[i:end])...)
	}
	if len(got) != len(want) {
		t.Fatalf("odd-split chunked output = %d samples, reference = %d", len(got)/2, len(want)/2)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("odd-split output diverges at byte %d: got %d want %d", i, got[i], want[i])
		}
	}
}
