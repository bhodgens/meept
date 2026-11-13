package voicechild

// D1 regression (defect 2026-11-13): rmsEnergy wrapped negative samples
// through two's complement (-u mod 2^64) and SQUARED the wrapped value,
// so ANY chunk containing a negative sample read astronomically loud:
// room tone at rms~40 fabricated a 7.0e11 sum against the 2.88e9 the
// 3000-threshold needs, tripping rmsChunkOver (barge-in), the
// bargeInRms/4 mic-hold floor (noteMicEnergy), the rmsBargeInCheck echo
// window, and noteOutgoingChunk's echo reference. The amplitude floor
// was effectively 0 — only an all-zero chunk ever read quiet. Contract
// pinned here: the magnitude is |sample| squared in int32 BEFORE the
// widening, so a NEGATIVE-amplitude quiet chunk stays quiet and a loud
// +/- chunk stays loud. Existing TestRMSChunk covers zero+positive
// samples only; this extends, never weakens, that table.

import (
	"encoding/binary"
	"testing"
)

// mkPCM encodes n constant int16 samples (sign preserved) as s16le.
func mkPCM(t *testing.T, sample int, n int) []byte {
	t.Helper()
	b := make([]byte, n*2)
	for i := 0; i+1 < len(b); i += 2 {
		binary.LittleEndian.PutUint16(b[i:], uint16(int16(sample))) //nolint:gosec // G115: test fixture PCM encode of a constant int16 sample
	}
	return b
}

func TestRMSNegativeAmplitudeQuietStaysQuiet(t *testing.T) {
	// Quiet NEGATIVE-amplitude chunks: every one of these was LOUD under
	// the wrapped arithmetic (the fabricated sums ranged ~4.2e9 to ~7.0e11
	// per 320-sample chunk against the 2.88e9 need). All must be quiet.
	for _, sample := range []int{-50, -200, -500, -1500, -2000, -2999} {
		if rmsChunkOver(mkPCM(t, sample, 320), rmsThresh) {
			t.Fatalf("negative amplitude %d rated over threshold %d (quiet room-tone must not trip the VAD)", sample, rmsThresh)
		}
	}
	// noteMicEnergy's floor (bargeInRms/4) must stay quiet too: the
	// mic-hold lane rides the same helper. Amplitudes under the 750
	// floor stay under it (sign irrelevant now).
	floor := rmsThresh / 4
	for _, sample := range []int{-50, -500, -740} {
		if rmsChunkOver(mkPCM(t, sample, 320), floor) {
			t.Fatalf("negative amplitude %d rated over the mic-hold floor %d", sample, floor)
		}
	}
	// Loud NEGATIVE chunks must stay loud (|v| squared, no wrap).
	if !rmsChunkOver(mkPCM(t, -3000, 320), rmsThresh) {
		t.Fatal("constant -3000 rated under threshold; a caller shouting must trip")
	}
	if !rmsChunkOver(mkPCM(t, -32768, 320), rmsThresh) {
		t.Fatal("full-scale negative -32768 rated quiet; |v| = 32768 is maximally loud")
	}
	// Mixed-sign loud chunk (alternating +/-3000): loud both ways.
	alt := mkPCM(t, 3000, 320)
	negSample := int16(-3000)
	for i := 2; i+1 < len(alt); i += 4 {
		binary.LittleEndian.PutUint16(alt[i:], uint16(negSample)) //nolint:gosec // G115: test fixture flips the sample sign
	}
	if !rmsChunkOver(alt, rmsThresh) {
		t.Fatal("alternating +/-3000 rated under threshold")
	}
	// Exact identity check on the helper itself: -50 energy == +50 energy.
	neg, nNeg := rmsEnergy(mkPCM(t, -50, 320))
	pos, nPos := rmsEnergy(mkPCM(t, 50, 320))
	if neg != pos || nNeg != nPos {
		t.Fatalf("rmsEnergy(-50)=(%d,%d) != rmsEnergy(+50)=(%d,%d); sign must not change energy", neg, nNeg, pos, nPos)
	}
	// Full-scale negative bound: -32768 -> 32768^2 * 320, no wrap.
	sum, _ := rmsEnergy(mkPCM(t, -32768, 320))
	want := uint64(32768) * uint64(32768) * 320
	if sum != want {
		t.Fatalf("rmsEnergy(-32768) sum = %d, want %d (true magnitude squared)", sum, want)
	}
}
