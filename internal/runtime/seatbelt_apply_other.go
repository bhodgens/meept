//go:build !darwin

package runtime

import (
	"fmt"
	"os/exec"
)

// confineLocal applies the network class and, on darwin only, the seatbelt
// policy. On this host a seatbelt policy cannot be enforced, so an armed
// policy refuses the launch: silently running unconfined after being asked
// to confine would be an escape. An inactive policy is a no-op and Linux
// keeps its network-namespace confinement byte-identically.
func confineLocal(cmd *exec.Cmd, class string, policy *SeatbeltConfig) error {
	if SeatbeltActive(policy) {
		return fmt.Errorf("%w: seatbelt confinement is macOS-only", ErrNetworkClassUnenforceable)
	}
	return ApplyNetworkClass(cmd, class)
}
