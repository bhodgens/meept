//go:build linux

package auth

import (
	"fmt"
	"os"
	"strings"
)

// platformMachineID returns a unique hardware identifier for Linux by reading
// /etc/machine-id.
func platformMachineID() (string, error) {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return "", fmt.Errorf("read /etc/machine-id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
