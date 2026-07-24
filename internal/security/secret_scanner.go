package security

import (
	"fmt"
	"regexp"
	"strings"
)

// SecretScanner detects unknown secrets in text using regex pattern rules.
type SecretScanner struct {
	rules []SecretRule
}

// SecretRule defines a named regex pattern for detecting a specific secret type.
type SecretRule struct {
	Name    string
	Pattern *regexp.Regexp
}

// NewSecretScanner creates a scanner loaded with default secret detection rules.
func NewSecretScanner() *SecretScanner {
	return &SecretScanner{rules: defaultSecretRules()}
}

func defaultSecretRules() []SecretRule {
	return []SecretRule{
		{"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
		{"aws_secret_key", regexp.MustCompile(`(?i)aws_secret_access_key\s*[=:]\s*[A-Za-z0-9/+=]{40}`)},
		{"github_token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,255}`)},
		{"github_fine_grained", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`)},
		{"openai_key", regexp.MustCompile(`sk-[A-Za-z0-9]{20}T3BlbkFJ[A-Za-z0-9]{20}`)},
		{"anthropic_key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{90,}`)},
		{"slack_token", regexp.MustCompile(`xox[baprs]-[0-9]{10,13}-[0-9]{10,13}-[a-zA-Z0-9]{24,34}`)},
		{"stripe_key", regexp.MustCompile(`[sr]k_live_[0-9a-zA-Z]{24,}`)},
		{"private_key_header", regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`)},
		{"jwt_token", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
		{"generic_api_key", regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[=:]\s*['"][A-Za-z0-9_\-]{20,}['"]`)},
	}
}

// Scan returns the names of all rules that match the given text.
func (s *SecretScanner) Scan(text string) []string {
	var matches []string
	for _, rule := range s.rules {
		if rule.Pattern.MatchString(text) {
			matches = append(matches, rule.Name)
		}
	}
	return matches
}

// ScanAndReport returns a human-readable warning if any secrets are detected,
// or an empty string if the text is clean.
func (s *SecretScanner) ScanAndReport(text string) string {
	matches := s.Scan(text)
	if len(matches) == 0 {
		return ""
	}
	return fmt.Sprintf("WARNING: potential secrets detected (%s). Review output before sharing or storing.", strings.Join(matches, ", "))
}
