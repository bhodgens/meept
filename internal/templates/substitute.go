package templates

import (
	"regexp"
	"strconv"
	"strings"
)

// sliceRe matches ${@:N} and ${@:N:L} patterns.
var sliceRe = regexp.MustCompile(`\$\{@:(\d+)(?::(\d+))?\}`)

// Substitute performs argument substitution on a template body.
//
// Supported patterns:
//
//	$1 .. $9  - positional argument (1-indexed)
//	$@        - all arguments joined by spaces
//	${@:N}    - arguments from index N onward
//	${@:N:L}  - L arguments starting at index N
//
// Unrecognized patterns are left as-is.
func Substitute(body string, args []string) string {
	// First handle ${@:N:L} and ${@:N} patterns.
	result := sliceRe.ReplaceAllStringFunc(body, func(match string) string {
		submatches := sliceRe.FindStringSubmatch(match)
		start, _ := strconv.Atoi(submatches[1])
		if start < 1 {
			start = 1
		}
		// Convert to 0-indexed.
		idx := start - 1
		if idx >= len(args) {
			return ""
		}
		if submatches[2] != "" {
			length, _ := strconv.Atoi(submatches[2])
			if length < 0 {
				length = 0
			}
			end := idx + length
			if end > len(args) {
				end = len(args)
			}
			return strings.Join(args[idx:end], " ")
		}
		return strings.Join(args[idx:], " ")
	})

	// Handle $@ (all args).
	result = strings.ReplaceAll(result, "$@", strings.Join(args, " "))

	// Handle $1-$9 positional args with a manual scanner.
	// We must ensure $N is not followed by another digit (so $50 is not
	// treated as $5 followed by 0). Go's regexp does not support negative
	// lookahead, so we scan character by character.
	result = replacePositional(result, args)

	return result
}

// replacePositional scans the input for $1-$9 that are not followed by
// another digit and replaces them with the corresponding positional argument.
func replacePositional(s string, args []string) string {
	var b strings.Builder
	b.Grow(len(s))

	i := 0
	for i < len(s) {
		if s[i] == '$' && i+1 < len(s) {
			digit := s[i+1]
			if digit >= '1' && digit <= '9' {
				// Check if followed by another digit.
				if i+2 < len(s) && s[i+2] >= '0' && s[i+2] <= '9' {
					// Not a positional pattern (e.g., $50). Write as-is.
					b.WriteByte(s[i])
					i++
					continue
				}
				// Valid positional $1-$9.
				idx := int(digit-'0') - 1 // 0-indexed.
				if idx < len(args) {
					b.WriteString(args[idx])
				}
				// If idx >= len(args), the placeholder is replaced with empty string.
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}

	return b.String()
}
