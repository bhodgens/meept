package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestDetectMediaURL pins the acceptance set of the media-URL guard:
// YouTube URL forms unconditionally, and bare 11-char video IDs only when
// the message also carries media context (H5). An ungated 11-char
// alternation matched ordinary prose — "the development plan", "fix the
// application error", "performance", "handleClick", 11-char git SHAs —
// hijacking those messages to the analyst at 0.9 before the LLM chain
// could classify them.
func TestDetectMediaURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// URL forms: ungated.
		{"watch url", "summarize this https://www.youtube.com/watch?v=DWoJZs6TuVs please", true},
		{"shorts url", "https://www.youtube.com/shorts/DWoJZs6TuVs", true},
		{"embed url", "https://www.youtube.com/embed/DWoJZs6TuVs", true},
		{"live url", "https://www.youtube.com/live/DWoJZs6TuVs", true},
		{"youtu.be", "learn from https://youtu.be/DWoJZs6TuVs", true},
		{"case insensitive host", "HTTPS://WWW.YOUTUBE.COM/WATCH?V=DWoJZs6TuVs", true},
		{"url without media context words", "review https://youtu.be/DWoJZs6TuVs before standup", true},

		// Bare-ID form: requires media-context co-occurrence.
		{"bare id with video context", "summarize this video DWoJZs6TuVs", true},
		{"bare id with transcript context", "transcript for DWoJZs6TuVs", true},
		{"bare id with youtube context", "what is the youtube id DWoJZs6TuVs about", true},
		{"bare id uppercase context", "WATCH DWoJZs6TuVs and summarize", true},

		// Bare-ID form without media context: must NOT match (H5).
		{"the development plan", "the development plan", false},
		{"fix the application error", "fix the application error", false},
		{"review handleClick please", "review handleClick please", false},
		{"performance", "improve performance", false},
		{"git sha no context", "revert commit a1B2c3D4e5F6 from main", false},
		{"eleven chars alone", "DWoJZs6TuVs", false},
		{"collaboration", "thanks for the collaboration", false},

		// Unrelated input.
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
	// NewDispatcher, not a bare struct: classifyIntent logs via d.logger,
	// which a zero-value Dispatcher leaves nil.
	d := NewDispatcher(DispatcherConfig{})

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

// TestClassifyIntent_BareVideoIDWithContextGuard verifies the bare-ID
// form routes to the analyst only when media context co-occurs, and that
// ordinary 11-char prose words are left to the normal chain (H5).
func TestClassifyIntent_BareVideoIDWithContextGuard(t *testing.T) {
	// NewDispatcher, not a bare struct: classifyIntent logs via d.logger,
	// which a zero-value Dispatcher leaves nil.
	d := NewDispatcher(DispatcherConfig{})

	// Positive: media context + bare ID → analyst.
	intent, err := d.classifyIntent(nil, "summarize this video DWoJZs6TuVs for me", nil)
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent.Method != "media_url_guard" || intent.AgentType != config.AgentIDAnalyst {
		t.Errorf("bare ID with context: agent=%q method=%q, want analyst/media_url_guard",
			intent.AgentType, intent.Method)
	}

	// Negative: an 11-char word in plain prose must NOT hijack to analyst.
	negatives := []string{
		"the development plan",
		"fix the application error",
		"review handleClick please",
	}
	for _, in := range negatives {
		intent, err := d.classifyIntent(nil, in, nil)
		if err != nil {
			t.Fatalf("classifyIntent(%q): %v", in, err)
		}
		if intent.Method == "media_url_guard" {
			t.Errorf("classifyIntent(%q) matched media guard; plain prose must fall through", in)
		}
		if intent.AgentType == config.AgentIDAnalyst {
			t.Errorf("classifyIntent(%q) routed to analyst; want non-media routing", in)
		}
	}
}
