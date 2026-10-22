// Package process exposes kernel-verified process facts (parent PID) used
// for caller-identity checks on privileged surfaces.
//
// The core primitive is IsDescendantOf: walk the ancestor chain of a PID and
// report whether `ancestor` appears in it. The ACP approver-identity gate
// (internal/rpc) uses it to answer "was this RPC caller spawned, directly or
// transitively, by the daemon?" — agent-controlled processes (shell-tool
// children, PTY sessions, MCP stdio servers) are all daemon descendants;
// human surfaces (TUI, menubar, GUI) connect from processes the daemon did
// not spawn.
//
// Platforms without parent-PID lookup report failure, and the ACP gate fails
// closed on unverifiable ancestry.
package process

// maxAncestryDepth bounds the walk so a pathological/cyclic process table
// cannot spin. Real process trees are far shallower; init (PID 1) terminates
// the walk long before this.
const maxAncestryDepth = 64

// IsDescendantOf reports whether pid's ancestor chain contains ancestor.
// It walks ParentPID upward until it reaches ancestor (true), PID <= 1
// (false), the platform cannot resolve a parent (false), or
// maxAncestryDepth is exceeded (false).
//
// Note the direction: the caller-supplied `ancestor` is trusted (it is the
// daemon's own PID); the walked pid comes from the kernel (peer credentials)
// and cannot be forged by the connecting process.
func IsDescendantOf(pid, ancestor int) bool {
	if ancestor <= 1 || pid <= 0 {
		return false
	}
	cur := pid
	for i := 0; i < maxAncestryDepth; i++ {
		ppid, ok := ParentPID(cur)
		if !ok {
			return false
		}
		if ppid == ancestor {
			return true
		}
		if ppid <= 1 {
			return false
		}
		cur = ppid
	}
	return false
}
