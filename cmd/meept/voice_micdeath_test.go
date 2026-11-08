package main

// Pins the live-mode mic-death failure condition (2026-11-08 live defect:
// a broken brew ffmpeg died at exec, io.Copy returned nil EOF, and the CLI
// printed "call ended." with NO error - the operator could not tell the
// capture pipeline was broken). classifyMicDeath is the seam: every
// zero-byte mic death MUST surface as ErrMicNoAudio, never a clean return.
import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestClassifyMicDeath(t *testing.T) {
	t.Parallel()
	brokenFFmpeg := errors.New("dyld: Library not loaded: libx265.216.dylib")
	tests := []struct {
		name       string
		micErr     error
		micBytes   int64
		wantErr    bool
		wantSentel bool   // ErrMicNoAudio wrapped
		wantInText string // substring the message must carry
	}{
		{
			name:       "zero-byte EOF: broken ffmpeg exits cleanly - the 2026-11-08 live defect",
			micErr:     nil,
			micBytes:   0,
			wantErr:    true,
			wantSentel: true,
			wantInText: "capture ended cleanly with zero bytes",
		},
		{
			name:       "zero-byte error: capture process died at exec",
			micErr:     brokenFFmpeg,
			micBytes:   0,
			wantErr:    true,
			wantSentel: true,
			wantInText: "libx265",
		},
		{
			name:     "clean EOF after audio: normal hangup path, no error",
			micErr:   nil,
			micBytes: 32000,
			wantErr:  false,
		},
		{
			name:       "error after audio: real stream failure surfaces",
			micErr:     io.ErrClosedPipe,
			micBytes:   32000,
			wantErr:    true,
			wantSentel: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := classifyMicDeath(tt.micErr, tt.micBytes)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("classifyMicDeath(%v, %d) = %v, want nil", tt.micErr, tt.micBytes, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("classifyMicDeath(%v, %d) = nil, want an error", tt.micErr, tt.micBytes)
			}
			if tt.wantSentel && !errors.Is(err, ErrMicNoAudio) {
				t.Fatalf("classifyMicDeath(%v, %d) = %v, want errors.Is(err, ErrMicNoAudio)", tt.micErr, tt.micBytes, err)
			}
			if tt.wantInText != "" && !strings.Contains(err.Error(), tt.wantInText) {
				t.Fatalf("classifyMicDeath(%v, %d) = %q, want text containing %q", tt.micErr, tt.micBytes, err, tt.wantInText)
			}
		})
	}
}

// The failure the 2026-11-08 operator run hit: a NIL mic error with zero
// bytes must never classify as success. This is the exact regression that
// printed "call ended." with no diagnosis.
func TestClassifyMicDeathZeroByteNilErrorNeverClean(t *testing.T) {
	t.Parallel()
	if err := classifyMicDeath(nil, 0); err == nil {
		t.Fatal("classifyMicDeath(nil, 0) = nil: a zero-byte mic death silently ending the call is the pinned regression")
	} else if !errors.Is(err, ErrMicNoAudio) {
		t.Fatalf("classifyMicDeath(nil, 0) = %v, want ErrMicNoAudio wrap", err)
	}
}
