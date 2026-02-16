package sqlite

import (
	"fmt"
	"strings"
	"unicode"
)

// FTS5Query helps build FTS5 full-text search queries.
type FTS5Query struct {
	terms    []string
	columns  []string
	operator string
}

// NewFTS5Query creates a new FTS5 query builder.
func NewFTS5Query() *FTS5Query {
	return &FTS5Query{
		operator: "AND",
	}
}

// Term adds a search term to the query.
func (q *FTS5Query) Term(term string) *FTS5Query {
	if sanitized := sanitizeTerm(term); sanitized != "" {
		q.terms = append(q.terms, sanitized)
	}
	return q
}

// Terms adds multiple search terms to the query.
func (q *FTS5Query) Terms(terms ...string) *FTS5Query {
	for _, t := range terms {
		q.Term(t)
	}
	return q
}

// TermsFromString splits a string into terms and adds them.
func (q *FTS5Query) TermsFromString(input string) *FTS5Query {
	for _, t := range strings.Fields(input) {
		q.Term(t)
	}
	return q
}

// Column restricts search to specific columns.
func (q *FTS5Query) Column(col string) *FTS5Query {
	q.columns = append(q.columns, col)
	return q
}

// Columns restricts search to multiple columns.
func (q *FTS5Query) Columns(cols ...string) *FTS5Query {
	q.columns = append(q.columns, cols...)
	return q
}

// And sets the operator to AND (default).
func (q *FTS5Query) And() *FTS5Query {
	q.operator = "AND"
	return q
}

// Or sets the operator to OR.
func (q *FTS5Query) Or() *FTS5Query {
	q.operator = "OR"
	return q
}

// Build constructs the FTS5 MATCH expression.
// Returns an empty string if there are no valid terms.
func (q *FTS5Query) Build() string {
	if len(q.terms) == 0 {
		return ""
	}

	// Quote each term for safety
	quotedTerms := make([]string, len(q.terms))
	for i, t := range q.terms {
		quotedTerms[i] = fmt.Sprintf(`"%s"`, t)
	}

	query := strings.Join(quotedTerms, " "+q.operator+" ")

	// Add column filter if specified
	if len(q.columns) > 0 {
		colFilter := "{" + strings.Join(q.columns, " ") + "}"
		query = colFilter + ":" + query
	}

	return query
}

// BuildPhrase constructs a phrase query (exact sequence match).
func (q *FTS5Query) BuildPhrase() string {
	if len(q.terms) == 0 {
		return ""
	}

	// Join terms with spaces inside quotes for phrase matching
	phrase := strings.Join(q.terms, " ")
	return fmt.Sprintf(`"%s"`, phrase)
}

// BuildPrefix constructs a prefix query (matches terms starting with the given prefixes).
func (q *FTS5Query) BuildPrefix() string {
	if len(q.terms) == 0 {
		return ""
	}

	// Add * to each term for prefix matching
	prefixTerms := make([]string, len(q.terms))
	for i, t := range q.terms {
		prefixTerms[i] = fmt.Sprintf(`"%s"*`, t)
	}

	return strings.Join(prefixTerms, " "+q.operator+" ")
}

