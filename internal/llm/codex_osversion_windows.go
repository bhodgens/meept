//go:build windows

package llm

import "golang.org/x/sys/windows"

// codexOSVersionRaw returns the Windows version (e.g. "10.0.26100") via
// RtlGetNtVersionNumbers, the same source codex-rs's os_info crate uses.
func codexOSVersionRaw() string {
	major, minor, build := windows.RtlGetNtVersionNumbers()
	return itoaU32(major) + "." + itoaU32(minor) + "." + itoaU32(build&0xffff)
}

func itoaU32(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
