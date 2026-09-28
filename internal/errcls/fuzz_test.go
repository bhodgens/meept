package errcls

import (
	"fmt"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// wrappedTree builds a wrapped error tree of the given depth around base,
// mixing fmt.Errorf %w (unwrap-compatible) so the classifier's errors.Is/As
// walks exercise deep chains.
func wrappedTree(depth int, base error) error {
	err := base
	for i := range depth {
		err = fmt.Errorf("layer %d: %w", i, err)
	}
	return err
}

// FuzzErrorClassifiers exercises IsRateLimit / IsRetryable /
// IsRateLimitErrorMessage with arbitrary inputs and wrapped error trees.
// Invariants: never panics; pure functions of the input (no state mutated).
func FuzzErrorClassifiers(f *testing.F) {
	// String seeds for IsRateLimitErrorMessage.
	strSeeds := []string{
		"",
		"rate limit exceeded",
		"HTTP 429: Too Many Requests",
		"quota exceeded for model",
		"RATE_LIMIT hit",
		"requests per minute limit",
		"api calls per day exhausted",
		"rpm limit / tpm limit",
		"concurrent requests exceeded",
		"totally normal error",
		"4292 is not 429 but contains it",
		"unicode 4\u00e929 rate limit \u4e2d\u6587",
	}
	for _, s := range strSeeds {
		f.Add(s, false)
	}

	f.Fuzz(func(t *testing.T, msg string, wrap bool) {
		// String-only classification: never panics, no panicking regexp.
		_ = IsRateLimitErrorMessage(msg)

		// Error-tree classification over a few representative bases and
		// depths, including nil. These are pure predicates: results are
		// computed and discarded, the point is "no panic on any input".
		bases := []error{
			nil,
			fmt.Errorf("%s", msg),
			&llm.APIError{StatusCode: 429, Detail: msg},
			&llm.APIError{StatusCode: 500, Detail: msg},
			fmt.Errorf("wrap: %w", fmt.Errorf("%s", msg)),
		}
		for _, base := range bases {
			for _, depth := range []int{0, 1, 8} {
				err := wrappedTree(depth, base)
				_ = IsRateLimit(err)
				_ = IsRetryable(err)
			}
		}
	})
}