// sanitizeTerm removes characters that are invalid in FTS5 queries.
func sanitizeTerm(term string) string {
	// Remove quotes and FTS5 special characters
	var sb strings.Builder
	for _, r := range term {
		if r == '"' || r == '\'' || r == '*' || r == ':' || r == '(' || r == ')' {
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		sb.WriteRune(r)
	}
	return strings.TrimSpace(sb.String())
}

// SanitizeQuery converts a raw user query into a safe FTS5 MATCH expression.
// Each whitespace-separated token is quoted for safety.
func SanitizeQuery(raw string) string {
	return NewFTS5Query().TermsFromString(raw).Build()
}

// Highlight returns SQL for FTS5 highlight function.
// tableName: the FTS5 table name
// column: column index (0-based) or -1 for all columns
// openTag: HTML/text to insert before match
// closeTag: HTML/text to insert after match
func Highlight(tableName string, column int, openTag, closeTag string) string {
	return fmt.Sprintf("highlight(%s, %d, '%s', '%s')",
		tableName, column, openTag, closeTag)
}

// Snippet returns SQL for FTS5 snippet function.
// tableName: the FTS5 table name
// column: column index (0-based)
// openTag: HTML/text to insert before match
// closeTag: HTML/text to insert after match
// ellipsis: text to show when content is truncated
// maxTokens: maximum tokens in snippet
func Snippet(tableName string, column int, openTag, closeTag, ellipsis string, maxTokens int) string {
	return fmt.Sprintf("snippet(%s, %d, '%s', '%s', '%s', %d)",
		tableName, column, openTag, closeTag, ellipsis, maxTokens)
}

// BM25 returns SQL for FTS5 BM25 ranking function.
// tableName: the FTS5 table name
// weights: optional column weights (higher = more important)
func BM25(tableName string, weights ...float64) string {
	if len(weights) == 0 {
		return fmt.Sprintf("bm25(%s)", tableName)
	}

	weightStrs := make([]string, len(weights))
	for i, w := range weights {
		weightStrs[i] = fmt.Sprintf("%.2f", w)
	}

	return fmt.Sprintf("bm25(%s, %s)", tableName, strings.Join(weightStrs, ", "))
}

// Rank returns the built-in rank column for FTS5.
// This is equivalent to bm25() but faster for simple cases.
func Rank() string {
	return "rank"
}

// NormalizeRank converts an FTS5 rank value to a [0.0, 1.0] score.
// FTS5 rank values are negative (more negative = better match).
func NormalizeRank(rank float64) float64 {
	if rank >= 0 {
		return 0.0
	}
	return 1.0 / (1.0 + abs(rank))
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// CreateFTS5Table returns SQL to create an FTS5 virtual table.
// contentTable: optional backing content table (empty for standalone)
// columns: column names to index
func CreateFTS5Table(tableName string, contentTable string, columns ...string) string {
	var sb strings.Builder
	sb.WriteString("CREATE VIRTUAL TABLE IF NOT EXISTS ")
	sb.WriteString(tableName)
	sb.WriteString(" USING fts5(")
	sb.WriteString(strings.Join(columns, ", "))

	if contentTable != "" {
		sb.WriteString(fmt.Sprintf(", content='%s', content_rowid='rowid'", contentTable))
	}

	sb.WriteString(")")
	return sb.String()
}

// CreateFTS5Trigger returns SQL for insert/update/delete triggers to keep
// an FTS5 table in sync with its content table.
func CreateFTS5Triggers(ftsTable, contentTable string, columns ...string) []string {
	colList := strings.Join(columns, ", ")
	newCols := "new." + strings.Join(columns, ", new.")
	oldCols := "old." + strings.Join(columns, ", old.")

	return []string{
		// Insert trigger
		fmt.Sprintf(`
CREATE TRIGGER IF NOT EXISTS %s_ai AFTER INSERT ON %s BEGIN
    INSERT INTO %s(rowid, %s) VALUES (new.rowid, %s);
END`, ftsTable, contentTable, ftsTable, colList, newCols),

		// Delete trigger
		fmt.Sprintf(`
CREATE TRIGGER IF NOT EXISTS %s_ad AFTER DELETE ON %s BEGIN
    INSERT INTO %s(%s, rowid, %s) VALUES ('delete', old.rowid, %s);
END`, ftsTable, contentTable, ftsTable, ftsTable, colList, oldCols),

		// Update trigger
		fmt.Sprintf(`
CREATE TRIGGER IF NOT EXISTS %s_au AFTER UPDATE ON %s BEGIN
    INSERT INTO %s(%s, rowid, %s) VALUES ('delete', old.rowid, %s);
    INSERT INTO %s(rowid, %s) VALUES (new.rowid, %s);
END`, ftsTable, contentTable, ftsTable, ftsTable, colList, oldCols, ftsTable, colList, newCols),
	}
}
