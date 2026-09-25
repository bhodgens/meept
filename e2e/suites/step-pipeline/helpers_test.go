//go:build e2e

package steppipeline

import "os"

// readFile is a thin os.ReadFile wrapper kept so the test helpers stay
// one abstraction above the stdlib (swap for a harness reader if the
// assertions ever need sandbox enforcement).
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
