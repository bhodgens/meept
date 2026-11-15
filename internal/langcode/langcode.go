// Package langcode owns the ISO-639 language-code vocabulary shared by the
// config validation surface (internal/config) and the output-filter detector
// (internal/validator).
//
// Why a separate package: internal/config needs these helpers to fail-fast
// on an unknown daemon.output_filters.expected_language at load, and
// internal/validator needs them for the same vocabulary in its filter. A
// direct config -> validator import used to carry that, but the validator
// package's validators take *task.TaskStep, and internal/task imports
// internal/queue — so config transitively depends on the task/queue machines.
// The kanban StateEvent publishers (plan 02/03, C2) put internal/queue above
// internal/kanban, which would have closed a cycle through config. The
// vocabulary itself is pure stdlib, so it lives here as the single shared
// authority and both consumers import DOWN to it (llm.TierPolicy/
// FailurePolicyConfig precedent: mirror rather than cycle).
//
// internal/validator keeps same-named wrapper funcs over this package for
// its existing callers and test pins; this package stays the one list —
// never duplicate the tables.
package langcode

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Default is the language code assumed when none is configured — the frozen
// pre-existing default for an empty expected_language.
const Default = "en"

// Builtin is every language code the detector can name with NO user data:
// the per-rune script codes plus the two bundled Latin-script word tables. A
// code outside this set is still perfectly usable — it just needs a word
// table at $MEEPT_HOME/validator/lang/<code>.txt (the data-driven tier). This
// is the vocabulary the config layer validates against;
// TestBuiltinLangCodesCoverScriptCodes (internal/validator) pins it against
// the codes scriptCodeByRune actually returns.
var Builtin = []string{
	"ar", "de", "el", "en", "he", "hi", "ja", "ko", "ru", "th", "zh",
}

// Normalize reduces a configured language code to the single token detection
// actually compares: lowercased, region/script subtags dropped ("en-US" ->
// "en", "EN_us" -> "en"), and a shape check that rejects anything that is not
// an ISO-639 primary subtag (2-3 ASCII letters). It reports false for "" and
// for typos like "english".
//
// Why a primary-subtag shape and not a full BCP-47 parse: detection returns a
// bare primary code (the bundled tables' literals and the word-table filename
// stem), so a region qualifier can never match it. Accepting "en-US" as "en"
// is the deliberate call (bughunt wave M5): a well-formed tag whose PRIMARY
// language is real is the user's intent spelled with a region, and treating it
// as "no words match" would reject every output including correct English — a
// self-inflicted denial of service on the chat path. A code whose primary
// subtag is not real ("xx", "english") is NOT repaired here: it fails the
// vocabulary check at config load, where the mistake is visible, instead of
// resolving to some other language here.
func Normalize(raw string) (string, bool) {
	code := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		// An empty subtag on either side ("en-", "-en") is a malformed
		// tag, not a base language: reject rather than silently repair.
		if i == 0 || i == len(code)-1 {
			return "", false
		}
		code = code[:i]
	}
	if len(code) < 2 || len(code) > 3 {
		return "", false
	}
	for i := range len(code) {
		if code[i] < 'a' || code[i] > 'z' {
			return "", false
		}
	}
	return code, true
}

// IsKnown reports whether raw names a language detection can resolve with no
// user data: it normalizes to a primary subtag AND that subtag is one of the
// bundled script codes or word tables. It is the predicate the config layer
// uses to reject an unknown code at load.
func IsKnown(raw string) bool {
	code, ok := Normalize(raw)
	if !ok {
		return false
	}
	return slices.Contains(Builtin, code)
}

// HasWordTable reports whether a user word table for raw is present in dir —
// the second way a code becomes usable (the data-driven Latin-script tier).
// Callers pass $MEEPT_HOME/validator/lang. It shares Normalize with the
// loader, so the code a table satisfies is exactly the code the filter
// compares against.
func HasWordTable(raw, dir string) bool {
	code, ok := Normalize(raw)
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, code+".txt"))
	return err == nil && !info.IsDir()
}
