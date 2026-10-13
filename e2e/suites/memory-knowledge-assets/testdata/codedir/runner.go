package alpha

import "fmt"

// Runner drives parse runs and reports results.
type Runner struct {
	parser Parser
}

// NewRunner constructs a Runner over a fresh Parser.
func NewRunner() *Runner {
	return &Runner{parser: Parser{}}
}

// Run parses the input and prints the token count. Calls Parser.Parse —
// the fixture's pinned call edge (local-var method resolution).
func (r *Runner) Run(input string) string {
	p := Parser{}
	tokens := p.Parse(input)
	return fmt.Sprintf("tokens: %d", len(tokens))
}

// RunAll runs every input through ParseAll (second pinned edge,
// package-function resolution).
func (r *Runner) RunAll(inputs []string) string {
	tokens := ParseAll(inputs)
	return fmt.Sprintf("total: %d", len(tokens))
}
