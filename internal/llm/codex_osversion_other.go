//go:build !darwin && !windows

package llm

import "golang.org/x/sys/unix"

// codexOSVersionRaw returns the OS kernel release (e.g. "6.12.34") via
// uname. Only the dotted-numeric prefix is ever surfaced — sanitizeOSVersion
// strips any trailing packaging suffix ("6.12.34-1-MANJARO" → "6.12.34").
func codexOSVersionRaw() string {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return ""
	}
	// Int8 array → string up to the first NUL.
	out := make([]byte, 0, len(uts.Release))
	for _, c := range uts.Release {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}
