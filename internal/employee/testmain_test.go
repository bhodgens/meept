package employee

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain chdirs to the module root: NewComponents resolves
// config/models.json5 relative to the working directory, and `go test`
// runs with the package directory as CWD. Fresh machines without a live
// ~/.meept (CI) then fail with "models.json5 not found".
func TestMain(m *testing.M) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if err := os.Chdir(root); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
