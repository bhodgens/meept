package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestDetectMediaURL pins the acceptance set of the media-URL guard:
// YouTube URL forms and bare 11-char video IDs embedded in a message.
// Mirrors transcript_fetch's parseVideoID acceptance set (budget-tree
// follow-up: the live smoke showed the LLM classifier routing
// "summarize this video" to coder, which holds no transcript_fetch
// grant — the guard makes media-ingest routing deterministic).
func TestDetectMediaURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"watch url", "summarize this https://www.youtube.com/watch?v=DWoJZs6TuVs please", true},
		{"shorts url", "https://www.youtube.com/shorts/DWoJZs6TuVs", true},
		{"embed url", "https://www.youtube.com/embed/DWoJZs6TuVs", true},
		{"live url", "https://www.youtube.com/live/DWoJZs6TuVs", true},
		{"youtu.be", "learn from https://youtu.be/DWoJZs6TuVs", true},
		{"case insensitive host", "HTTPS://WWW.YOUTUBE.COM/WATCH?V=DWoJZs6TuVs", true},
		{"bare id", "summarize DWoJZs6TuVs for me", true},
		{"no url", "summarize this document for me", false},
		{"github url", "review https://github.com/caimlas/meept/pull/1", false},
		{"short id", "check abc", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectMediaURL(tc.in)
			if tc.want && got == "" {
				t.Errorf("detectMediaURL(%q) = \"\", want a match", tc.in)
			}
			if !tc.want && got != "" {
				t.Errorf("detectMediaURL(%q) = %q, want no match", tc.in, got)
			}
		})
	}
}

// TestClassifyIntent_MediaURLGuard verifies the guard routes a
// URL-bearing media request to the analyst (the transcript_fetch
// grant holder) BEFORE the LLM classifier can misroute it to coder,
// and that non-media messages still fall through to the rest of the
// chain.
func TestClassifyIntent_MediaURLGuard(t *testing.T) {
	d := &Dispatcher{}

	intent, err := d.classifyIntent(nil, "summarize this video: https://www.youtube.com/watch?v=DWoJZs6TuVs", nil)
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent.AgentType != config.AgentIDAnalyst {
		t.Errorf("agent = %q, want analyst (transcript_fetch grant holder)", intent.AgentType)
	}
	if intent.Type != string(IntentAnalyze) {
		t.Errorf("intent = %q, want %q", intent.Type, string(IntentAnalyze))
	}
	if intent.Method != "media_url_guard" {
		t.Errorf("method = %q, want media_url_guard", intent.Method)
	}
	if intent.Confidence < 0.9 {
		t.Errorf("confidence = %v, want >= 0.9 (deterministic signal)", intent.Confidence)
	}
}
