package builtin

import (
	"path/filepath"
	"strings"
	"unicode"
)

// tokenizer provides a minimal shell-like tokenizer that handles quoted
// strings and environment variable assignments. It is a lightweight
// replacement for github.com/mitchellh/go-shellwords until that dependency
// can be fetched.
type tokenizer struct {
	input string
	pos   int
}

func newTokenizer(input string) *tokenizer {
	return &tokenizer{input: input}
}

func (t *tokenizer) nextToken() (string, bool) {
	// Skip leading whitespace
	for t.pos < len(t.input) && unicode.IsSpace(rune(t.input[t.pos])) {
		t.pos++
	}
	if t.pos >= len(t.input) {
		return "", false
	}

	start := t.pos
	ch := t.input[t.pos]

	// Handle quoted strings
	if ch == '"' || ch == '\'' {
		quote := ch
		t.pos++ // skip opening quote
		for t.pos < len(t.input) {
			if t.input[t.pos] == '\\' && t.pos+1 < len(t.input) {
				t.pos += 2 // skip escaped char
				continue
			}
			if t.input[t.pos] == quote {
				t.pos++ // skip closing quote
				break
			}
			t.pos++
		}
		return t.input[start:t.pos], true
	}

	// Handle unquoted token (quotes may appear inline e.g. FOO='bar baz')
	for t.pos < len(t.input) && !unicode.IsSpace(rune(t.input[t.pos])) {
		if t.input[t.pos] == '\\' && t.pos+1 < len(t.input) {
			t.pos += 2
			continue
		}
		if t.input[t.pos] == '"' || t.input[t.pos] == '\'' {
			// Consume inline quoted portion as part of this token
			quote := t.input[t.pos]
			t.pos++
			for t.pos < len(t.input) {
				if t.input[t.pos] == '\\' && t.pos+1 < len(t.input) {
					t.pos += 2
					continue
				}
				if t.input[t.pos] == quote {
					t.pos++
					break
				}
				t.pos++
			}
			continue
		}
		t.pos++
	}
	return t.input[start:t.pos], true
}

// tokenizeShell splits a shell command string into tokens, preserving
// quoted substrings as single tokens. It is a minimal substitute for
// go-shellwords suitable for extracting the base command.
func tokenizeShell(command string) []string {
	t := newTokenizer(command)
	var tokens []string
	for {
		tok, ok := t.nextToken()
		if !ok {
			break
		}
		tokens = append(tokens, tok)
	}
	return tokens
}

// extractBaseCommand returns the base command name from a shell command string.
// It skips environment variable assignments (FOO=bar) and handles quoted
// arguments correctly.
func extractBaseCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	tokens := tokenizeShell(command)
	for _, tok := range tokens {
		if strings.Contains(tok, "=") && !strings.HasPrefix(tok, "-") {
			continue
		}
		return filepath.Base(tok)
	}
	return ""
}
