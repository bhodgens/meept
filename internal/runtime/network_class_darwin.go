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

// confineDarwin profiles cmd before Start when a network class restricts
// egress or a seatbelt policy is armed. The original argv is preserved as
// discrete arguments after the profile, so a shell metacharacter in the
// script cannot escape the argv boundary. A build or stat failure refuses
// the launch: running unconfined after requesting confinement would be an
// escape.
func confineDarwin(cmd *exec.Cmd, class string, policy *SeatbeltConfig) error {
	profile, err := BuildSeatbeltProfile(class, policy)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNetworkClassUnenforceable, err)
	}
	if profile == "" {
		return nil
	}
	if cmd == nil {
		return fmt.Errorf("%w: nil command", ErrNetworkClassUnenforceable)
	}
	if _, err := os.Stat(darwinSandboxExec); err != nil {
		return fmt.Errorf("%w: %s unavailable: %w", ErrNetworkClassUnenforceable, darwinSandboxExec, err)
	}
	cmd.Args = append([]string{darwinSandboxExec, "-p", profile}, cmd.Args...)
	cmd.Path = darwinSandboxExec
	return nil
}

// ApplyNetworkClass confines cmd before Start when class restricts egress.
// Seatbelt policy stays unarmed on this path: a bare exec.Cmd carries no
// per-agent policy. LocalBackend uses confineDarwin with its configured
// policy instead.
func ApplyNetworkClass(cmd *exec.Cmd, class string) error {
	return confineDarwin(cmd, class, nil)
}
