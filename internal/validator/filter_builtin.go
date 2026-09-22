package validator

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// BuiltinConfig carries the constructor parameters for the builtin filter
// set. Zero values are legal: the language filter defaults to "en", the
// lint filters fall back to PATH lookup, and the script linters resolve
// python/node/tsc from PATH and well-known locations.
type BuiltinConfig struct {
	// ExpectedLang is the expected language code for language_<code>
	// filters; empty means "en".
	ExpectedLang string
	// GofmtBin is the gofmt binary path; empty means PATH lookup.
	GofmtBin string
	// GoBin is the go binary path (advisory vet); empty means PATH lookup.
	GoBin string
	// PythonBin is the python binary for lint_python; empty means PATH
	// lookup ("python3").
	PythonBin string
	// NodeBin is the node binary for lint_js; empty means PATH lookup.
	NodeBin string
	// TSCBin is the tsc binary for lint_js TS blocks; empty means PATH +
	// well-known nvm locations.
	TSCBin string
}

// knownBuiltinFilters lists the registry names in canonical order; it is
// the error text for unknown names and the documentation of the set.
var knownBuiltinFilters = []string{
	"json_format", "language_en", "lint_go", "lint_python", "lint_js",
}

// NewBuiltinFilter returns the builtin OutputFilter registered under name:
// "json_format", "language_en" (or "language_<code>"), "lint_go",
// "lint_python", or "lint_js". Unknown names return an error listing the
// known names - the (nil, nil) pair is impossible by construction.
func NewBuiltinFilter(name string, cfg BuiltinConfig) (OutputFilter, error) {
	switch name {
	case "json_format":
		return NewJSONFormatFilter(), nil
	case "language_en":
		return NewLanguageFilter(cfg.ExpectedLang), nil
	case "lint_go":
		return NewGoLintFilter(cfg.GofmtBin, cfg.GoBin), nil
	case "lint_python":
		return NewPythonLintFilter(cfg.PythonBin), nil
	case "lint_js":
		return NewJSLintFilter(cfg.NodeBin, cfg.TSCBin), nil
	default:
		return nil, fmt.Errorf("%w %q (known: %s)",
			errUnknownBuiltin, name, strings.Join(knownBuiltinFilters, ", "))
	}
}

// errUnknownBuiltin is the sentinel wrapped by NewBuiltinFilter for
// errors.Is checks by the config layer (leaf 04).
var errUnknownBuiltin = errors.New("unknown builtin output filter")

// toolOnPath reports whether a binary is resolvable on PATH. The daemon
// wiring uses it to keep host-missing linters out of the default chain so
// a fresh install without node never sees lint_js rejections caused by the
// missing binary rather than by the content.
func toolOnPath(bin string) bool {
	if bin == "" {
		return false
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// DefaultFilters returns the default filter set for a host: the
// always-safe content filters plus every script linter whose toolchain is
// resolvable on this host. Used by the daemon wiring when the config does
// not override `filters` - enabling output_filters with an empty list
// means "the sensible defaults for THIS host", not "nothing".
func DefaultFilters() []string {
	filters := []string{"json_format", "language_en", "lint_go"}
	if toolOnPath("python3") || toolOnPath("python") {
		filters = append(filters, "lint_python")
	}
	if toolOnPath("node") {
		filters = append(filters, "lint_js")
	}
	return filters
}
