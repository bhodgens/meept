package providers

// F2/F3 gate: NewSTT constructs the Rumble adapter from the keys-frame
// selector, and the pre-existing default posture is unchanged. RED for
// F2 (unknown vendor) until leaf 03 adds case "rumble".

import (
	"errors"
	"strings"
	"testing"
)

func TestNewSTT_Rumble(t *testing.T) {
	t.Run("with base URL", func(t *testing.T) {
		p, err := NewSTT(ProviderConfig{
			Vendor:     "rumble",
			Model:      "Reson8/Rumble-2-Flash",
			BaseURL:    "http://127.0.0.1:8000",
			SampleRate: 16000,
		}, "" /* keyless */)
		if err != nil {
			t.Fatalf("NewSTT(rumble): %v", err)
		}
		if p == nil {
			t.Fatal("NewSTT(rumble) returned a nil provider without an error")
		}
	})
	t.Run("without base URL", func(t *testing.T) {
		_, err := NewSTT(ProviderConfig{Vendor: "rumble", Model: "Reson8/Rumble-2-Flash"}, "")
		if err == nil {
			t.Fatal("NewSTT(rumble) with empty BaseURL must fail")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "base url") {
			t.Fatalf("error %q must name the missing base URL", err.Error())
		}
	})
}

func TestNewSTT_DefaultUnchanged(t *testing.T) {
	// Existing behavior: an empty vendor is unknown (the pipeline resolves
	// the default to "whisper" before the keys frame reaches the child).
	if _, err := NewSTT(ProviderConfig{}, ""); !errors.Is(err, ErrUnknownVendor) {
		t.Fatalf("empty vendor error = %v, want ErrUnknownVendor", err)
	}
	// Existing behavior: whisper needs stt_model; no base URL or key.
	if _, err := NewSTT(ProviderConfig{Vendor: "whisper"}, ""); !errors.Is(err, ErrLocalModelRequired) {
		t.Fatalf("whisper without model error = %v, want ErrLocalModelRequired", err)
	}
}
