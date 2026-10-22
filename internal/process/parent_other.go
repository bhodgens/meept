//go:build !linux && !darwin

package process

// ParentPID is unavailable on this platform. It always reports failure so
// ancestor-based caller checks fail closed (see internal/rpc ACP approver
// identity): a platform that cannot verify ancestry cannot verify that a
// caller is NOT daemon-spawned, so privileged RPC methods reject.
func ParentPID(pid int) (int, bool) {
	return 0, false
}
