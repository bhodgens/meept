package slack

import (
	"strings"
	"unicode/utf8"
)

// escapeSlackText strips control characters and bounds the length so an
// agent-controlled summary can never inject Block Kit structure or blow the
// message size cap. The bound is on runes so the cut never splits a UTF-8
// sequence.
func escapeSlackText(s string) string {
	const maxRunes = 600
	var b strings.Builder
	b.Grow(len(s))
	runes := 0
	for _, r := range s {
		if runes >= maxRunes {
			b.WriteString(" ...")
			break
		}
		if r < 0x20 && r != '\n' && r != '\t' {
			r = ' '
		}
		b.WriteRune(r)
		runes++
	}
	out := b.String()
	if !utf8.ValidString(out) {
		return ""
	}
	return out
}

// truncateRunes bounds s to max runes on a rune boundary.
func truncateRunes(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + " ..."
}
