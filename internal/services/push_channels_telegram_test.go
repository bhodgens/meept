package services

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Pins the M6 fix: Telegram truncation must (a) cut on a rune boundary so
// the payload is always valid UTF-8 (Telegram 400s on invalid UTF-8) and
// (b) count the ESCAPED text against the 4096 limit so MarkdownV2 escape
// growth cannot push the payload over it.
func TestFormatForTelegram(t *testing.T) {
	const maxLen = 4096

	t.Run("short text passes through escaped", func(t *testing.T) {
		got := formatForTelegram("hello world")
		if got != "hello world" {
			t.Errorf("formatForTelegram(simple) = %q, want %q", got, "hello world")
		}
		if got := formatForTelegram("a.b"); got != `a\.b` {
			t.Errorf("escape: got %q, want %q", got, `a\.b`)
		}
	})

	t.Run("CJK cut mid-rune still valid UTF-8", func(t *testing.T) {
		// 4096 bytes is not a multiple of 3, so a naive text[:4096] of CJK
		// splits the final rune. The escaped form of CJK is identity (no
		// special chars), so the truncation path runs on pure multibyte.
		text := strings.Repeat("日", 2000) // 6000 bytes
		got := formatForTelegram(text)
		if !utf8.ValidString(got) {
			t.Fatalf("formatForTelegram(CJK) produced invalid UTF-8")
		}
		if len(got) > maxLen {
			t.Errorf("len = %d, want <= %d", len(got), maxLen)
		}
		// Every rune must be intact (no U+FFFD replacement chars).
		if strings.ContainsRune(got, utf8.RuneError) {
			t.Error("output contains replacement rune: a rune was split")
		}
	})

	t.Run("emoji (4-byte runes) cut stays valid", func(t *testing.T) {
		text := strings.Repeat("\U0001F600", 1500) // 6000 bytes
		got := formatForTelegram(text)
		if !utf8.ValidString(got) {
			t.Fatalf("formatForTelegram(emoji) produced invalid UTF-8")
		}
		if len(got) > maxLen {
			t.Errorf("len = %d, want <= %d", len(got), maxLen)
		}
	})

	t.Run("4090 special chars stay within 4096 post-format", func(t *testing.T) {
		// Each '.' escapes to 2 bytes; the pre-fix code truncated the raw
		// text to 4096 and THEN escaped, producing 8192+ bytes.
		text := strings.Repeat(".", 4090)
		got := formatForTelegram(text)
		if len(got) > maxLen {
			t.Errorf("len(escaped) = %d, want <= %d (escape expansion not accounted)", len(got), maxLen)
		}
	})

	t.Run("truncated output is exactly 4096 with ellipsis", func(t *testing.T) {
		text := strings.Repeat("a", 5000)
		got := formatForTelegram(text)
		if len(got) != maxLen {
			t.Errorf("len = %d, want exactly %d (payload + ...)", len(got), maxLen)
		}
		if !strings.HasSuffix(got, "...") {
			t.Error("truncated output missing ellipsis")
		}
	})

	t.Run("exactly at limit is not truncated", func(t *testing.T) {
		text := strings.Repeat("a", maxLen)
		got := formatForTelegram(text)
		if got != text {
			t.Errorf("text at exactly %d bytes was modified", maxLen)
		}
	})
}
