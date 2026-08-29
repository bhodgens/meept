package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestHarnessHashDeterministic(t *testing.T) {
	a := HarnessHash("prompt", "tools", "gate")
	b := HarnessHash("prompt", "tools", "gate")
	if a != b {
		t.Errorf("same inputs produced different hashes: %q vs %q", a, b)
	}
}

func TestHarnessHashDiffers(t *testing.T) {
	base := HarnessHash("prompt", "tools", "gate")
	tests := []struct {
		name        string
		prompt      string
		toolList    string
		gateCommand string
	}{
		{name: "different prompt", prompt: "prompt2", toolList: "tools", gateCommand: "gate"},
		{name: "different tools", prompt: "prompt", toolList: "tools2", gateCommand: "gate"},
		{name: "different gate", prompt: "prompt", toolList: "tools", gateCommand: "gate2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HarnessHash(tt.prompt, tt.toolList, tt.gateCommand); got == base {
				t.Error("different inputs must produce a different hash")
			}
		})
	}
}

func TestHarnessHashFormat(t *testing.T) {
	h := HarnessHash("p", "t", "g")
	if len(h) != 64 {
		t.Fatalf("want 64-char sha256 hex, got %d chars: %q", len(h), h)
	}
	if _, err := hex.DecodeString(h); err != nil {
		t.Errorf("not valid hex: %v", err)
	}

	// Reference implementation: double sha256 of NUL-joined inputs.
	inner := sha256.Sum256([]byte("p\x00t\x00g"))
	outer := sha256.Sum256(inner[:])
	if want := hex.EncodeToString(outer[:]); h != want {
		t.Errorf("hash = %q, want %q", h, want)
	}
}
