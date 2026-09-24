//go:build e2e

package sessionbinding

import (
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// start wraps harness.Start for local naming.
func start(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t)
}
