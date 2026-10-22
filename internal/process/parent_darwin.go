//go:build darwin

package process

import "golang.org/x/sys/unix"

// ParentPID returns the parent PID of pid via sysctl(KERN_PROC). Returns
// (0, false) when the process is gone or the sysctl fails.
//
// Eproc.Ppid is the authoritative parent. ExternProc.P_oppid is only
// non-zero while the process is being traced (ptrace), so it must not be
// used for the ancestry walk: the ACP self-approval gate relies on this
// being unforgeable by the traced process itself.
func ParentPID(pid int) (int, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, false
	}
	ppid := int(kp.Eproc.Ppid)
	if ppid <= 0 {
		return 0, false
	}
	return ppid, true
}
