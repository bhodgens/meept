package fieldguard

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestGuardedFieldAccess(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, Analyzer, "guarded")
}

func TestImmutableFieldWrite(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, Analyzer, "immutable")
}
