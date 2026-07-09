package learning

import "strings"

// ClassifyDomain routes a (query, toolOutput) pair to a domain label using
// keyword counting. Returns one of "code", "debugging", or "api_research".
func ClassifyDomain(query, toolOutput string) string {
	codeKeywords := []string{"code", "function", "package", "import", "interface"}
	debugKeywords := []string{"error", "fail", "bug", "panic", "stack trace"}
	apiKeywords := []string{"api", "endpoint", "http", "rest", "authenticate"}

	text := strings.ToLower(query + " " + toolOutput)

	codeScore := countKeywords(text, codeKeywords)
	debugScore := countKeywords(text, debugKeywords)
	apiScore := countKeywords(text, apiKeywords)

	if codeScore >= debugScore && codeScore >= apiScore {
		return "code"
	} else if debugScore >= apiScore {
		return "debugging"
	}
	return "api_research"
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
