//go:build darwin

package rpc

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerCredential returns the kernel-verified peer UID for an accepted
// Unix-socket connection via getsockopt(LOCAL_PEERCRED). ok is false when
// the connection is not a Unix socket or the kernel lookup fails.
func peerCredential(conn net.Conn) (uid int, ok bool) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}

	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, false
	}

	var cred *unix.Xucred
	var sockErr error
	if ctrlErr := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); ctrlErr != nil {
		return 0, false
	}
	if sockErr != nil || cred == nil {
		return 0, false
	}
	return int(cred.Uid), true
}
