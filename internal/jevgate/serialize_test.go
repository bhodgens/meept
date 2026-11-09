package jevgate

// JEV-2 (docs/plans/20261108-plan-audit/findings.md): automated pin that
// SerializeCommandContext's render matches the FROZEN serialization_template
// in config/jev-command-gate.contract.json5 byte-for-byte. The contract file
// is the training-manifest-hash authority, so the test reads it directly
// (never a copy) and fails loudly if either side drifts.

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// contractTemplatePath is the frozen contract file, resolved from this
// package directory (internal/jevgate -> repo root -> config/).
const contractTemplatePath = "../../config/jev-command-gate.contract.json5"

// contractDoc is the subset of the contract file this test consumes.
type contractDoc struct {
	SerializationTemplate struct {
		Lines    []string `json:"lines"`
		FactLine string   `json:"fact_line"`
	} `json:"serialization_template"`
}

// stripJSON5Comments removes // line comments outside string literals
// (the contract's only JSON5 extension in practice) plus trailing commas,
// so encoding/json can parse it. String-aware: a "//" inside a JSON string
// is preserved.
func stripJSON5Comments(t *testing.T, text string) string {
	t.Helper()
	var b strings.Builder
	inString := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			b.WriteByte(ch)
			if ch == '\\' && i+1 < len(text) {
				i++
				b.WriteByte(text[i])
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch {
		case ch == '"':
			inString = true
			b.WriteByte(ch)
		case ch == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
			i-- // the loop's ++ restores position on the newline
		default:
			b.WriteByte(ch)
		}
	}
	cleaned := b.String()
	// Tolerate trailing commas (defensive; the contract does not use them).
	cleaned = strings.ReplaceAll(cleaned, ",\n}", "\n}")
	cleaned = strings.ReplaceAll(cleaned, ",\n]", "\n]")
	return cleaned
}

// loadContractTemplate parses the frozen contract file and returns the
// serialization_template block.
func loadContractTemplate(t *testing.T) (lines []string, factLine string) {
	t.Helper()
	data, err := os.ReadFile(contractTemplatePath)
	if err != nil {
		t.Fatalf("read frozen contract %s: %v", contractTemplatePath, err)
	}
	var doc contractDoc
	if err := json.Unmarshal([]byte(stripJSON5Comments(t, string(data))), &doc); err != nil {
		t.Fatalf("parse contract %s: %v", contractTemplatePath, err)
	}
	tmpl := doc.SerializationTemplate
	// The template shape is FROZEN (Contract C2/C6): three substituted
	// header lines plus the facts block line, and the fact-line shape.
	// A structural drift must fail here, not silently render wrong bytes.
	if len(tmpl.Lines) != 4 ||
		tmpl.Lines[0] != "command: {command_oneline}" ||
		tmpl.Lines[1] != "cwd: {cwd_oneline}" ||
		tmpl.Lines[2] != "facts:" ||
		tmpl.Lines[3] != "{fact_lines}" {
		t.Fatalf("frozen template lines drifted: %q", tmpl.Lines)
	}
	if tmpl.FactLine != "  {key}: {value_oneline}" {
		t.Fatalf("frozen fact_line drifted: %q", tmpl.FactLine)
	}
	return tmpl.Lines, tmpl.FactLine
}

// renderContractTemplate renders the contract template for a command
// context, following the frozen line-anchored convention: every rendered
// line (including each fact line) is newline-terminated, so an empty facts
// block leaves the bare "facts:" line with no body. This mirrors
// SerializeCommandContext's byte shape; the comparison below is the pin.
func renderContractTemplate(t *testing.T, lines []string, factLine, command, cwd string, facts map[string]string) string {
	t.Helper()
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys) // rule: fact keys sorted byte-wise before rendering

	var b strings.Builder
	for _, line := range lines {
		if line == "{fact_lines}" {
			for _, k := range keys {
				fl := strings.ReplaceAll(factLine, "{key}", k)
				fl = strings.ReplaceAll(fl, "{value_oneline}", Oneline(facts[k]))
				b.WriteString(fl)
				b.WriteString("\n")
			}
			continue
		}
		l := strings.ReplaceAll(line, "{command_oneline}", Oneline(command))
		l = strings.ReplaceAll(l, "{cwd_oneline}", Oneline(cwd))
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

// TestSerializeCommandContext_MatchesContractTemplate pins JEV-2: the
// serializer's bytes equal the frozen contract template's render for a
// known command context, byte-for-byte — including Oneline newline
// escaping, sorted fact keys, and the empty-facts shape.
func TestSerializeCommandContext_MatchesContractTemplate(t *testing.T) {
	lines, factLine := loadContractTemplate(t)

	t.Run("populated facts with escapes", func(t *testing.T) {
		command := "echo one\necho two" // embedded newline must render escaped
		cwd := "/Users/caimlas/git/meept"
		facts := map[string]string{
			"session":  "s-123",
			"agent":    "coder",
			"risk":     "medium",
			"protocol": "ssh",
			"note":     "line1\nline2", // fact values escape too
		}
		want := renderContractTemplate(t, lines, factLine, command, cwd, facts)
		got := SerializeCommandContext(command, cwd, facts)
		if got != want {
			t.Fatalf("serializer output diverged from the frozen contract template:\nserializer: %q\ncontract:  %q", got, want)
		}
		// Sanity: the escaped forms and sorted order are actually exercised.
		if !strings.Contains(got, `echo one\necho two`) || !strings.Contains(got, `line1\nline2`) {
			t.Fatalf("escape paths not exercised in render: %q", got)
		}
		if strings.Index(got, "agent: coder") > strings.Index(got, "session: s-123") {
			t.Fatalf("fact keys not in sorted order: %q", got)
		}
	})

	t.Run("empty facts block", func(t *testing.T) {
		command := "ls -la"
		cwd := "/tmp"
		want := renderContractTemplate(t, lines, factLine, command, cwd, nil)
		got := SerializeCommandContext(command, cwd, nil)
		if got != want {
			t.Fatalf("empty-facts render diverged:\nserializer: %q\ncontract:  %q", got, want)
		}
		if !strings.HasSuffix(got, "facts:\n") || strings.Contains(got, "facts:\n\n") {
			t.Fatalf("empty facts must render the bare 'facts:' line: %q", got)
		}
	})
}
