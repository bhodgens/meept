//go:build linux

package process

import (
	"os"
	"strconv"
	"strings"
)

// ParentPID returns the parent PID of pid by parsing the PPid field of
// /proc/<pid>/status. Returns (0, false) when the process is gone or the
// field cannot be parsed — callers treat a missing parent as "not an
// ancestor" (fail-closed against false matches, fail-open against false
// accusations is handled one level up).
func ParentPID(pid int) (int, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if ppid, err := strconv.Atoi(fields[1]); err == nil {
					return ppid, true
				}
			}
			return 0, false
		}
	}
	return 0, false
}
