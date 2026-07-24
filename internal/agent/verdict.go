package agent

import (
	"regexp"
	"strings"
)

// Verdict represents the outcome of an adversarial verification check.
type Verdict int

const (
	// VerdictPass indicates all checks passed.
	VerdictPass Verdict = iota
	// VerdictFail indicates one or more critical checks failed.
	VerdictFail
	// VerdictPartial indicates the implementation mostly works but has issues.
	VerdictPartial
	// VerdictUnknown indicates no valid verdict was found in the output.
	VerdictUnknown
)

// String returns the human-readable verdict name.
func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "PASS"
	case VerdictFail:
		return "FAIL"
	case VerdictPartial:
		return "PARTIAL"
	default:
		return "UNKNOWN"
	}
}

// CheckResult represents a single verification check performed by the verifier.
type CheckResult struct {
	// Name is the check identifier (from CHECK: line).
	Name string
	// Command is the command or action taken (from COMMAND: line).
	Command string
	// Output is the observed output or result (from OUTPUT: line).
	Output string
	// Passed indicates whether this individual check passed.
	Passed bool
}

// verdictRe matches the VERDICT line at the start of a line.
var verdictRe = regexp.MustCompile(`(?m)^VERDICT:\s*(PASS|FAIL|PARTIAL)\s*$`)

// checkNameRe matches CHECK: lines.
var checkNameRe = regexp.MustCompile(`(?m)^CHECK:\s*(.+)$`)

// checkCommandRe matches COMMAND: lines.
var checkCommandRe = regexp.MustCompile(`(?m)^COMMAND:\s*(.+)$`)

// checkOutputRe matches OUTPUT: lines.
var checkOutputRe = regexp.MustCompile(`(?m)^OUTPUT:\s*(.+)$`)

// checkResultRe matches RESULT: lines.
var checkResultRe = regexp.MustCompile(`(?m)^RESULT:\s*(PASS|FAIL)\s*$`)

// ParseVerdict extracts the verdict and individual check results from verifier output.
// It returns VerdictUnknown if no valid VERDICT line is found.
func ParseVerdict(output string) (Verdict, []CheckResult) {
	checks := parseChecks(output)

	match := verdictRe.FindStringSubmatch(output)
	if match == nil {
		return VerdictUnknown, checks
	}

	switch strings.TrimSpace(match[1]) {
	case "PASS":
		return VerdictPass, checks
	case "FAIL":
		return VerdictFail, checks
	case "PARTIAL":
		return VerdictPartial, checks
	default:
		return VerdictUnknown, checks
	}
}

// parseChecks extracts individual CHECK blocks from verifier output.
// Each block starts with CHECK: and may contain COMMAND:, OUTPUT:, and RESULT: lines.
func parseChecks(output string) []CheckResult {
	nameMatches := checkNameRe.FindAllStringSubmatchIndex(output, -1)
	if len(nameMatches) == 0 {
		return nil
	}

	checks := make([]CheckResult, 0, len(nameMatches))

	for i, nameIdx := range nameMatches {
		name := strings.TrimSpace(output[nameIdx[2]:nameIdx[3]])

		// Determine the segment of text belonging to this check block:
		// from the end of this CHECK line to the start of the next CHECK line (or end of output).
		segStart := nameIdx[1]
		segEnd := len(output)
		if i+1 < len(nameMatches) {
			segEnd = nameMatches[i+1][0]
		}
		segment := output[segStart:segEnd]

		check := CheckResult{
			Name: name,
		}

		if m := checkCommandRe.FindStringSubmatch(segment); m != nil {
			check.Command = strings.TrimSpace(m[1])
		}
		if m := checkOutputRe.FindStringSubmatch(segment); m != nil {
			check.Output = strings.TrimSpace(m[1])
		}
		if m := checkResultRe.FindStringSubmatch(segment); m != nil {
			check.Passed = strings.TrimSpace(m[1]) == "PASS"
		}

		checks = append(checks, check)
	}

	return checks
}
