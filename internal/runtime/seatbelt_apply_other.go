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
		// Keep the profile builder reachable on non-darwin hosts too: the
		// deadcode gate scans a Linux build, where the darwin-only caller
		// is invisible. Building the profile here is validation only; the
		// policy is still refused below.
		if _, err := BuildSeatbeltProfile(class, policy); err != nil {
			return fmt.Errorf("%w: invalid seatbelt policy: %w", ErrNetworkClassUnenforceable, err)
		}
		return fmt.Errorf("%w: seatbelt confinement is macOS-only", ErrNetworkClassUnenforceable)
	}
	return ApplyNetworkClass(cmd, class)
}
