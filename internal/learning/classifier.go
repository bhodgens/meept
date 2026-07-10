package learning

import "strings"

// domainRule pairs a domain label with the keywords that signal it.
type domainRule struct {
	domain   string
	keywords []string
}

// domainRules is ordered by tie-break priority: when two domains have equal
// scores the one declared earlier wins (ClassifyDomain uses strict >).
var domainRules = []domainRule{
	{"code", []string{"code", "function", "package", "import", "interface", "struct", "method", "refactor", "compile"}},
	{"debugging", []string{"error", "fail", "bug", "panic", "stack trace", "crash", "nil pointer", "deadlock", "race"}},
	{"api_research", []string{"api", "endpoint", "http", "rest", "authenticate", "oauth", "webhook", "sdk", "graphql"}},
	{"security", []string{"vulnerability", "cve", "exploit", "injection", "xss", "csrf", "privilege", "authz", "sanitize", "crypto", "tls", "certificate"}},
	{"meept_internal", []string{"meept", "agent", "orchestrator", "dispatcher", "session", "memory", "rpc", "daemon", "plan", "skill"}},
	{"personal", []string{"my", "calendar", "email", "reminder", "meeting", "schedule", "todo", "task"}},
}

// ClassifyDomain routes a (query, toolOutput) pair to a domain label using
// keyword counting. Returns one of "code", "debugging", "api_research",
// "security", "meept_internal", or "personal". When no keywords match any
// domain the default is "code". Ties are broken by the declaration order in
// domainRules (code > debugging > api_research > security > meept_internal >
// personal).
func ClassifyDomain(query, toolOutput string) string {
	text := strings.ToLower(query + " " + toolOutput)
	bestScore := 0
	bestDomain := "code"
	for _, rule := range domainRules {
		s := countKeywords(text, rule.keywords)
		if s > bestScore {
			bestScore = s
			bestDomain = rule.domain
		}
	}
	return bestDomain
}

// countKeywords returns the number of times any keyword in the list appears
// as a substring of text. Matching is case-insensitive (caller should
// pre-lowercase text).
func countKeywords(text string, keywords []string) int {
	count := 0
	for _, kw := range keywords {
		count += strings.Count(text, strings.ToLower(kw))
	}
	return count
}
