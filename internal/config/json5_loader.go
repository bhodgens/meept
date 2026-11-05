package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/hujson"
)

// LoadJSON5 reads a JSON5 file, expands environment variables, standardizes to JSON, and unmarshals into v.
func LoadJSON5(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s: %w", path, err)
		}
		return fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Expand env vars in raw content
	content, err := ExpandEnvVars(string(data))
	if err != nil {
		return fmt.Errorf("failed to expand env vars in config %s: %w", path, err)
	}

	// Convert Go-style duration values ("30s", "1h", quoted or bare) to
	// nanosecond integers so time.Duration fields unmarshal. Same
	// preprocessing as UnmarshalJSON5 — without it, any config loaded
	// through this path rejects duration strings that the TOML path accepts
	// (found via [skills.evolver] interval in a -c config, 2026-08-29).
	content = preprocessDurations(content)

	// Standardize JSON5 to JSON
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5 config %s: %w (hujson accepts comments (// and /* */) and trailing commas; keys must be quoted; check for syntax errors near the reported position)", path, err)
	}

	// Promote a top-level "groups" alias into security.groups (agent-groups
	// 03-mutation-gate) so every JSON5 load path sees the section at its
	// schema home — see promoteTopLevelGroups.
	stdJSON, err = promoteTopLevelGroups(stdJSON)
	if err != nil {
		return fmt.Errorf("failed to normalize groups section in %s: %w", path, err)
	}

	// Unmarshal with detailed error handling for type mismatches
	if err := json.Unmarshal(stdJSON, v); err != nil {
		return wrapJSONUnmarshalError(err, path)
	}
	// Clamp memory.tiers to defaults when the destination is a full
	// *Config (wiring-gap leaf 03-tierpolicy-daemon T1): LoadJSON5 is
	// shared by partial unmarshals, so the normalize is type-asserted
	// rather than forced.
	if cfg, ok := v.(*Config); ok {
		NormalizeMemoryTiersDefaults(&cfg.Memory.Tiers)
	}
	return nil
}

// stringDurationKeys are config keys declared as STRING fields whose values
// happen to look like Go durations and must stay strings. preprocessDurations
// must not rewrite their values to nanosecond integers — the schema declares
// string and json.Unmarshal rejects the number (configui save/load roundtrip
// regression, tree 04 leaf 01 follow-up). Keys are the bare field names;
// duration-typed fields (time.Duration) still convert in both passes.
//
// IMPORTANT: when adding a duration-STRING config field, register it here —
// otherwise a quoted value like "5m" silently becomes an integer and config
// load fails. Known members:
//
//	queue.interactive_window          — held as string, parsed at use site
//	oauth.refresh_interval            — time.ParseDuration at wiring (components.go)
//	oauth.refresh_margin              — time.ParseDuration at wiring (components.go)
//	agent.worker_pool.idle_timeout    — time.ParseDuration (agent_manager_wiring.go)
//	intake.poll_interval              — time.ParseDuration at wiring (leaf 05)
var stringDurationKeys = map[string]bool{
	"interactive_window": true,
	"refresh_interval":   true,
	"refresh_margin":     true,
	"idle_timeout":       true,
	"poll_interval":      true,
}

// preprocessDurations applies the shared duration preprocessing used by both
// config load paths: bare duration tokens get quoted, then quoted duration
// values become nanosecond integers (stringDurationKeys exempt).
//
// It is a PRE-processed text transform: the output still feeds the
// hujson/json5 parser, exactly as before. The implementation is a minimal
// tokenizer over the raw bytes — see quoteBareDurations and
// convertQuotedDurations. The previous line-by-line pass broke on compact
// single-line JSON5 ({retry:30s,warn:1h}) and skipped rewriting whenever a
// quoted value appeared earlier on the same line.
func preprocessDurations(content string) string {
	content = quoteBareDurations(content)
	return convertQuotedDurations(content)
}

