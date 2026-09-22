package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/caimlas/meept/internal/task"
)

// jsonToolHints is the small known set of tool hints whose outputs are
// expected to carry JSON payloads. Chain membership (the configured
// output_filters list) is the declaration; this set is the additional
// "step expects JSON" gate from the leaf contract.
var jsonToolHints = map[string]struct{}{
	"api":          {},
	"api_call":     {},
	"curl":         {},
	"fetch":        {},
	"http":         {},
	"http_request": {},
	"json":         {},
	"rest":         {},
	"web_fetch":    {},
}

// JSONFormatFilter validates step output as JSON and canonically reformats
// it: 2-space indent, map keys sorted (encoding/json sorts map keys on
// marshal), trailing newline. Idempotent by construction: canonical input
// is byte-identical to its own canonical form, so the second run passes.
type JSONFormatFilter struct{}

// NewJSONFormatFilter creates a json_format output filter.
func NewJSONFormatFilter() *JSONFormatFilter { return &JSONFormatFilter{} }

// Name implements OutputFilter.
func (f *JSONFormatFilter) Name() string { return "json_format" }

// Applies implements OutputFilter: the step must expect JSON output
// (declared by chain membership) AND carry a JSON-ish tool hint.
func (f *JSONFormatFilter) Applies(step *task.TaskStep) bool {
	if step == nil {
		return false
	}
	_, ok := jsonToolHints[strings.ToLower(step.ToolHint)]
	return ok
}

// Process implements OutputFilter. Empty/whitespace output passes (nothing
// to validate); invalid JSON fails with the parser's byte offset in the
// Reason; valid JSON that is not already canonical is rewritten.
func (f *JSONFormatFilter) Process(_ context.Context, _ *task.TaskStep, output string) FilterResult {
	const self = "json_format"
	if strings.TrimSpace(output) == "" {
		return FilterResult{Outcome: FilterPass, Filter: self}
	}
	value, err := decodeSingleJSON(output)
	if err != nil {
		return FilterResult{Outcome: FilterFail, Filter: self, Reason: jsonInvalidReason(err)}
	}
	canonical, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		// A decoded generic value always re-marshals; treat any failure as
		// invalid input rather than fabricating a rewrite.
		return FilterResult{Outcome: FilterFail, Filter: self,
			Reason: fmt.Sprintf("invalid JSON at offset 0: %v", err)}
	}
	formatted := string(canonical) + "\n"
	if formatted == output {
		return FilterResult{Outcome: FilterPass, Filter: self}
	}
	return FilterResult{Outcome: FilterRewrite, Filter: self, Output: formatted}
}

// decodeSingleJSON parses exactly one JSON value and nothing else,
// preserving number literals via json.Number so canonical reformatting
// never alters numeric precision.
func decodeSingleJSON(output string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(output))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("unexpected trailing data after JSON value")
		}
		return nil, err
	}
	return value, nil
}

// jsonInvalidReason renders the parser failure prefixed with its byte
// offset: the machine-readable repair anchor
// "invalid JSON at offset N: <err>".
func jsonInvalidReason(err error) string {
	offset := int64(0)
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		offset = syn.Offset
	} else {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			offset = typeErr.Offset
		}
	}
	return fmt.Sprintf("invalid JSON at offset %d: %v", offset, err)
}
