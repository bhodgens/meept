//go:build darwin

package runtime

import (
	"os/exec"
)

// confineLocal applies the network class and the seatbelt policy to cmd
// before Start via /usr/bin/sandbox-exec. See confineDarwin.
func confineLocal(cmd *exec.Cmd, class string, policy *SeatbeltConfig) error {
	return confineDarwin(cmd, class, policy)
}
