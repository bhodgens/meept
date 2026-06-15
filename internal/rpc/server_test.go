package rpc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestIsParameterError_Structured verifies that isParameterError classifies
// errors using structured detection (errors.Is/As) rather than substring
// heuristics that false-positive on common words.
//
// Pre-migration, isParameterError took a string and matched substrings like
// "expected", "type", "parse", etc. This test verifies the new behavior.
//
// Note: services.ErrInvalidInput is tested in internal/errcls/classify_test.go.
// This test cannot import services due to an import cycle
// (services -> scheduler -> rpc), so we focus on the *llm.APIError 400 path
// and the false-positive regressions.
func TestIsParameterError_Structured(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// True cases — structured parameter errors
		{"api 400", &llm.APIError{StatusCode: 400, Detail: "bad request"}, true},
		{"wrapped api 400", fmt.Errorf("handler: %w", &llm.APIError{StatusCode: 400}), true},

		// False-positive cases that the old substring heuristic incorrectly
		// classified as parameter errors. These MUST return false now.
		{"plain 'expected' string (old false positive)", errors.New("expected 1 result, got 0"), false},
		{"plain 'type' string (old false positive)", errors.New("type mismatch in data"), false},
		{"plain 'parse' string (old false positive)", errors.New("parse phase completed"), false},
		{"plain 'missing' string (old false positive)", errors.New("missing file on disk"), false},
		{"plain 'invalid' string (old false positive)", errors.New("invalid state reached"), false},
		{"plain 'required' string (old false positive)", errors.New("required field absent"), false},
		{"plain 'unmarshal' string (old false positive)", errors.New("unmarshal step skipped"), false},
		{"plain 'argument' string (old false positive)", errors.New("argument list too long"), false},
		{"plain 'param' string (old false positive)", errors.New("param export complete"), false},

		// Negative cases
		{"api 500", &llm.APIError{StatusCode: 500}, false},
		{"api 404", &llm.APIError{StatusCode: 404}, false},
		{"nil", nil, false},
		{"generic internal error", errors.New("internal failure"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isParameterError(tt.err); got != tt.want {
				t.Errorf("isParameterError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
