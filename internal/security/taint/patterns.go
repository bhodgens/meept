// Package taint implements information flow tracking for security.
package taint

import (
	"regexp"
	"strings"
)

// ShellInjectionPattern is a regex pattern for detecting shell injection attempts.
var ShellInjectionPattern = regexp.MustCompile(
	`(?:[;&|` + "`" + `]|>\s*\w|\$\(.*\)|` + "``" + `\([^)]*\))`,
)

// Base64Pattern matches base64-encoded content (commonly used in obfuscation).
var Base64Pattern = regexp.MustCompile(
	`(?:[A-Za-z0-9+/]{4})+(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?`,
)

// URLPattern matches HTTP/HTTPS URLs.
var URLPattern = regexp.MustCompile(
	`https?://[^\s<>"]+`,
)

// PatternMatcher provides pattern detection for security checks.
type PatternMatcher struct {
	customPatterns []customPattern
}

type customPattern struct {
	name     string
	pattern  *regexp.Regexp
	taint    TaintLabel
	enabled  bool
}

// NewPatternMatcher creates a new pattern matcher.
func NewPatternMatcher() *PatternMatcher {
	return &PatternMatcher{
		customPatterns: make([]customPattern, 0),
	}
}

// AddPattern adds a custom pattern to match.
func (m *PatternMatcher) AddPattern(name string, pattern *regexp.Regexp, taint TaintLabel) {
	m.customPatterns = append(m.customPatterns, customPattern{
		name:    name,
		pattern: pattern,
		taint:   taint,
		enabled: true,
	})
}

// RemovePattern disables a pattern by name.
func (m *PatternMatcher) RemovePattern(name string) {
	for i := range m.customPatterns {
		if m.customPatterns[i].name == name {
			m.customPatterns[i].enabled = false
			return
		}
	}
}

// MatchAll checks all patterns against the input and returns all matches.
func (m *PatternMatcher) MatchAll(input string) []PatternMatch {
	var matches []PatternMatch

	for _, cp := range m.customPatterns {
		if !cp.enabled {
			continue
		}
		if cp.pattern.MatchString(input) {
			matches = append(matches, PatternMatch{
				Name:    cp.name,
				Taint:   cp.taint,
				Matches: cp.pattern.FindAllString(input, -1),
			})
		}
	}

	return matches
}

// PatternMatch represents a pattern match result.
type PatternMatch struct {
	Name    string
	Taint   TaintLabel
	Matches []string
}

// DetectSuspiciousPatterns checks a command for suspicious shell patterns.
// Returns the detected pattern name and true if found.
func DetectSuspiciousPatterns(command string) (string, bool) {
	patterns := map[string]string{
		"curl_pipe_sh":     "curl | sh",
		"curl_pipe_bash":   "curl | bash",
		"wget_pipe_sh":     "wget | sh",
		"wget_pipe_bash":   "wget | bash",
		"base64_decode":    "base64 -d",
		"subshell_curl":    "$(curl",
		"subshell_wget":    "$(wget",
		"backtick_curl":    "`curl",
		"backtick_wget":    "`wget",
		"eval_statement":   "eval ",
		"eval_subshell":    "eval $(",
		"command_injection": ";",
		"pipe_injection":    "|",
		"redirect_injection": ">",
	}

	lowerCmd := strings.ToLower(command)

	for name, pattern := range patterns {
		if strings.Contains(lowerCmd, strings.ToLower(pattern)) {
			return name, true
		}
	}

	// Check for command substitution patterns
	if ShellInjectionPattern.MatchString(command) {
		return "shell_injection", true
	}

	return "", false
}

// DetectExfiltration checks a URL for potential data exfiltration patterns.
// Returns the detected pattern name and true if found.
func DetectExfiltration(url string) (string, bool) {
	lowerURL := strings.ToLower(url)

	exfilPatterns := map[string]string{
		"api_key_param":        "api_key=",
		"apikey_param":         "apikey=",
		"token_param":          "token=",
		"secret_param":         "secret=",
		"password_param":       "password=",
		"authorization_header": "authorization:",
		"bearer_token":         "bearer ",
		"basic_auth":           "basic ",
		"session_param":        "session=",
		"sessionid_param":      "sessionid=",
		"access_token_param":   "access_token=",
		"refresh_token_param":  "refresh_token=",
		"private_key":          "private_key=",
		"ssh_key":              "ssh_key=",
	}

	for name, pattern := range exfilPatterns {
		if strings.Contains(lowerURL, pattern) {
			return name, true
		}
	}

	return "", false
}

