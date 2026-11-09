//go:build !linux && !darwin

package runtime

import (
	"fmt"
	"os/exec"
)

// ApplyNetworkClass refuses a restrictive class on hosts with no
// confinement primitive. Launching anyway would be unrestricted egress.
func ApplyNetworkClass(cmd *exec.Cmd, class string) error {
	if !NetworkClassRestricts(class) {
		return nil
	}
	if cmd == nil {
		return fmt.Errorf("%w: nil command", ErrNetworkClassUnenforceable)
	}
	return fmt.Errorf("%w: class %q has no confinement on this OS", ErrNetworkClassUnenforceable, class)
}
