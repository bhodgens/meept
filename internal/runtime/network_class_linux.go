//go:build linux

package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// ApplyNetworkClass confines cmd to a network namespace with no interfaces
// before Start. Root uses CLONE_NEWNET alone. Unprivileged launches also
// enter a user namespace mapped back to the caller's uid/gid so the
// namespace can be created without CAP_SYS_ADMIN. A kernel that rejects
// the flags fails Start; the command does not run unrestricted.
func ApplyNetworkClass(cmd *exec.Cmd, class string) error {
	if !NetworkClassRestricts(class) {
		return nil
	}
	if cmd == nil {
		return fmt.Errorf("%w: nil command", ErrNetworkClassUnenforceable)
	}
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
		cmd.SysProcAttr = attr
	}
	attr.Cloneflags |= syscall.CLONE_NEWNET
	if os.Geteuid() != 0 {
		attr.Cloneflags |= syscall.CLONE_NEWUSER
		if len(attr.UidMappings) == 0 {
			attr.UidMappings = []syscall.SysProcIDMap{{
				ContainerID: 0,
				HostID:      os.Getuid(),
				Size:        1,
			}}
		}
		if len(attr.GidMappings) == 0 {
			attr.GidMappings = []syscall.SysProcIDMap{{
				ContainerID: 0,
				HostID:      os.Getgid(),
				Size:        1,
			}}
			attr.GidMappingsEnableSetgroups = false
		}
	}
	return nil
}
