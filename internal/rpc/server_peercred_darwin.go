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

// peerProcessID returns the kernel-verified peer PID for an accepted
// Unix-socket connection via getsockopt(LOCAL_PEERPID). ok is false when
// the connection is not a Unix socket, the kernel lookup fails, or the
// peer reports no PID. The PID cannot be forged by the connecting
// process; it is the identity the ACP approver gate walks ancestors from.
func peerProcessID(conn net.Conn) (pid int, ok bool) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}

	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, false
	}

	var peerPID int
	var sockErr error
	if ctrlErr := raw.Control(func(fd uintptr) {
		peerPID, sockErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); ctrlErr != nil {
		return 0, false
	}
	if sockErr != nil || peerPID <= 0 {
		return 0, false
	}
	return peerPID, true
}
