package session

import (
	"encoding/json"
	"strings"
)

// cleanTitleResult applies the standard cleanup pipeline to an LLM-generated
// title JSON response. It lowercases, trims whitespace, removes trailing
// periods from the description, enforces a single-word name, and truncates
// long descriptions.
func cleanTitleResult(raw, nameFallback, descFallback string) (name, desc string) {
	name = nameFallback
	desc = descFallback

	content := strings.TrimSpace(raw)
	if content == "" {
		return
	}

	var tmp struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(content), &tmp); err != nil {
		return
	}

	name = strings.ToLower(strings.TrimSpace(tmp.Name))
	desc = strings.ToLower(strings.TrimSpace(tmp.Description))
	desc = strings.TrimSuffix(desc, ".")

	// Ensure name is a single word
	if words := strings.Fields(name); len(words) > 1 {
		name = words[0]
	}
	if name == "" {
		name = "chat"
	}
	if len(desc) > 60 {
		desc = desc[:57] + "..."
	}

	return
}
