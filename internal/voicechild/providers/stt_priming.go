package providers

import (
	"log/slog"
	"strings"
)

// maxPrimingTerms caps the number of critical-entity terms pushed onto an
// STT request. A long vocabulary over-biases recognition toward the listed
// terms and degrades broad WER; keeping the list short is the F5 contract.
// Terms beyond the cap are dropped (the first N win) and a warning is logged.
const maxPrimingTerms = 100

// primingTerms normalizes a configured critical-entity vocabulary for the
// wire. It trims whitespace, drops empty entries, and caps the list at
// maxPrimingTerms. A nil/empty vocabulary returns nil so both providers keep
// their legacy wire shape byte-identically (R4). It never logs the terms
// themselves (they may be names or identifiers); the over-bias warning
// reports only the count.
func primingTerms(vocab []string) []string {
	if len(vocab) == 0 {
		return nil
	}
	terms := make([]string, 0, min(len(vocab), maxPrimingTerms))
	for _, raw := range vocab {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		if len(terms) == maxPrimingTerms {
			slog.Warn("voicechild stt: vocabulary exceeds cap; extra terms ignored",
				"configured", len(vocab), "cap", maxPrimingTerms)
			break
		}
		terms = append(terms, term)
	}
	if len(terms) == 0 {
		return nil
	}
	return terms
}