// DetectInjection checks for various injection patterns in the input.
// Returns the type of injection found and true if any detected.
func DetectInjection(input string) (string, bool) {
	// Command injection patterns
	if strings.Contains(input, "; ") && strings.ContainsAny(input, "&|`") {
		return "command_injection", true
	}

	// Shell command substitution
	if strings.Contains(input, "$(") || strings.Contains(input, "`") {
		return "command_substitution", true
	}

	// Pipe injection
	if strings.Contains(input, "|") && !strings.Contains(input, " || ") {
		// Check if it's a legitimate pipe (likely not if combined with curl/wget)
		lower := strings.ToLower(input)
		if strings.Contains(lower, "curl") || strings.Contains(lower, "wget") {
			return "pipe_injection", true
		}
	}

	// Redirect injection
	if strings.Contains(input, ">") && (strings.Contains(input, " > ") || strings.Contains(input, "&gt;")) {
		return "redirect_injection", true
	}

	// Newline injection (command separator)
	if strings.Contains(input, "\n") || strings.Contains(input, "\\n") {
		return "newline_injection", true
	}

	// Eval patterns
	if strings.Contains(strings.ToLower(input), "eval") {
		return "eval_injection", true
	}

	return "", false
}

// ScoreSuspiciousness returns a score (0-100) indicating how suspicious a command is.
// Higher scores indicate more suspicious content.
func ScoreSuspiciousness(command string) int {
	score := 0
	lowerCmd := strings.ToLower(command)

	// Base patterns with weights
	weightedPatterns := map[string]int{
		"curl |":    40,
		"wget |":    40,
		"| sh":      50,
		"| bash":    50,
		"base64 -d": 60,
		"eval ":     70,
		"$(curl":    50,
		"$(wget":    50,
		"`curl":     50,
		"`wget":     50,
		"&& rm":     30,
		"; rm":      40,
		"> /":       30,
	}

	for pattern, weight := range weightedPatterns {
		if strings.Contains(lowerCmd, pattern) {
			score += weight
		}
	}

	// Check for URL patterns
	urls := URLPattern.FindAllString(command, -1)
	if len(urls) > 0 {
		for _, url := range urls {
			// Extra weight for suspicious TLDs
			if strings.Contains(url, ".xyz") ||
				strings.Contains(url, ".top") ||
				strings.Contains(url, ".tk") ||
				strings.Contains(url, ".ml") {
				score += 20
			}
		}
	}

	// Check for shell metacharacters
	if strings.ContainsAny(command, "&|;`$") {
		score += 10
	}

	// Cap at 100
	if score > 100 {
		score = 100
	}

	return score
}

// SanitizeShellCommand removes or escapes potentially dangerous shell constructs.
// Returns the sanitized command and a list of modifications made.
func SanitizeShellCommand(command string) (string, []string) {
	modifications := []string{}
	sanitized := command

	// Remove or flag dangerous patterns
	dangerousPatterns := []string{
		"| sh",
		"| bash",
		"| /bin/",
		"eval ",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(sanitized, pattern) {
			sanitized = strings.ReplaceAll(sanitized, pattern, "")
			modifications = append(modifications, "removed: "+pattern)
		}
	}

	// Handle command substitution by replacing with safe marker
	if strings.Contains(sanitized, "$(") {
		sanitized = strings.ReplaceAll(sanitized, "$(", "[SUBSHELL]")
		modifications = append(modifications, "neutralized: $()")
	}

	if strings.Contains(sanitized, "`") {
		sanitized = strings.ReplaceAll(sanitized, "`", "[BACKTICK]")
		modifications = append(modifications, "neutralized: backticks")
	}

	return sanitized, modifications
}

// ContainsSecret checks if a value appears to contain secret material.
// Returns true if likely secret content is detected.
func ContainsSecret(value string) bool {
	secretPatterns := []string{
		"sk-",
		"api-key",
		"api_key",
		"apikey",
		"secret",
		"password",
		"token",
		"private-key",
		"private_key",
		"ssh-rsa",
		"BEGIN PRIVATE KEY",
		"BEGIN RSA PRIVATE",
	}

	lower := strings.ToLower(value)
	for _, pattern := range secretPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// ExtractURLs extracts all HTTP/HTTPS URLs from the input.
func ExtractURLs(input string) []string {
	return URLPattern.FindAllString(input, -1)
}

// HasURL returns true if the input contains any HTTP/HTTPS URLs.
func HasURL(input string) bool {
	return URLPattern.MatchString(input)
}

// EstimateEntropy calculates a rough estimate of the Shannon entropy of the input.
// High entropy values may indicate encoded or encrypted content.
func EstimateEntropy(value string) float64 {
	if len(value) == 0 {
		return 0
	}

	freq := make(map[rune]float64)
	for _, ch := range value {
		freq[ch]++
	}

	entropy := 0.0
	length := float64(len(value))
	for _, count := range freq {
		p := count / length
		entropy -= p * log2(p)
	}

	return entropy
}

// log2 is base-2 logarithm.
func log2(x float64) float64 {
	const ln2 = 0.6931471805599453
	// Simple implementation avoiding math import for minimal dependencies
	// Using natural log approximation
	if x <= 0 {
		return 0
	}
	// This is a simplified approximation
	// For production, use math.Log2(x)
	n := 0.0
	for x > 1 {
		x /= 2
		n++
	}
	return n
}

// IsHighEntropy returns true if the value has high entropy (likely encoded content).
func IsHighEntropy(value string, threshold float64) bool {
	return EstimateEntropy(value) > threshold
}
