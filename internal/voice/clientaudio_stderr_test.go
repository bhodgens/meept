package voice

import "testing"

// Pins the zero-byte-death diagnosis seam: boundedTail keeps only the
// last max bytes (ffmpeg error lines survive; stdout stays clean), and
// RawMicCapture.StderrTail exposes it after teardown - the CLI error must
// be able to NAME the capture's cause (TCC mic denial, dead device)
// instead of the silent "capture ended cleanly with zero bytes" false
// alarm the operator hit 2026-11-09.

func TestBoundedTailKeepsLastMax(t *testing.T) {
	b := &boundedTail{max: 8}
	for _, chunk := range []string{"aaaa", "bbbb", "cccc"} {
		n, err := b.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = (%d, %v)", chunk, n, err)
		}
	}
	if got := b.String(); got != "bbbbcccc" {
		t.Fatalf("String() = %q, want last 8 bytes \"bbbbcccc\"", got)
	}
}

func TestStderrTailFileModeEmpty(t *testing.T) {
	// File-mode captures arm no ffmpeg and buffer no stderr: the CLI
	// failure renderer must treat "" as "no cause to show".
	c := &RawMicCapture{}
	if got := c.StderrTail(); got != "" {
		t.Fatalf("file-mode StderrTail() = %q, want \"\"", got)
	}
}
