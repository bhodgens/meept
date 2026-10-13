// Package alpha is the e2e fixture "module" for the memory-knowledge-assets
// suite: two files with call shapes the code index resolves (package-level
// function calls and local-var method calls via composite-literal
// assignment — the indexer's documented v1 heuristics). Known edges:
// Runner.Run -> Parser.Parse, ParseAll -> ParseOne. NOT a real Go module —
// IndexGoDirs walks directories with go/parser and needs no go.mod.
package alpha

import "strings"

// Parser splits raw input into tokens.
type Parser struct{}

// Parse tokenizes the input on whitespace.
func (p *Parser) Parse(input string) []string {
	return strings.Fields(input)
}

// ParseAll parses every input, concatenating tokens.
func ParseAll(inputs []string) []string {
	var out []string
	for _, in := range inputs {
		out = append(out, ParseOne(in)...)
	}
	return out
}

// ParseOne parses a single input.
func ParseOne(input string) []string {
	p := Parser{}
	return p.Parse(input)
}
