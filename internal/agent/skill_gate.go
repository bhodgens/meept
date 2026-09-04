package agent

import (
	"strings"

	"github.com/caimlas/meept/internal/skills"
)

// SkillEntryView is the minimal skill metadata the domain-agreement gate
// inspects. It decouples the gate from the full internal/skills index entry
// so tests can exercise it without building a CapabilityIndex.
type SkillEntryView struct {
	Name        string
	Tags        []string
	Description string
}

// domainAgrees reports whether any non-stopword domain token from the query
// appears (case-insensitive substring) in the skill's name, tags, or
// description. Queries that yield no domain tokens (short or generic, e.g.
// "hi", "help me") pass through unconditionally so the gate never suppresses
// discovery for vague inputs — threshold semantics stay unchanged.
//
// It extends the stopword work in commit e0d08e2f by reusing the same single
// source of truth: internal/skills.StopWordSet (the exported form of
// defaultStopWords, which that commit extended). internal/agent already
// imports internal/skills for the capability index, so the dependency
// direction is acyclic.
func domainAgrees(query string, entry SkillEntryView) bool {
	domainTokens := queryDomainTokens(query)
	if len(domainTokens) == 0 {
		return true
	}

	nameLower := strings.ToLower(entry.Name)
	descLower := strings.ToLower(entry.Description)
	tagsLower := make([]string, 0, len(entry.Tags))
	for _, tag := range entry.Tags {
		tagsLower = append(tagsLower, strings.ToLower(tag))
	}

	for _, token := range domainTokens {
		if strings.Contains(nameLower, token) {
			return true
		}
		for _, tagLower := range tagsLower {
			if strings.Contains(tagLower, token) {
				return true
			}
		}
		if strings.Contains(descLower, token) {
			return true
		}
	}
	return false
}

// queryDomainTokens splits the query on non-alphanumeric runs, lowercases,
// drops stopwords and tokens shorter than 4 characters, and returns the
// remaining domain-flavored tokens.
func queryDomainTokens(query string) []string {
	stopWords := skills.StopWordSet()

	var tokens []string
	var current strings.Builder
	flush := func() {
		token := current.String()
		current.Reset()
		if token == "" {
			return
		}
		if len(token) < 4 || stopWords[token] {
			return
		}
		tokens = append(tokens, token)
	}
	for _, r := range strings.ToLower(query) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}