// quoteBareDurations walks the raw JSON5 text with a minimal tokenizer that
// tracks double-quoted strings (with backslash escapes) and both comment
// forms (// to end of line and /* ... */), so only real value positions are
// considered. Unlike the line-based pass it replaced, this handles compact
// single-line objects ({retry:30s,warn:1h}), duration arrays ([30s, 1h]),
// trailing commas, and quoted values appearing earlier on the same line.
//
// Bare duration tokens in value position (after ':', ',', or '[') get
// quoted, EXCEPT under a stringDurationKeys member — those values stay
// verbatim so the declared string field keeps its text. Already-quoted
// values are untouched here. Brace/bracket depth is intentionally not
// tracked: a value ends at the first comma/closing brace/closing bracket,
// which needs only the string/comment state; unbalanced input fails later
// in hujson.Standardize.
func quoteBareDurations(content string) string {
	var b strings.Builder
	b.Grow(len(content))

	i := 0
	for i < len(content) {
		switch c := content[i]; {
		case c == '"':
			start := i
			i++
			for i < len(content) && content[i] != '"' {
				if content[i] == '\\' {
					i++
					if i >= len(content) {
						// Truncated input ending in a bare backslash:
						// stop scanning; the string passes through
						// verbatim and hujson reports the real error.
						break
					}
				}
				i++
			}
			if i < len(content) {
				i++ // closing quote
			}
			b.WriteString(content[start:i])
		case c == '/' && i+1 < len(content) && content[i+1] == '/':
			cstart := i
			for i < len(content) && content[i] != '\n' {
				i++
			}
			b.WriteString(content[cstart:i])
		case c == '/' && i+1 < len(content) && content[i+1] == '*':
			cstart := i
			i += 2
			for i+1 < len(content) && (content[i] != '*' || content[i+1] != '/') {
				i++
			}
			if i+1 < len(content) {
				i += 2 // "*/"
			} else {
				i = len(content)
			}
			// Comments pass through verbatim: hujson strips them at parse
			// time, and the preprocessed text should stay faithful to the
			// source (error messages surface it).
			b.WriteString(content[cstart:i])
		case isIdentStart(c):
			start := i
			for i < len(content) && isIdentPart(content[i]) {
				i++
			}
			word := content[start:i]
			b.WriteString(word)
		case isDigit(c):
			if end := bareDurationEnd(content, i); end >= 0 &&
				bareValueTerminated(content, end) && bareAtValuePos(content, i) {
				// Bare duration in value position: quote it so hujson
				// accepts it. convertQuotedDurations turns the quotes into
				// a nanosecond integer — EXCEPT under stringDurationKeys,
				// where the value stays a quoted string (matching the old
				// pass, which quoted exempt-key values but skipped the
				// conversion).
				b.WriteByte('"')
				b.WriteString(content[i:end])
				b.WriteByte('"')
				i = end
				continue
			}
			b.WriteByte(c)
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// bareValueTerminated reports whether a bare duration token ending at i is
// actually a complete JSON value (followed by a value terminator rather
// than more token characters).
func bareValueTerminated(data string, i int) bool {
	if i >= len(data) {
		return true
	}
	switch data[i] {
	case ' ', '	', '\r', '\n', ',', '}', ']', '/':
		return true
	}
	return false
}

// bareAtValuePos reports whether the bare token starting at i sits in JSON
// value position: the previous non-space byte is ':', ',', or '['.
func bareAtValuePos(data string, i int) bool {
	switch prevNonSpace(data, i) {
	case ':', ',', '[':
		return true
	}
	return false
}

// convertQuotedDurations rewrites quoted duration VALUES ("30s") that follow
// a ':' in value position to their nanosecond integer form, using the same
// tokenizer state so strings inside strings and comment text are ignored.
// Values under a stringDurationKeys member stay quoted verbatim, and
// strings containing backslash escapes are never converted (configs use the
// escape spelling to smuggle duration text into string fields). Compact
// JSON5 ({interval:"1h",warn:"30s"}) works: each value is anchored to the
// colon immediately before it, not to the first colon of a line.
func convertQuotedDurations(data string) string {
	var b strings.Builder
	b.Grow(len(data))

	curKey := ""
	i := 0
	for i < len(data) {
		switch c := data[i]; {
		case c == '"':
			start := i
			i++
			for i < len(data) && data[i] != '"' {
				if data[i] == '\\' {
					i++
					if i >= len(data) {
						// Truncated input ending in a bare backslash:
						// stop scanning; the string passes through
						// verbatim and hujson reports the real error.
						break
					}
				}
				i++
			}
			if i < len(data) {
				i++ // closing quote
			}
			// A string followed by ':' is a member name.
			if j := skipJSONSpace(data, i); j < len(data) && data[j] == ':' {
				b.WriteString(data[start:i])
				curKey = data[start+1 : i-1]
				continue
			}
			// A duration VALUE must directly follow ':'; array-element
			// strings ("1h" inside ["1h"]) are left as strings.
			if prevNonSpace(data, start) != ':' ||
				!isSimpleQuotedString(data[start:i]) ||
				stringDurationKeys[curKey] {
				b.WriteString(data[start:i])
				continue
			}
			token := data[start+1 : i-1]
			d, err := parseDuration(token)
			if err != nil {
				b.WriteString(data[start:i])
				continue
			}
			fmt.Fprintf(&b, "%d", d)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			cstart := i
			for i < len(data) && data[i] != '\n' {
				i++
			}
			b.WriteString(data[cstart:i])
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			cstart := i
			i += 2
			for i+1 < len(data) && (data[i] != '*' || data[i+1] != '/') {
				i++
			}
			if i+1 < len(data) {
				i += 2 // "*/"
			} else {
				i = len(data)
			}
			b.WriteString(data[cstart:i])
		case isIdentStart(c):
			start := i
			for i < len(data) && isIdentPart(data[i]) {
				i++
			}
			word := data[start:i]
			b.WriteString(word)
			// Bare member name: remember it so the stringDurationKeys
			// exemption applies to bare keys too (interactive_window: "5m").
			if j := skipJSONSpace(data, i); j < len(data) && data[j] == ':' {
				curKey = word
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// skipJSONSpace skips JSON whitespace (space, tab, CR, LF) from i.
func skipJSONSpace(data string, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

// prevNonSpace returns the byte at the previous non-JSON-whitespace
// position, or 0 at the start of input.
func prevNonSpace(data string, i int) byte {
	j := i - 1
	for j >= 0 {
		switch data[j] {
		case ' ', '\t', '\r', '\n':
			j--
		default:
			return data[j]
		}
	}
	return 0
}

// bareDurationEnd returns the exclusive end index of a bare duration token
// starting at i (digits + duration suffix, optionally followed by more
// digits+suffix units), or -1 if the token at i is not a duration.
func bareDurationEnd(data string, i int) int {
	for {
		digits := 0
		for i < len(data) && isDigit(data[i]) {
			i++
			digits++
		}
		if digits == 0 {
			return -1
		}
		if i < len(data) && data[i] == '.' {
			i++
			frac := 0
			for i < len(data) && isDigit(data[i]) {
				i++
				frac++
			}
			if frac == 0 {
				return -1
			}
		}
		suffix := durationSuffixEnd(data, i)
		if suffix < 0 {
			return -1
		}
		i = suffix
		// A following digits+suffix sequence is a compound unit ("1h30m");
		// anything else (comma, brace, space, newline) ends the value.
		if i >= len(data) || !isDigit(data[i]) {
			return i
		}
	}
}

// durationSuffixEnd returns the exclusive end index of a duration suffix
// (ms, us, µs, ns, s, m, h, d) at i, longest match first, or -1.
func durationSuffixEnd(data string, i int) int {
	for _, suf := range []string{"ms", "us", "µs", "ns", "s", "m", "h", "d"} {
		if strings.HasPrefix(data[i:], suf) {
			return i + len(suf)
		}
	}
	return -1
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		c >= 0x80 // JSON5 allows Unicode identifiers; treat high bytes as ident chars
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c) || c == '-'
}

// isSimpleQuotedString reports whether data[start:close] is a JSON string
// containing no backslash escapes (\"2m\" etc. must never be converted).
func isSimpleQuotedString(seg string) bool {
	if len(seg) < 2 || seg[0] != '"' || seg[len(seg)-1] != '"' {
		return false
	}
	return strings.IndexByte(seg[1:len(seg)-1], '\\') < 0
}

// wrapJSONUnmarshalError provides detailed, user-friendly error messages for JSON unmarshaling failures.
func wrapJSONUnmarshalError(err error, configPath string) error {
	errMsg := err.Error()

	// Extract field information from error message
	var fieldInfo string
	if idx := strings.Index(errMsg, "into"); idx != -1 {
		// Error format: "json: cannot unmarshal X into Go struct field Y.Z of type T"
		remainder := errMsg[idx:]
		if strings.Contains(remainder, "field") {
			parts := strings.Split(remainder, " ")
			for i, part := range parts {
				if part == "field" && i+1 < len(parts) {
					fieldInfo = parts[i+1]
					break
				}
			}
		}
	}

	// Build context-aware error messages
	var detailMsg string
	var hintMsg string

	switch {
	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type bool") && strings.Contains(errMsg, "array"):
		detailMsg = "expected a boolean value (true/false) but found an array [list]"
		hintMsg = "Hint: This field should be a single true/false value, not a list. Remove the square brackets [] or change to true/false."

	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type bool") && strings.Contains(errMsg, "string"):
		detailMsg = "expected a boolean value (true/false) but found a string"
		hintMsg = "Hint: This field should be true or false (without quotes). If you're trying to set an enum value like 'ask', 'never', or 'always', check the config documentation for valid options."

	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type int") && strings.Contains(errMsg, "string"):
		detailMsg = "expected an integer value but found a string"
		hintMsg = "Hint: Remove quotes around numeric values. For durations, use the raw number or a quoted duration string like \"30s\" if the field supports it."

	case strings.Contains(errMsg, "cannot unmarshal") && strings.Contains(errMsg, "type []string") && strings.Contains(errMsg, "string"):
		detailMsg = "expected an array of strings but found a single string"
		hintMsg = "Hint: Wrap the value in square brackets: [\"value\"] or add more items: [\"value1\", \"value2\"]"

	case strings.Contains(errMsg, "cannot unmarshal"):
		// Generic type mismatch - extract as much info as possible
		detailMsg = fmt.Sprintf("type mismatch: %s", extractTypeMismatch(errMsg))
		hintMsg = "Hint: Check that the value type matches what the field expects (bool, int, string, array, or object)."

	case strings.Contains(errMsg, "unknown field"):
		detailMsg = "unknown configuration field"
		hintMsg = "Hint: This field name is not recognized. Check for typos or see if this feature requires a newer version."

	default:
		detailMsg = errMsg
		hintMsg = "Hint: Review the config file syntax and field values."
	}

	// Build the detailed error message
	var sb strings.Builder
	fmt.Fprintf(&sb, "failed to parse config %s:\n", configPath)
	if fieldInfo != "" {
		fmt.Fprintf(&sb, "  Field: %s\n", fieldInfo)
	}
	fmt.Fprintf(&sb, "  Detail: %s\n", detailMsg)
	fmt.Fprintf(&sb, "  %s", hintMsg)

	return fmt.Errorf("%s", sb.String())
}

// extractTypeMismatch extracts the core type information from a Go json.Unmarshal error.
func extractTypeMismatch(errMsg string) string {
	// Parse error like: "json: cannot unmarshal string into Go struct field Config.projects.enabled of type bool"
	parts := strings.Split(errMsg, "cannot unmarshal ")
	if len(parts) < 2 {
		return errMsg
	}

	remainder := parts[1]
	wordParts := strings.SplitN(remainder, " ", 2)
	if len(wordParts) < 1 {
		return errMsg
	}

	foundType := wordParts[0]

	// Find the target type
	if _, targetType, found := strings.Cut(remainder, " of type "); found {
		return fmt.Sprintf("found %s, expected %s", foundType, targetType)
	}

	return fmt.Sprintf("found %s", foundType)
}

// LoadJSON5Bytes parses JSON5 config content (already env-expanded) into
// v, applying the same duration preprocessing and JSON5 standardization
// as LoadJSON5. It exists so Load can try the JSON5 grammar first (the
// shipped template format) and fall back to legacy TOML without re-reading
// the file.
//
// Unlike LoadJSON5, this path also quotes JSON5 bare (unquoted) object
// keys before standardizing: JSON5 proper allows them, hujson does not.
// Bare-key tolerance lives here and not in LoadJSON5 so existing callers
// keep their exact strictness; meept.json5 itself quotes every key.
func LoadJSON5Bytes(data []byte, v any) error {
	content := preprocessDurations(string(data))
	stdJSON, err := hujson.Standardize([]byte(quoteBareKeys(content)))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}
	stdJSON, err = promoteTopLevelGroups(stdJSON)
	if err != nil {
		return fmt.Errorf("failed to normalize groups section: %w", err)
	}
	if err := json.Unmarshal(stdJSON, v); err != nil {
		return fmt.Errorf("failed to unmarshal JSON5: %w", err)
	}
	return nil
}

// promoteTopLevelGroups rewrites a top-level "groups" key into
// "security"."groups" (agent-groups leaf 03-mutation-gate): the groups config
// section is the [security.groups] schema home, but the e2e overlays and
// operator configs commonly carry it TOP-LEVEL (the harness's dotted
// "groups" path lands there). When the parsed document carries a top-level
// "groups" object it is MOVED under "security" — an existing
// security.groups section wins (explicit nesting is authoritative; the
// top-level alias only fills an absent section). A document without a
// top-level "groups" key is returned byte-identical (fast path: one
// contains check, zero re-marshal). The rewrite happens AFTER hujson
// standardization, so the input here is strict JSON.
func promoteTopLevelGroups(stdJSON []byte) ([]byte, error) {
	if !bytes.Contains(stdJSON, []byte(`"groups"`)) {
		return stdJSON, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(stdJSON, &root); err != nil {
		// Not a top-level object (or malformed post-standardize): hand the
		// original back and let the caller's Unmarshal produce the real
		// error.
		return stdJSON, nil
	}
	raw, ok := root["groups"]
	if !ok {
		return stdJSON, nil
	}
	var section map[string]any
	if err := json.Unmarshal(raw, &section); err != nil {
		return nil, fmt.Errorf("top-level groups: %w", err)
	}
	secRaw, hasSec := root["security"]
	var sec map[string]any
	if hasSec {
		if err := json.Unmarshal(secRaw, &sec); err != nil || sec == nil {
			sec = map[string]any{}
		}
	} else {
		sec = map[string]any{}
	}
	if _, exists := sec["groups"]; !exists {
		sec["groups"] = section
	}
	secBytes, err := json.Marshal(sec)
	if err != nil {
		return nil, fmt.Errorf("re-encode security section: %w", err)
	}
	root["security"] = secBytes
	delete(root, "groups")
	out, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("re-encode config: %w", err)
	}
	return out, nil
}

// quoteBareKeys walks JSON5 text and wraps unquoted identifier keys in
// double quotes so hujson (comments + trailing commas only) accepts full
// JSON5 input. String literals, comments, and non-key identifier
// positions (values, true/false/null) pass through: a bare token is only
// quoted when it is followed (after whitespace) by ':' AND the previous
// non-space byte is '{' or ',' - i.e. a member-name position.
func quoteBareKeys(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	i := 0
	for i < len(content) {
		c := content[i]
		switch {
		case c == '"':
			// String literal: copy verbatim (handles escapes).
			start := i
			i++
			for i < len(content) && content[i] != '"' {
				if content[i] == '\\' {
					i++
				}
				if i < len(content) {
					i++
				}
			}
			if i < len(content) {
				i++ // closing quote
			}
			b.WriteString(content[start:i])
		case c == '/' && i+1 < len(content) && content[i+1] == '/':
			cstart := i
			for i < len(content) && content[i] != '\n' {
				i++
			}
			b.WriteString(content[cstart:i])
		case c == '/' && i+1 < len(content) && content[i+1] == '*':
			cstart := i
			i += 2
			for i+1 < len(content) && (content[i] != '*' || content[i+1] != '/') {
				i++
			}
			if i+1 < len(content) {
				i += 2
			} else {
				i = len(content)
			}
			b.WriteString(content[cstart:i])
		case isIdentStart(c):
			start := i
			for i < len(content) && isIdentPart(content[i]) {
				i++
			}
			word := content[start:i]
			// Member-name position: prev non-space is '{' or ',' and the
			// next non-space is ':'. Anything else (true/false/null and
			// future reserved words in value position) passes through.
			if nextNonSpace(content, i) == ':' {
				switch prevNonSpace(content, start) {
				case '{', ',':
					b.WriteByte('"')
					b.WriteString(word)
					b.WriteByte('"')
					continue
				}
			}
			b.WriteString(word)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// nextNonSpace returns the next non-whitespace byte at or after i, or 0.
func nextNonSpace(data string, i int) byte {
	for i < len(data) {
		switch data[i] {
		case ' ', '	', '\r', '\n':
			i++
		default:
			return data[i]
		}
	}
	return 0
}

// UnmarshalJSON5 parses JSON5-formatted bytes into a struct.
// Unlike LoadJSON5, this does NOT expand environment variables.
// It also handles Go duration string values (e.g. "30s") in JSON.
func UnmarshalJSON5(data []byte, v any) error {
	content := preprocessDurations(string(data))
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}
	return json.Unmarshal(stdJSON, v)
}

func parseDuration(s string) (int64, error) {
	// Handle the non-standard "d" (day) suffix by converting to hours.
	if strings.HasSuffix(s, "d") {
		numStr := s[:len(s)-1]
		f, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration: %q", s)
		}
		return int64(f * 24 * float64(time.Hour.Nanoseconds())), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %q: %w", s, err)
	}
	return int64(d), nil
}
