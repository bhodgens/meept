//go:build darwin

package llm

import "golang.org/x/sys/unix"

// codexOSVersionRaw returns the macOS product version (e.g. "26.5.2") via
// the kern.osproductversion sysctl, matching the "Mac OS <ver>" shape the
// codex-rs os_info crate emits. Falls back to the BSD kernel version
// prefix when the product version is unavailable.
func codexOSVersionRaw() string {
	if v, err := unix.Sysctl("kern.osproductversion"); err == nil && v != "" {
		return v
	}
	if v, err := unix.Sysctl("kern.osrelease"); err == nil {
		return v
	}
	return ""
}
