//go:build !darwin && !linux

package auth

import (
	"fmt"
	"os"
)

// platformMachineID returns a fallback hardware identifier using the
// executable path on platforms without a dedicated hardware UUID source.
func platformMachineID() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "unknown", nil
	}
	return exe, nil
}

// ensure build succeeds on non-darwin/non-linux platforms
var _ = fmt.Sprintf
