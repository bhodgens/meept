package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if err := os.Chdir(root); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
