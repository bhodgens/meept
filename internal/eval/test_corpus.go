package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// TestCase represents a single classification test case.
type TestCase struct {
	Input          string `json:"input"`
	ExpectedIntent string `json:"expected_intent"`
	ExpectedAgent  string `json:"expected_agent"`
	Description    string `json:"description,omitempty"`
}

// TestCorpus represents the full test dataset.
type TestCorpus struct {
	Name       string                `json:"name"`
	Version    string                `json:"version"`
	Categories map[string][]TestCase `json:"categories"`
}

// LoadTestCorpus reads the test corpus from the testdata directory.
func LoadTestCorpus() (*TestCorpus, error) {
	path := filepath.Join("testdata", "eval", "classifier-test-corpus.json5")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read test corpus: %w", err)
	}

	// Convert JSON5 (unquoted keys, // comments, trailing commas) to standard JSON
	jsonBytes, err := json5ToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert JSON5: %w", err)
	}

	var corpus TestCorpus
	if err := json.Unmarshal(jsonBytes, &corpus); err != nil {
		return nil, fmt.Errorf("failed to parse test corpus: %w", err)
	}

	return &corpus, nil
}

var jsonKeywords = map[string]bool{
	"true": true, "false": true, "null": true,
}

// stripComments removes // line comments that are not inside string literals.
func stripComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var result []string
	for _, line := range lines {
		var sb strings.Builder
		inStr := false
		escaped := false
		for i := 0; i < len(line); i++ {
			if escaped {
				sb.WriteByte(line[i])
				escaped = false
				continue
			}
			if line[i] == '\\' {
				sb.WriteByte(line[i])
				escaped = true
				continue
			}
			if line[i] == '"' {
				inStr = !inStr
				sb.WriteByte(line[i])
				continue
			}
			if !inStr && i+1 < len(line) && line[i] == '/' && line[i+1] == '/' {
				break
			}
			sb.WriteByte(line[i])
		}
		result = append(result, sb.String())
	}
	return []byte(strings.Join(result, "\n"))
}

// stripTrailingCommas removes trailing commas before ] or } that are not inside strings.
func stripTrailingCommas(data []byte) []byte {
	var result []byte
	inStr := false
	escaped := false
	for i := 0; i < len(data); i++ {
		ch := data[i]

		if escaped {
			result = append(result, ch)
			escaped = false
			continue
		}
		if ch == '\\' && inStr {
			result = append(result, ch)
			escaped = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			result = append(result, ch)
			continue
		}
		if inStr {
			result = append(result, ch)
			continue
		}

		if ch == ',' {
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\n' || data[j] == '\r') {
				j++
			}
			if j < len(data) && (data[j] == ']' || data[j] == '}') {
				continue // skip trailing comma
			}
		}
		result = append(result, ch)
	}
	return result
}

// quoteUnquotedKeys converts unquoted JSON5 object keys to quoted keys.
func quoteUnquotedKeys(data []byte) ([]byte, error) {
	var result []byte
	i := 0
	buf := string(data)
	prevNonWS := byte(0)

	for i < len(buf) {
		ch := buf[i]

		// String literal
		if ch == '"' {
			result = append(result, ch)
			i++
			for i < len(buf) {
				c := buf[i]
				if c == '\\' {
					result = append(result, c)
					i++
					if i < len(buf) {
						result = append(result, buf[i])
						i++
					}
					continue
				}
				if c == '"' {
					result = append(result, c)
					i++
					break
				}
				result = append(result, c)
				i++
			}
			prevNonWS = '"'
			continue
		}

		// Raw token (identifier, keyword, number, bool, null)
		if unicode.IsLetter(rune(ch)) || unicode.IsDigit(rune(ch)) || ch == '_' || ch == '$' || ch == '-' {
			start := i
			tokLen := 0
			for i < len(buf) && (unicode.IsLetter(rune(buf[i])) || unicode.IsDigit(rune(buf[i])) || buf[i] == '_' || buf[i] == '$' || buf[i] == '-') {
				i++
				tokLen++
			}
			tok := buf[start : start+tokLen]

			wsStart := i
			for i < len(buf) && unicode.IsSpace(rune(buf[i])) {
				i++
			}
			whitespace := buf[wsStart:i]

			if i < len(buf) && buf[i] == ':' {
				// This token is followed by a colon
				_, isJSONKeyword := jsonKeywords[tok]
				if !isJSONKeyword && (prevNonWS == '[' || prevNonWS == '{' || prevNonWS == ',') {
					// Unquoted JSON key — quote it
					result = append(result, '"')
					result = append(result, tok...)
					result = append(result, '"')
				} else {
					result = append(result, tok...)
				}
				result = append(result, whitespace...)
				result = append(result, ':')
				i++ // skip over ':'
				prevNonWS = ':'
			} else {
				result = append(result, tok...)
				result = append(result, whitespace...)
				if len(whitespace) > 0 {
					prevNonWS = tok[tokLen-1]
				}
			}
			continue
		}

		result = append(result, ch)
		if !unicode.IsSpace(rune(ch)) {
			prevNonWS = ch
		}
		i++
	}

	return result, nil
}

func json5ToJSON(data []byte) ([]byte, error) {
	cleaned := stripComments(data)
	cleaned = stripTrailingCommas(cleaned)
	return quoteUnquotedKeys(cleaned)
}

// AllTestCases returns all test cases flattened across categories in stable order.
func (c *TestCorpus) AllTestCases() []TestCase {
	var all []TestCase
	// Sort category names for deterministic order.
	names := sortedKeys(c.Categories)
	for _, name := range names {
		all = append(all, c.Categories[name]...)
	}
	return all
}

// CategoryNames returns the list of intent categories in the corpus in deterministic order.
func (c *TestCorpus) CategoryNames() []string {
	return sortedKeys(c.Categories)
}

// CategoryCount returns the number of test cases in a category.
func (c *TestCorpus) CategoryCount(category string) int {
	return len(c.Categories[category])
}

// TotalCount returns the total number of test cases.
func (c *TestCorpus) TotalCount() int {
	total := 0
	for _, cases := range c.Categories {
		total += len(cases)
	}
	return total
}

// TestCaseByCategory returns a map of intent -> list of test cases with that intent.
func (c *TestCorpus) TestCaseByCategory() map[string][]TestCase {
	result := make(map[string][]TestCase)
	for _, cases := range c.Categories {
		for _, tc := range cases {
			result[tc.ExpectedIntent] = append(result[tc.ExpectedIntent], tc)
		}
	}
	return result
}

// sortedKeys returns the keys of m in sorted order.
func sortedKeys(m map[string][]TestCase) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
