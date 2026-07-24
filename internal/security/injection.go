package security

import (
	"fmt"
	"regexp"
)

// BashInjectionPattern pairs a compiled regex with a human-readable name and
// the risk level assigned when the pattern matches a shell command.
type BashInjectionPattern struct {
	Pattern *regexp.Regexp
	Name    string
	Risk    RiskLevel
}

// BashInjectionFinding is a single detection result from CheckBashInjections.
type BashInjectionFinding struct {
	Rule    string
	Risk    RiskLevel
	Message string
}

// bashInjectionPatterns are shell metacharacter and expansion tricks that
// attackers use to smuggle past naive command blocklists. They are matched
// against the raw command string in addition to the curated command patterns
// evaluated by the engine.
var bashInjectionPatterns = []BashInjectionPattern{
	{regexp.MustCompile(`\bIFS\s*=`), "IFS manipulation", RiskHigh},
	{regexp.MustCompile(`\$\{?IFS\}?`), "IFS variable reference", RiskHigh},
	{regexp.MustCompile(`\{[a-zA-Z0-9/]+\.\.[a-zA-Z0-9/]+\}`), "brace expansion range", RiskMedium},
	{regexp.MustCompile(`\{[a-zA-Z],[a-zA-Z]\}`), "brace expansion alternation", RiskMedium},
	{regexp.MustCompile(`[\x{00A0}\x{1680}\x{2000}-\x{200B}\x{202F}\x{205F}\x{3000}\x{FEFF}]`), "unicode whitespace", RiskHigh},
	{regexp.MustCompile(`<\(`), "process substitution <()", RiskHigh},
	{regexp.MustCompile(`>\(`), "process substitution >()", RiskHigh},
	{regexp.MustCompile(`=\(`), "zsh process substitution =()", RiskHigh},
	{regexp.MustCompile(`\$\[`), "legacy arithmetic expansion $[]", RiskMedium},
	{regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F]`), "control characters", RiskHigh},
}

// CheckBashInjections scans a shell command for bash injection techniques and
// returns a finding for every pattern that matches. An empty result means the
// command matched none of the known injection patterns.
func CheckBashInjections(command string) []BashInjectionFinding {
	var findings []BashInjectionFinding
	for _, p := range bashInjectionPatterns {
		if p.Pattern.MatchString(command) {
			findings = append(findings, BashInjectionFinding{
				Rule:    "bash_injection_" + p.Name,
				Risk:    p.Risk,
				Message: fmt.Sprintf("detected %s in command", p.Name),
			})
		}
	}
	return findings
}

// MaxBashInjectionRisk returns the highest risk level among the findings, or
// RiskSafe when there are no findings. Callers use this to raise a command's
// effective risk when an injection technique is detected.
func MaxBashInjectionRisk(findings []BashInjectionFinding) RiskLevel {
	max := RiskSafe
	for _, f := range findings {
		if f.Risk > max {
			max = f.Risk
		}
	}
	return max
}
