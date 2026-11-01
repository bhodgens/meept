package jevgate

import (
	"fmt"
	"sort"
	"strings"
)

// SerializeCommandContext renders the byte-stable command context (Contract
// C2, template shape from master.md C6): fixed key order, sorted fact keys,
// no map-iteration-order leakage. Identical inputs produce identical bytes,
// always -- training manifest hashes depend on it.
//
// TODO(jev-gate leaf 02/03): the authoritative template freezes into
// config/jev-command-gate.contract.json5 (C6); keep this render
// byte-identical with that file once it lands.
func SerializeCommandContext(command, cwd string, facts map[string]string) string {
	var b strings.Builder
	b.WriteString("command: ")
	b.WriteString(Oneline(command))
	b.WriteString("\ncwd: ")
	b.WriteString(Oneline(cwd))
	b.WriteString("\nfacts:\n")

	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("  ")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(Oneline(facts[k]))
		b.WriteString("\n")
	}
	return b.String()
}

// Oneline flattens a value to one line: newlines become the literal "\n"
// escape so every field stays a single record line and the template's
// line-anchored structure is spoof-resistant (a command containing
// "cwd: /" cannot forge an extra field).
func Oneline(s string) string {
	if !strings.ContainsAny(s, "\n\r") {
		return s
	}
	r := strings.NewReplacer("\r\n", "\\n", "\n", "\\n", "\r", "\\n")
	return r.Replace(s)
}

// ScoreGateConfig is the in-package gate configuration (jev-command-gate
// leaf 01). The JSON5 config schema is CLOSED this wave: the schema keys are
// jev_command_gate.enabled (default false, opt-in),
// jev_command_gate.score_threshold (default 0.90),
// jev_command_gate.confidence_threshold (default 0.10); the daemon wiring +
// config schema.go struct land next wave. The in-package defaults exist so
// the gate is correct standalone and the wiring wave only binds values.
type ScoreGateConfig struct {
	// Enabled is the opt-in master switch. Default false: zero behavior
	// change until an operator turns it on.
	Enabled bool `json5:"enabled"`
	// ScoreThreshold is the minimum rubric-anchored score that allows a
	// shell command. Default 0.90 -- the conservative end of the
	// research-recommended 0.85-0.90 band
	// (docs/research/jev-command-gate-calibration.md section 1.3); leaf
	// 03's teacher sweep freezes the exact value later.
	ScoreThreshold float64 `json5:"score_threshold"`
	// ConfidenceFloor is the minimum self-reported confidence that
	// allows a shell command. Deliberately LOW (0.10): confidence is a
	// VETO signal -- below it forces ASK -- never an allow signal above
	// it (research section 4, C3 amendment). The real allow bars are the
	// parsed score ANSWER and the score itself.
	ConfidenceFloor float64 `json5:"confidence_threshold"`
}

// DefaultScoreGateConfig returns the conservative defaults: disabled, score
// threshold 0.90, confidence floor 0.10.
func DefaultScoreGateConfig() ScoreGateConfig {
	return ScoreGateConfig{
		Enabled:         false,
		ScoreThreshold:  0.90,
		ConfidenceFloor: 0.10,
	}
}

// commandScoreAllowThreshold and commandScoreConfidenceFloor are the
// built-in fallbacks when a zero-valued ScoreGateConfig reaches the gate
// (a caller that forgot the defaults must not produce a threshold of 0.0,
// which would allow everything).
const (
	commandScoreAllowThreshold = 0.90
	commandScoreConfFloor      = 0.10
)

// effectiveThreshold returns c's score threshold, or the built-in
// conservative default when unset.
func (c ScoreGateConfig) effectiveThreshold() float64 {
	if c.ScoreThreshold <= 0 || c.ScoreThreshold > 1 {
		return commandScoreAllowThreshold
	}
	return c.ScoreThreshold
}

// effectiveConfFloor returns c's confidence floor, or the built-in low
// default when unset (negative floors clamp to 0: confidence can then never
// veto, which is a deliberate operator choice, not a forgotten default).
func (c ScoreGateConfig) effectiveConfFloor() float64 {
	if c.ConfidenceFloor < 0 || c.ConfidenceFloor > 1 {
		return commandScoreConfFloor
	}
	return c.ConfidenceFloor
}

// DescribeConfig renders the effective gate knobs for logs.
func DescribeConfig(c ScoreGateConfig) string {
	return fmt.Sprintf("enabled=%t score_threshold=%.2f confidence_threshold=%.2f",
		c.Enabled, c.effectiveThreshold(), c.effectiveConfFloor())
}
