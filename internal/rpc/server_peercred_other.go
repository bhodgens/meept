//go:build !linux && !darwin

package rpc

import "net"

// peerCredential is a stub for platforms without peer-credential support in
// this build (e.g. windows, plan9). Peer UID logging and allowlist enforcement
// are skipped silently; connections behave as before this feature existed.
func peerCredential(conn net.Conn) (uid int, ok bool) {
	return 0, false
}
