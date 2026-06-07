//go:build darwin

package auth

import (
	"fmt"
	"os/exec"
	"strings"
)

// platformMachineID returns a unique hardware identifier for macOS by reading
// the IOPlatformUUID via ioreg.
func platformMachineID() (string, error) {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", fmt.Errorf("ioreg: %w", err)
	}
	str := string(out)
	for _, line := range strings.Split(str, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				uuid := strings.Trim(parts[1], "\" ")
				if uuid != "" {
					return uuid, nil
				}
			}
		}
	}
	return "", fmt.Errorf("IOPlatformUUID not found in ioreg output")
}
