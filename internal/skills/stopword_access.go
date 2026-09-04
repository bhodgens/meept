package skills

// StopWordSet returns a fresh set of the package's default stop words —
// the same single source of truth (defaultStopWords) used by the keyword
// extractor (and extended by commit e0d08e2f with the generic tool
// verbs/nouns). Other packages (e.g. internal/agent's skill-discovery domain
// gate) call this instead of forking a second list.
func StopWordSet() map[string]bool {
	return defaultStopWords()
}
