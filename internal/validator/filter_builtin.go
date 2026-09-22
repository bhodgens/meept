package validator

import (
	"errors"
	"fmt"
	"strings"
)

// BuiltinConfig carries the constructor parameters for the builtin filter
// set. Zero values are legal: the language filter defaults to "en" and the
// lint filter falls back to PATH lookup.
type BuiltinConfig struct {
	// ExpectedLang is the expected language code for language_<code>
	// filters; empty means "en".
	ExpectedLang string
	// GofmtBin is the gofmt binary path; empty means PATH lookup.
	GofmtBin string
	// GoBin is the go binary path (advisory vet); empty means PATH lookup.
	GoBin string
}

// knownBuiltinFilters lists the registry names in canonical order; it is
// the error text for unknown names and the documentation of the set.
var knownBuiltinFilters = []string{"json_format", "language_en", "lint_go"}

// NewBuiltinFilter returns the builtin OutputFilter registered under name:
// "json_format", "language_en" (or "language_<code>"), or "lint_go".
// Unknown names return an error listing the known names - the (nil, nil)
// pair is impossible by construction.
func NewBuiltinFilter(name string, cfg BuiltinConfig) (OutputFilter, error) {
	switch name {
	case "json_format":
		return NewJSONFormatFilter(), nil
	case "language_en":
		return NewLanguageFilter(cfg.ExpectedLang), nil
	case "lint_go":
		return NewGoLintFilter(cfg.GofmtBin, cfg.GoBin), nil
	default:
		return nil, fmt.Errorf("%w %q (known: %s)",
			errUnknownBuiltin, name, strings.Join(knownBuiltinFilters, ", "))
	}
}

// errUnknownBuiltin is the sentinel wrapped by NewBuiltinFilter for
// errors.Is checks by the config layer (leaf 04).
var errUnknownBuiltin = errors.New("unknown builtin output filter")
