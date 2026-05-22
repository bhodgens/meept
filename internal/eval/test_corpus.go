package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Categories map[string][]TestCase `json:"categories"`
}

// LoadTestCorpus reads the test corpus from the testdata directory.
func LoadTestCorpus() (*TestCorpus, error) {
	path := filepath.Join("testdata", "eval", "classifier-test-corpus.json5")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read test corpus: %w", err)
	}

	// Parse JSON5 (JSON with comments)
	// Strip // line comments for basic JSON5 parsing.
	// For production use, consider a proper JSON5 library.
	jsonData := stripComments(data)

	var corpus TestCorpus
	if err := json.Unmarshal(jsonData, &corpus); err != nil {
		return nil, fmt.Errorf("failed to parse test corpus: %w", err)
	}

	return &corpus, nil
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

// stripComments removes // comments from JSON5 for basic parsing.
func stripComments(data []byte) []byte {
	lines := string(data)
	var result strings.Builder
	for _, line := range strings.Split(lines, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		result.WriteString(line)
		result.WriteString("\n")
	}
	return []byte(result.String())
}
