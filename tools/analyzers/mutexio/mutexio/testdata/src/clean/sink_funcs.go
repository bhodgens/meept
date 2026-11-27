// Package clean — sink-function table negative fixture (M8/D-2).
//
// The mutexio sink table resolves plain function calls by fully-qualified
// name (mutexio.sinkFuncs), keyed on the PRODUCTION sink names. This
// package declares same-named functions in a DIFFERENT package to pin the
// two negative properties at once:
//
//  1. qualified-name matching: a local saveConfigAtomic here does NOT
//     match the table entry (github.com/caimlas/meept/internal/rpc.…),
//     so a fixture-local name can never collide with a production sink.
//  2. no lock, no report: a sink call outside any Lock/Unlock range
//     produces no diagnostic even for a table-qualified entry (covered
//     by the range check itself — the clean package compiles with zero
//     expectations and analysistest fails on any unexpected report).
package clean

import "sync"

// saveConfigAtomic is deliberately named after the production sink. It
// lives in package clean, so its fully-qualified name does not match any
// sinkFuncs entry — calling it under a lock must NOT be flagged here (the
// name-match is package-qualified, not textual).
func saveConfigAtomic(path string, data []byte) error {
	return writeAll(path, data)
}

func writeAll(path string, data []byte) error {
	// Body intentionally without real I/O imports: this fixture checks
	// analyzer resolution, not behavior.
	if path == "" || data == nil { //nolint:nilseam // fixture shape check, no real I/O
		return nil
	}
	return nil
}

// cleanSinkFuncOutsideLock: the fixture-local saveConfigAtomic called
// under a lock — MUST stay silent (wrong package: no table match).
func cleanSinkFuncOutsideLock() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	if err := saveConfigAtomic("/tmp/clean-cfg", []byte("y")); err != nil {
		return //nolint:nilseam // fixture: error value intentionally unused, analyzer-resolution check only
	}
}

// cleanSinkFuncNoLock: same name called with no lock at all — silent.
func cleanSinkFuncNoLock() {
	if err := saveConfigAtomic("/tmp/clean-cfg", []byte("y")); err != nil {
		return //nolint:nilseam // fixture: error value intentionally unused, analyzer-resolution check only
	}
}
