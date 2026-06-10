package types

// LinterResult represents the output of a linting run
type LinterResult struct {
	File      string
	Line      int // 0-based line number
	Column    int // 0-based column (optional)
	EndLine   int // 0-based end line (optional)
	EndColumn int // 0-based end column (optional)
	Message   string
	Severity  string // "error" | "warning" | "info"
	Rule      string // Lint rule identifier
}

// HasErrors returns true if any error-severity issues found
func (r LinterResult) HasErrors() bool {
	return r.Severity == "error"
}
