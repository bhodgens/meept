package selflock

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestSelfLock(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, Analyzer, "bad")
}

func TestSelfLockClean(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, Analyzer, "clean")
}
