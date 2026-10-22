//go:build linux

package rpc

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerCredential returns the kernel-verified peer UID for an accepted
// Unix-socket connection using SO_PEERCRED. ok is false when the
// connection is not a Unix socket or the kernel lookup fails.
func peerCredential(conn net.Conn) (uid int, ok bool) {
	cred, ok := peerUcred(conn)
	if !ok {
		return 0, false
	}
	return int(cred.Uid), true
}

// peerProcessID returns the kernel-verified peer PID for an accepted
// Unix-socket connection using SO_PEERCRED. ok is false when the
// connection is not a Unix socket, the kernel lookup fails, or the peer
// reports no PID. The PID cannot be forged by the connecting process;
// it is the identity the ACP approver gate walks ancestors from.
func peerProcessID(conn net.Conn) (pid int, ok bool) {
	cred, ok := peerUcred(conn)
	if !ok || cred.Pid <= 0 {
		return 0, false
	}
	return int(cred.Pid), true
}

// peerUcred fetches the full SO_PEERCRED payload once for both accessors.
func peerUcred(conn net.Conn) (*unix.Ucred, bool) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, false
	}

	raw, err := uc.SyscallConn()
	if err != nil {
		return nil, false
	}

	var cred *unix.Ucred
	var sockErr error
	if ctrlErr := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); ctrlErr != nil {
		return nil, false
	}
	if sockErr != nil || cred == nil {
		return nil, false
	}
	return cred, true
}
