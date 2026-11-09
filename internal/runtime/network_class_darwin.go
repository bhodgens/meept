//go:build darwin

package runtime

import (
	"fmt"
	"os"
	"os/exec"
)

// darwinSandboxExec is the seatbelt launcher. A constant path keeps the
// wrap out of PATH lookup and out of shell re-parsing.
const darwinSandboxExec = "/usr/bin/sandbox-exec"

// noNetworkProfile allows local execution and denies every network
// operation. Applied to the child at launch; not a packet filter.
const noNetworkProfile = "(version 1)(allow default)(deny network*)"

// ApplyNetworkClass confines cmd before Start when class restricts egress.
// The original argv is preserved as discrete arguments after the profile,
// so a shell metacharacter in the script cannot escape the argv boundary.
func ApplyNetworkClass(cmd *exec.Cmd, class string) error {
	if !NetworkClassRestricts(class) {
		return nil
	}
	if cmd == nil {
		return fmt.Errorf("%w: nil command", ErrNetworkClassUnenforceable)
	}
	if _, err := os.Stat(darwinSandboxExec); err != nil {
		return fmt.Errorf("%w: %s unavailable: %v", ErrNetworkClassUnenforceable, darwinSandboxExec, err)
	}
	cmd.Args = append([]string{darwinSandboxExec, "-p", noNetworkProfile}, cmd.Args...)
	cmd.Path = darwinSandboxExec
	return nil
}
