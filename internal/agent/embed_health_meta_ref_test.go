package agent

import (
	"fmt"
	"testing"
)

// TestPrefilterStoreMetaReferences pins a reference to prefilterStoreMeta
// (U1000: the type is reserved for the planned per-intent retention report
// but currently unreferenced). Compile-only use keeps the type without
// widening lint exclusions.
func TestPrefilterStoreMetaReferences(t *testing.T) {
	var m prefilterStoreMeta
	if len(m.Examples) != 0 {
		t.Fatal("zero value must have no examples")
	}
	_ = fmt.Sprintf("%T", m)
}
