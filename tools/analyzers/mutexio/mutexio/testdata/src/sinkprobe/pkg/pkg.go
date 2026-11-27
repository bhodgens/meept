// Package pkg hosts the sink-fixture SaveConfigAtomic/atomicWrite that
// sinkFuncs keys on: analysistest loads testdata under GOPATH semantics,
// so the fully-qualified name is "sinkprobe/pkg.<func>". This mirrors the
// production wrappers (internal/rpc/reasoning.go saveConfigAtomic/
// atomicWrite) — plain functions whose bodies funnel into os.* I/O with
// I/O-silent names.
package pkg

import "os"

// SaveConfigAtomic is the sink-fixture twin of rpc.saveConfigAtomic.
func SaveConfigAtomic(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// AtomicWrite is the sink-fixture twin of rpc.atomicWrite.
func AtomicWrite(path string, data []byte) error {
	return SaveConfigAtomic(path, data)
}
