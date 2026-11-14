package clipstore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestMetaJSONShape pins Contract C2's byte-exact json field names:
// {key, text, vendor, model, voice, sample_rate, bytes, created_at}.
// The child (leaf 03) writes and leaves 02/04 read these files; a
// renamed field silently breaks the cache-hit validation.
func TestMetaJSONShape(t *testing.T) {
	m := Meta{
		Key:        strings.Repeat("a", 64),
		Text:       "Just a sec.",
		Vendor:     "qwen3",
		Model:      "qwen3-tts-flash",
		Voice:      "CloneVoice",
		SampleRate: 24000,
		Bytes:      640,
		CreatedAt:  time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	for _, field := range []string{
		`"key":`, `"text":`, `"vendor":`, `"model":`, `"voice":`,
		`"sample_rate":24000`, `"bytes":640`, `"created_at":`,
	} {
		if !strings.Contains(got, field) {
			t.Errorf("meta json missing %s in %s", field, got)
		}
	}

	// Round-trip from the C2 wire shape.
	var back Meta
	if err := json.Unmarshal([]byte(`{"key":"k","text":"t","vendor":"v","model":"m",`+
		`"voice":"w","sample_rate":24000,"bytes":640,"created_at":"2026-11-13T12:00:00Z"}`), &back); err != nil {
		t.Fatalf("unmarshal wire shape: %v", err)
	}
	if back.Key != "k" || back.Text != "t" || back.Vendor != "v" || back.Model != "m" ||
		back.Voice != "w" || back.SampleRate != 24000 || back.Bytes != 640 ||
		!back.CreatedAt.Equal(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("wire round-trip mismatch: %+v", back)
	}
}
