package sqlite

import (
	"testing"
)

func TestFTS5QueryBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single term",
			input:    "hello",
			expected: `"hello"`,
		},
		{
			name:     "multiple terms",
			input:    "hello world",
			expected: `"hello" AND "world"`,
		},
		{
			name:     "special characters removed",
			input:    `hello "world" test`,
			expected: `"hello" AND "world" AND "test"`,
		},
		{
			name:     "empty query",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    `"" * :`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewFTS5Query().TermsFromString(tt.input).Build()
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFTS5QueryOr(t *testing.T) {
	query := NewFTS5Query().
		Term("hello").
		Term("world").
		Or().
		Build()

	expected := `"hello" OR "world"`
	if query != expected {
		t.Errorf("Expected %q, got %q", expected, query)
	}
}

func TestFTS5QueryColumns(t *testing.T) {
	query := NewFTS5Query().
		Column("content").
		Term("test").
		Build()

	expected := `{content}:"test"`
	if query != expected {
		t.Errorf("Expected %q, got %q", expected, query)
	}

	query = NewFTS5Query().
		Columns("content", "title").
		Term("test").
		Build()

	expected = `{content title}:"test"`
	if query != expected {
		t.Errorf("Expected %q, got %q", expected, query)
	}
}

func TestFTS5QueryPhrase(t *testing.T) {
	query := NewFTS5Query().
		Term("hello").
		Term("world").
		BuildPhrase()

	expected := `"hello world"`
	if query != expected {
		t.Errorf("Expected %q, got %q", expected, query)
	}
}

func TestFTS5QueryPrefix(t *testing.T) {
	query := NewFTS5Query().
		Term("hello").
		Term("world").
		BuildPrefix()

	expected := `"hello"* AND "world"*`
	if query != expected {
		t.Errorf("Expected %q, got %q", expected, query)
	}
}

func TestSanitizeQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", `"hello" AND "world"`},
		{"test", `"test"`},
		{"", ""},
		{`"quotes" and *stars*`, `"quotes" AND "and" AND "stars"`},
	}

	for _, tt := range tests {
		result := SanitizeQuery(tt.input)
		if result != tt.expected {
			t.Errorf("SanitizeQuery(%q): expected %q, got %q", tt.input, tt.expected, result)
		}
	}
}

func TestSanitizeTerm(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{`"hello"`, "hello"},
		{"test*", "test"},
		{"foo:bar", "foobar"},
		{"(test)", "test"},
		{"  spaces  ", "spaces"},
		{"", ""},
	}

	for _, tt := range tests {
		result := sanitizeTerm(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeTerm(%q): expected %q, got %q", tt.input, tt.expected, result)
		}
	}
}

func TestHighlight(t *testing.T) {
	result := Highlight("my_fts", 0, "<b>", "</b>")
	expected := "highlight(my_fts, 0, '<b>', '</b>')"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestSnippet(t *testing.T) {
	result := Snippet("my_fts", 0, "<b>", "</b>", "...", 64)
	expected := "snippet(my_fts, 0, '<b>', '</b>', '...', 64)"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestBM25(t *testing.T) {
	// Without weights
	result := BM25("my_fts")
	expected := "bm25(my_fts)"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// With weights
	result = BM25("my_fts", 1.0, 2.0, 3.0)
	expected = "bm25(my_fts, 1.00, 2.00, 3.00)"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestRank(t *testing.T) {
	if Rank() != "rank" {
		t.Error("Rank() should return 'rank'")
	}
}

func TestNormalizeRank(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{0.0, 0.0},
		{1.0, 0.0},     // Positive ranks should return 0
		{-1.0, 0.5},    // 1 / (1 + 1) = 0.5
		{-4.0, 0.2},    // 1 / (1 + 4) = 0.2
		{-9.0, 0.1},    // 1 / (1 + 9) = 0.1
		{-0.5, 1.0/1.5}, // 1 / (1 + 0.5) = 0.666...
	}

	for _, tt := range tests {
		result := NormalizeRank(tt.input)
		// Use approximate comparison for floating point
		diff := result - tt.expected
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.001 {
			t.Errorf("NormalizeRank(%f): expected %f, got %f", tt.input, tt.expected, result)
		}
	}
}

func TestCreateFTS5Table(t *testing.T) {
	// Standalone FTS table
	result := CreateFTS5Table("my_fts", "", "content", "title", "author")
	expected := "CREATE VIRTUAL TABLE IF NOT EXISTS my_fts USING fts5(content, title, author)"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}

	// Content table backed FTS
	result = CreateFTS5Table("my_fts", "my_content", "content", "title")
	expected = "CREATE VIRTUAL TABLE IF NOT EXISTS my_fts USING fts5(content, title, content='my_content', content_rowid='rowid')"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestCreateFTS5Triggers(t *testing.T) {
	triggers := CreateFTS5Triggers("my_fts", "my_content", "content", "title")

	if len(triggers) != 3 {
		t.Fatalf("Expected 3 triggers, got %d", len(triggers))
	}

	// Check that triggers contain expected keywords
	insertTrigger := triggers[0]
	if !contains(insertTrigger, "AFTER INSERT") {
		t.Error("Insert trigger missing 'AFTER INSERT'")
	}

	deleteTrigger := triggers[1]
	if !contains(deleteTrigger, "AFTER DELETE") {
		t.Error("Delete trigger missing 'AFTER DELETE'")
	}

	updateTrigger := triggers[2]
	if !contains(updateTrigger, "AFTER UPDATE") {
		t.Error("Update trigger missing 'AFTER UPDATE'")
	}
}

func TestFTS5QueryChaining(t *testing.T) {
	// Test method chaining
	query := NewFTS5Query().
		Term("foo").
		Term("bar").
		Terms("baz", "qux").
		And().
		Build()

	expected := `"foo" AND "bar" AND "baz" AND "qux"`
	if query != expected {
		t.Errorf("Expected %q, got %q", expected, query)
	}
}

func TestFTS5QueryEmptyTerms(t *testing.T) {
	// Empty terms should be filtered
	query := NewFTS5Query().
		Term("").
		Term("hello").
		Term("").
		Build()

	expected := `"hello"`
	if query != expected {
		t.Errorf("Expected %q, got %q", expected, query)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
