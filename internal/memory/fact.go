package memory

import (
	"strings"
	"time"
)

// ExtractFactsFromMessages is the v1 heuristic fact extractor (leaf 12):
// a pure function over dialogue lines producing typed MemoryFacts. It is
// deliberately conservative — it captures only unambiguous patterns. The
// signature is the seam for a later LLM-backed extractor; call sites must
// not assume coverage, only safety.
//
// Patterns captured:
//   - preference: "I prefer X", "I like X", "I usually X"
//   - restriction: "I'm allergic to X", "I am allergic to X",
//     "I'm vegetarian", "I am vegan", "I can't eat X", "I cannot eat X"
//   - account: "my <name> number is <alnum-id>"
//   - temporal: "on <date phrase> I will <commitment>"
//
// Every returned fact carries sourceSession and UpdatedAt from the caller
// via stampFacts; here they are left zero so tests can assert shape.
func ExtractFactsFromMessages(msgs []string) []MemoryFact {
	var out []MemoryFact
	for _, raw := range msgs {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)

		switch {
		case hasRestriction(lower):
			out = append(out, MemoryFact{
				Kind:  FactRestriction,
				Key:   "dietary",
				Value: trimSentence(line),
			})
		case hasAccount(lower):
			if acct := extractAccount(line); acct.value != "" {
				out = append(out, MemoryFact{
					Kind:  FactAccount,
					Key:   acct.key,
					Value: acct.value,
				})
			}
		case hasTemporal(lower):
			out = append(out, MemoryFact{
				Kind:  FactTemporal,
				Key:   "commitment",
				Value: trimSentence(line),
			})
		case hasPreference(lower):
			out = append(out, MemoryFact{
				Kind:  FactPreference,
				Key:   "preference",
				Value: trimSentence(line),
			})
		}
	}
	return out
}

// StampFacts fills OwnerID, SourceSession, and UpdatedAt on extracted
// facts (helper for callers; keeps Extract pure and testable).
func StampFacts(facts []MemoryFact, ownerID, sourceSession string, at time.Time) {
	for i := range facts {
		facts[i].OwnerID = ownerID
		facts[i].SourceSession = sourceSession
		facts[i].UpdatedAt = at
	}
}

func hasPreference(lower string) bool {
	return containsAny(lower,
		"i prefer ", "i like ", "i usually ", "i always ", "i never ")
}

func hasRestriction(lower string) bool {
	return containsAny(lower,
		"allergic to", "i'm vegetarian", "i am vegetarian",
		"i'm vegan", "i am vegan", "can't eat", "cannot eat")
}

func hasTemporal(lower string) bool {
	return containsAny(lower, "i will ", "i'll ", "i'm going to ", "i am going to ") &&
		containsAny(lower,
			"monday", "tuesday", "wednesday", "thursday", "friday",
			"saturday", "sunday", "tomorrow", "next week", "next month",
			"january", "february", "march", "april", "may", "june",
			"july", "august", "september", "october", "november", "december")
}

type accountFact struct{ key, value string }

func hasAccount(lower string) bool {
	return containsAny(lower, "number is", "id is")
}

func extractAccount(line string) accountFact {
	marker := ""
	switch {
	case strings.Contains(line, "number is"):
		marker = "number is"
	case strings.Contains(line, "id is"):
		marker = "id is"
	default:
		return accountFact{}
	}
	idx := strings.Index(strings.ToLower(line), marker)
	value := strings.TrimSpace(line[idx+len(marker):])
	// Value runs to end of sentence.
	if cut, _, found := strings.Cut(value, "."); found {
		value = cut
	}
	value = strings.Trim(value, ".,!?")
	if value == "" {
		return accountFact{}
	}
	// Key = the words immediately before the marker ("united mileageplus").
	before := strings.ToLower(strings.TrimSpace(line[:idx]))
	fields := strings.Fields(before)
	if len(fields) < 1 {
		return accountFact{key: "account", value: value}
	}
	// Take up to the last 3 words before the marker as the key label.
	start := len(fields) - 3
	if start < 0 {
		start = 0
	}
	key := strings.Join(fields[start:], " ")
	// Drop leading articles.
	for _, art := range []string{"my ", "the "} {
		key = strings.TrimPrefix(key, art)
	}
	if key == "" {
		key = "account"
	}
	return accountFact{key: key, value: value}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// trimSentence trims surrounding whitespace and trailing punctuation.
func trimSentence(line string) string {
	line = strings.TrimSpace(line)
	if cut, _, found := strings.Cut(line, "."); found && len(cut) > 0 {
		return cut
	}
	return line
}
